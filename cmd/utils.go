package cmd

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var accessibleEndpoints []string
var jsonResultsStringArray []string
var jsonResultArray []Result
var jsonVerboseResultArray []VerboseResult
var specTitle string
var specDescription string
var externalRefCache = make(map[string]map[string]interface{})
var specToFilePath = make(map[*interface{}]string) // Maps external spec pointers to their file paths
var specBaseDir string                             // Directory of the loaded spec file for resolving external refs

type SchemaNode struct {
	Type                 string
	Properties           map[string]*SchemaNode
	Items                *SchemaNode
	Required             map[string]bool
	Enum                 []interface{}
	Example              interface{}
	Ref                  string
	OneOf                []*SchemaNode
	AnyOf                []*SchemaNode
	AdditionalProperties *SchemaNode
}

func BuildRequestsFromPaths(spec map[string]interface{}, client http.Client, replayClient *http.Client) {
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || paths == nil {
		die("Could not find any defined operations. Review the file manually.")
	}

	pathKeys := make([]string, 0, len(paths))
	for k := range paths {
		pathKeys = append(pathKeys, k)
	}
	slices.Sort(pathKeys)

	// Snapshot CLI-supplied headers so per-endpoint Content-Type
	// mutations from EnforceSingleContentType don't leak across endpoints.
	userHeaders := append([]string(nil), Headers...)
	var userContentType string
	for _, h := range userHeaders {
		if strings.HasPrefix(strings.ToLower(h), "content-type:") {
			if idx := strings.Index(h, ":"); idx >= 0 {
				userContentType = strings.TrimSpace(h[idx+1:])
			}
			break
		}
	}

	for _, pathName := range pathKeys {
		pathItem := paths[pathName]
		if ops, ok := pathItem.(map[string]interface{}); ok {
			methodKeys := make([]string, 0, len(ops))
			for k := range ops {
				methodKeys = append(methodKeys, k)
			}
			slices.Sort(methodKeys)
			for _, method := range methodKeys {
				op := ops[method]
				switch strings.ToLower(method) {
				// SKIPS THE "DELETE" AND "PATCH" METHODS FOR SAFETY
				case "delete":
					continue
				case "patch":
					continue
				default:
					if opMap, ok := op.(map[string]interface{}); ok {
						// Reset header state so the previous endpoint's
						// Content-Type doesn't leak into this one.
						Headers = append([]string(nil), userHeaders...)
						contentType = userContentType

						// Scoped to this operation: a description belongs to the
						// response it was declared under, so one endpoint's "Not
						// found" must never be printed against another's.
						errorDescriptions := make(map[string]string)
						if responses, ok := opMap["responses"].(map[string]interface{}); ok {
							for status, respItem := range responses {
								if respMap, ok := respItem.(map[string]interface{}); ok {
									if desc, ok := respMap["description"].(string); ok {
										errorDescriptions[status] = desc
									}
								}
							}
						}

						// The effective security the spec declares for this operation,
						// and whether an auth-required operation is being tested without a
						// matching credential (so a 2xx becomes a missing-auth finding).
						// userHeaders is the pre-mutation snapshot, so it carries any
						// auth-supplied header credential but not this op's header params.
						opSec := resolveOperationSecurity(spec, opMap)
						secLabel := opSec.label()
						authRequiredNoCred := opSec.state == secRequired &&
							!requirementSatisfiedByCredentials(opSec, spec, userHeaders)

						targetURL := fmt.Sprintf("%s%s%s", apiTarget, basePath, pathName)
						// Apply any query-string API keys from a security scheme
						// before spec params and before curl is composed, so the
						// sent request and the printed command both carry the key.
						for _, qp := range authQueryParams {
							targetURL = appendQueryParam(targetURL, qp.name, qp.value)
						}
						curl := fmt.Sprintf("curl -X %s \"%s\"", strings.ToUpper(method), targetURL)
						// variants collects every body this operation declares, from
						// all three sources below. None is discarded; selectVariants
						// decides which ones are sent.
						var variants []bodyVariant
						// cookiePairs accumulates "in: cookie" parameters so they can be sent
						// as a single Cookie header. formBody accumulates "in: formData"
						// (Swagger v2) fields, which become one urlencoded variant.
						var cookiePairs []string
						// Seed with any cookie-based API keys from a security scheme
						// (apiKey with "in: cookie") so they fold into the same Cookie
						// header as the operation's own cookie params, mirroring how
						// authQueryParams seeds the query string above.
						for _, cp := range authCookieParams {
							cookiePairs = append(cookiePairs, encodePair(cp.name, cp.value))
						}
						var formBody string
						var hasFormData bool

						// Extracts the expected parameters from the parameters object
						if params, ok := opMap["parameters"].([]interface{}); ok {
							for _, p := range params {
								var pValue string

								// Handle parameter-level $ref (e.g., #/parameters/... or #/components/parameters/...)
								if pMap, ok := p.(map[string]interface{}); ok {
									// Check if the parameter itself is a reference
									paramContextSpec := spec
									if ref, hasRef := pMap["$ref"].(string); hasRef {
										resolved, contextSpec := ResolveRefWithContext(spec, ref)
										if resolved != nil {
											pMap = resolved
											paramContextSpec = contextSpec
										}
									}

									if name, ok := pMap["name"].(string); ok {
										in, hasIn := pMap["in"].(string)
										if !hasIn {
											continue
										}

										// A Swagger v2 "in: body" parameter's schema is the entire request
										// body, so it is serialized as JSON rather than folded into form
										// fields. This covers non-object schemas (an array body) too.
										if in == "body" {
											bodySchema, _ := pMap["schema"].(map[string]interface{})
											expandedBody := ExpandSchema(spec, bodySchema, map[string]bool{}, paramContextSpec)
											if variant, ok := buildVariant(ctJSON, GenerateExample(expandedBody)); ok {
												variants = append(variants, variant)
											}
											continue
										}

										// Handle schema-based parameters (OpenAPI v3 and some v2)
										var handledAsObject bool // Track if we already handled this as an object
										if schema, ok := pMap["schema"].(map[string]interface{}); ok {
											expanded := ExpandSchema(spec, schema, map[string]bool{}, paramContextSpec)
											if expanded.Type == "object" || len(expanded.Properties) > 0 {
												// For object schemas, generate full example and serialize
												example := GenerateExample(expanded)
												if exampleMap, ok := example.(map[string]interface{}); ok {
													// Handle based on parameter location
													if in == "query" {
														// Query params with object schema: add each property to query string
														for _, propertyItem := range slices.Sorted(maps.Keys(exampleMap)) {
															targetURL = appendQueryParam(targetURL, propertyItem, exampleMap[propertyItem])
														}
														handledAsObject = true
													}
												}
											} else {
												// For primitive types, generate simple value
												exampleValue := GenerateExample(expanded)
												if expanded.Type == "string" && name != "version" {
													pValue = testString
												} else if exampleValue != nil {
													pValue = fmt.Sprintf("%v", exampleValue)
												} else {
													pValue = "1"
												}
											}
										} else if pType, ok := pMap["type"].(string); ok {
											// Direct type without schema (Swagger v2 style)
											// Check for default value first
											if defaultVal := pMap["default"]; defaultVal != nil {
												pValue = fmt.Sprintf("%v", defaultVal)
											} else if pType == "string" && name != "version" {
												pValue = testString
											} else {
												pValue = "1"
											}
										} else if defaultVal := pMap["default"]; defaultVal != nil {
											// Use default value if no type or schema
											pValue = fmt.Sprintf("%v", defaultVal)
										} else {
											// Fallback to generic value
											pValue = "1"
										}

										// An unconsumed object schema leaves pValue empty: "/pet/{id}" -> "/pet/".
										if !handledAsObject && pValue == "" {
											pValue = testString
										}

										// Only process parameters that weren't already handled as objects
										if !handledAsObject {
											switch in {
											case "query":
												targetURL = appendQueryParam(targetURL, name, pValue)
											case "path":
												targetURL = replacePathParam(targetURL, name, pValue)
											case "header":
												// Append to Headers so the value is actually sent (applyHeaders
												// applies every Headers entry to the request), and mirror it in
												// the printed command. A header value is not a URL component, so
												// it is shell-quoted for display but never percent-encoded.
												Headers = append(Headers, name+": "+pValue)
												curl += " -H " + shellSingleQuote(name+": "+pValue)
											case "cookie":
												// Accumulate cookie params; they are emitted as one Cookie
												// header after every parameter is known.
												cookiePairs = append(cookiePairs, encodePair(name, pValue))
											case "formData":
												// Swagger v2 form field: contributes to the urlencoded body.
												formBody = appendFormField(formBody, name, pValue)
												hasFormData = true
											}
										}
									}
								}
							}
						}

						// Emit accumulated cookie parameters as a single Cookie header,
						// both on the request (via Headers) and in the printed command.
						if len(cookiePairs) > 0 {
							cookieValue := strings.Join(cookiePairs, "; ")
							Headers = append(Headers, "Cookie: "+cookieValue)
							curl += " -b " + shellSingleQuote(cookieValue)
						}

						// Swagger v2 formData fields have no requestBody to declare a
						// content type, so they become an explicit urlencoded variant.
						if hasFormData {
							variants = append(variants, newTextBodyVariant(ctForm, ctForm, formBody))
						}

						// Extracts the expected parameters from the requestBody object
						if reqBody, ok := opMap["requestBody"].(map[string]interface{}); ok {
							// Handle requestBody-level $ref (e.g., #/components/requestBodies/...)
							reqBodyContextSpec := spec
							if ref, hasRef := reqBody["$ref"].(string); hasRef {
								resolved, contextSpec := ResolveRefWithContext(spec, ref)
								if resolved != nil {
									reqBody = resolved
									reqBodyContextSpec = contextSpec
								}
							}
							variants = append(variants, buildBodyVariants(spec, reqBody, reqBodyContextSpec)...)
						}

						logURL, parseErr := url.Parse(targetURL)
						if parseErr != nil || logURL == nil {
							printWarn("Error parsing URL '%s': %v - skipping endpoint.", targetURL, parseErr)
							continue
						}

						if os.Args[1] == "endpoints" {
							// endpoints only lists paths, so the body never matters and
							// one line per path+method is printed regardless of how many
							// content types the operation declares.
							fmt.Println(basePath + pathName)
							continue
						}

						variants = orderVariants(variants)
						if os.Args[1] == "prepare" && strings.EqualFold(prepareFor, "sqlmap") {
							// sqlmap has no -F equivalent, so a multipart variant would
							// print a command that cannot be run.
							variants = slices.DeleteFunc(variants, func(v bodyVariant) bool {
								return v.declared == ctMultipart
							})
						}

						selected := selectVariants(variants, contentType, allContentTypes)
						if len(selected) == 0 {
							// No declared body: a single bodyless request, as before.
							selected = []bodyVariant{{}}
						}

						if os.Args[1] == "automate" {
							// Ask once per operation. sj may send this same URL once per
							// declared content type, and re-prompting for each one would
							// be noise; approvedTarget stops MakeRequestFull asking again.
							if dangerousRequestDeclined(targetURL) {
								continue
							}
							approvedTarget = targetURL
						}

						// Snapshot the state every variant starts from. The loop below
						// rebuilds curl and Headers per variant so that what is printed
						// and what goes on the wire describe the same request.
						baseCurl := curl
						baseHeaders := append([]string(nil), Headers...)

						// The body encoding is only worth reporting when an operation
						// was tested under more than one. Otherwise it would add a field
						// to every result row that says nothing the request did not
						// already imply, and change output every consumer already parses.
						reportContentType := len(selected) > 1

						for _, v := range selected {
							// Re-copy rather than assign: EnforceSingleContentType
							// compacts Headers in place, so sharing a backing array would
							// let one variant rewrite the snapshot the next one starts from.
							Headers = append([]string(nil), baseHeaders...)
							curl = baseCurl

							// A bodyless request declares no media type. Enforcing an
							// empty one would append a bare "Content-Type:" that
							// applyHeaders only replaces when the request carries bytes.
							if v.contentType != "" {
								EnforceSingleContentType(v.contentType)
								if !v.omitCurlCT {
									curl += " -H " + shellSingleQuote("Content-Type: "+v.contentType)
								}
							}
							curl += v.curlArgs

							// Update the curl command with the final targetURL (which may have been modified with query params)
							// Extract and replace the URL in quotes
							curlParts := strings.SplitN(curl, "\"", 3)
							if len(curlParts) >= 3 {
								curl = curlParts[0] + "\"" + targetURL + "\"" + curlParts[2]
							}

							switch os.Args[1] {
							case "automate":
								// These are the same bytes the printed curl command was
								// composed from above, so what is shown and what goes on
								// the wire cannot disagree.
								sentBody := []byte(v.body)

								_, resp, sc, _, sentUA := MakeRequestFull(client, strings.ToUpper(method), targetURL, timeout, bytes.NewReader(sentBody))

								tempResponsePreviewLength := responsePreviewLength
								if len(resp) <= responsePreviewLength {
									tempResponsePreviewLength = len(resp)
								}

								// Only annotate responses that went wrong: a 2xx
								// description ("successful operation") only repeats what
								// the status already says. Descriptions are keyed by the
								// status as written in the document, with "default" as the
								// spec's catch-all.
								description := ""
								if sc < 200 || sc > 299 {
									desc, ok := errorDescriptions[strconv.Itoa(sc)]
									if !ok {
										desc = errorDescriptions["default"]
									}
									description = desc
								}

								reportedCT := ""
								if reportContentType {
									reportedCT = v.declared
								}
								// A spec-declared-auth operation answering 2xx with no
								// credential supplied is a possible broken access control.
								missingAuth := authRequiredNoCred && sc >= 200 && sc <= 299
								var result []byte

								if verbose {
									result, _ = json.Marshal(VerboseResult{Method: method, Preview: resp[:tempResponsePreviewLength], Status: sc, Target: logURL.Path, Curl: curl, ContentType: reportedCT, Security: secLabel, MissingAuth: missingAuth})
								} else {
									result, _ = json.Marshal(Result{Method: method, Status: sc, Target: logURL.Path, ContentType: reportedCT, Security: secLabel, MissingAuth: missingAuth})
								}

								if getAccessibleEndpoints {
									if sc == 200 {
										accessibleEndpoints = append(accessibleEndpoints, logURL.Path)
										if jsonResultsStringArray == nil {
											jsonResultsStringArray = append(jsonResultsStringArray, string(result))
										} else {
											jsonResultsStringArray = append(jsonResultsStringArray, ","+string(result))
										}
										if outputFormat == "console" {
											writeLog(sc, logURL.Path, strings.ToUpper(method), description, resp[:tempResponsePreviewLength], secLabel, missingAuth)
										}
										if replayClient != nil {
											ReplayRequest(replayClient, strings.ToUpper(method), targetURL, timeout, bytes.NewReader(sentBody), sentUA)
										}
									}
								} else {
									if jsonResultsStringArray == nil {
										jsonResultsStringArray = append(jsonResultsStringArray, string(result))
									} else {
										jsonResultsStringArray = append(jsonResultsStringArray, ","+string(result))
									}
									if outputFormat == "console" {
										writeLog(sc, logURL.Path, strings.ToUpper(method), description, resp[:tempResponsePreviewLength], secLabel, missingAuth)
									}
									if replayClient != nil {
										ReplayRequest(replayClient, strings.ToUpper(method), targetURL, timeout, bytes.NewReader(sentBody), sentUA)
									}
								}

							case "prepare":
								var preparedCommand string = curl
								if strings.EqualFold(prepareFor, "sqlmap") {
									preparedCommand = strings.Replace(preparedCommand, "curl", "sqlmap", 1)
									preparedCommand = strings.Replace(preparedCommand, "-X "+strings.ToUpper(method), "--method="+strings.ToUpper(method)+" -u", 1)
									if v.curlBodyArg != "" {
										preparedCommand = strings.Replace(preparedCommand, "-d "+v.curlBodyArg, "--data="+v.curlBodyArg, 1)
									}
									preparedCommand = "$ " + preparedCommand
								} else if strings.EqualFold(prepareFor, "curl") {
									preparedCommand = "$ " + curl
								}
								fmt.Println(preparedCommand)
							}
						}
					}
				}
			}
		}
	}
	if os.Args[1] == "automate" && outputFormat == "json" {
		for r := range jsonResultsStringArray {
			var result Result
			var verboseResult VerboseResult
			if verbose {
				err := json.Unmarshal([]byte(strings.TrimPrefix(jsonResultsStringArray[r], ",")), &verboseResult)
				if err != nil {
					die("Error marshalling JSON: %v", err)
				}
				jsonVerboseResultArray = append(jsonVerboseResultArray, verboseResult)
			} else {
				err := json.Unmarshal([]byte(strings.TrimPrefix(jsonResultsStringArray[r], ",")), &result)
				if err != nil {
					die("Error marshalling JSON: %v", err)
				}
				jsonResultArray = append(jsonResultArray, result)
			}
		}
		writeLog(8899, "", "", "", "", "", false)
	}
}

func EnforceSingleContentType(newContentType string) {
	newContentType = strings.TrimSpace(newContentType)

	// Remove old 'Content-Type' header
	Headers = slices.DeleteFunc(Headers, func(h string) bool {
		return strings.HasPrefix(strings.ToLower(h), "content-type:")
	})

	Headers = append(Headers, "Content-Type: "+newContentType)

	// Remove empty elements to avoid repetitions of "-H ''"
	Headers = slices.DeleteFunc(Headers, func(h string) bool {
		return strings.TrimSpace(h) == ""
	})
}

func ExpandSchema(
	spec map[string]interface{},
	schema map[string]interface{},
	visited map[string]bool,
	contextSpec map[string]interface{}, // The spec document this schema belongs to (for resolving nested refs)
) *SchemaNode {
	if schema == nil {
		return &SchemaNode{Type: "object"}
	}

	if ref, ok := schema["$ref"].(string); ok {
		if visited[ref] {
			return &SchemaNode{Type: "object"} // break cycle: ref is already on the current path
		}

		// Resolve ref in the context spec (could be external)
		resolved, resolvedSpec := ResolveRefWithContext(contextSpec, ref)
		if resolved == nil {
			return &SchemaNode{Type: "object"}
		}

		// Mark ref as on the current expansion path, expand, then pop it so sibling
		// branches can reference the same schema without a false cycle-break.
		visited[ref] = true
		node := ExpandSchema(spec, resolved, visited, resolvedSpec)
		delete(visited, ref)
		return node
	}

	node := &SchemaNode{
		Properties: map[string]*SchemaNode{},
		Required:   map[string]bool{},
	}

	if t, ok := schema["type"].(string); ok {
		node.Type = t
	}

	// Handle enum values
	if enum, ok := schema["enum"].([]interface{}); ok {
		node.Enum = enum
	}

	// Handle example values
	if example := schema["example"]; example != nil {
		node.Example = example
	}

	// Populate required fields
	if required, ok := schema["required"].([]interface{}); ok {
		for _, r := range required {
			if fieldName, ok := r.(string); ok {
				node.Required[fieldName] = true
			}
		}
	}

	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for name, raw := range props {
			if m, ok := raw.(map[string]interface{}); ok {
				node.Properties[name] = ExpandSchema(spec, m, visited, contextSpec)
			}
		}
	}

	if items, ok := schema["items"].(map[string]interface{}); ok {
		node.Items = ExpandSchema(spec, items, visited, contextSpec)
	}

	// Handle additionalProperties
	if addProps, ok := schema["additionalProperties"]; ok {
		if addPropsMap, ok := addProps.(map[string]interface{}); ok {
			node.AdditionalProperties = ExpandSchema(spec, addPropsMap, visited, contextSpec)
		}
	}

	// Handle allOf (merge every subschema into this node; the node's own values win)
	if allOf, ok := schema["allOf"].([]interface{}); ok {
		for _, entry := range allOf {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			sub := ExpandSchema(spec, m, visited, contextSpec)
			for k, v := range sub.Properties {
				if _, exists := node.Properties[k]; !exists {
					node.Properties[k] = v
				}
			}
			for k, v := range sub.Required {
				node.Required[k] = v
			}
			if node.Type == "" {
				node.Type = sub.Type
			}
			if node.Enum == nil {
				node.Enum = sub.Enum
			}
			if node.Example == nil {
				node.Example = sub.Example
			}
			if node.Items == nil {
				node.Items = sub.Items
			}
			if node.AdditionalProperties == nil {
				node.AdditionalProperties = sub.AdditionalProperties
			}
			node.OneOf = append(node.OneOf, sub.OneOf...)
			node.AnyOf = append(node.AnyOf, sub.AnyOf...)
		}
		if node.Type == "" {
			node.Type = "object"
		}
	}

	// Handle oneOf (expand all options)
	if oneOf, ok := schema["oneOf"].([]interface{}); ok {
		for _, entry := range oneOf {
			if m, ok := entry.(map[string]interface{}); ok {
				node.OneOf = append(node.OneOf, ExpandSchema(spec, m, visited, contextSpec))
			}
		}
	}

	// Handle anyOf (expand all options)
	if anyOf, ok := schema["anyOf"].([]interface{}); ok {
		for _, entry := range anyOf {
			if m, ok := entry.(map[string]interface{}); ok {
				node.AnyOf = append(node.AnyOf, ExpandSchema(spec, m, visited, contextSpec))
			}
		}
	}

	return node
}

func GenerateExample(node *SchemaNode) interface{} {
	// Use example value if available
	if node.Example != nil {
		return node.Example
	}

	// Use first enum value if available
	if len(node.Enum) > 0 {
		return node.Enum[0]
	}

	// Handle oneOf - use first option
	if len(node.OneOf) > 0 {
		return GenerateExample(node.OneOf[0])
	}

	// Handle anyOf - use first option
	if len(node.AnyOf) > 0 {
		return GenerateExample(node.AnyOf[0])
	}

	switch node.Type {
	case "object", "":
		obj := map[string]interface{}{}
		for k, v := range node.Properties {
			if strings.Contains(strings.ToLower(k), "date") {
				obj[k] = customDate
			} else if strings.Contains(strings.ToLower(k), "url") {
				obj[k] = customURL
			} else if strings.Contains(strings.ToLower(k), "email") {
				obj[k] = customEmail
			} else {
				obj[k] = GenerateExample(v)
			}
		}
		// Handle additionalProperties if present and no regular properties
		if len(obj) == 0 && node.AdditionalProperties != nil {
			obj["additionalProp1"] = GenerateExample(node.AdditionalProperties)
		}
		return obj
	case "array":
		if node.Items != nil {
			return []interface{}{GenerateExample(node.Items)}
		}
		return []interface{}{}
	case "string":
		return testString
	case "integer", "number":
		return 1
	case "boolean":
		return true
	default:
		return nil
	}
}

func GenerateRequests(bodyBytes []byte, client http.Client, replayClient *http.Client) {
	// Swagger UI bundles serve the spec wrapped in JavaScript (e.g.
	// swagger-ui-init.js). Detect that and unwrap before parsing so the
	// downstream YAML/JSON unmarshal succeeds.
	if looksLikeJSSpec(bodyBytes) {
		if extracted, ok := ExtractJSONFromJSSpec(bodyBytes); ok {
			bodyBytes = extracted
		}
	}

	// Ingests the specification file
	spec := SafelyUnmarshalSpec(bodyBytes)

	// Checks defined security schemes and prompts for authentication
	CheckSecuritySchemes(spec)

	// Reports the effective security requirement of every declared operation so
	// the operator can see which are meant to be authenticated, which are public,
	// and which reference an undefined scheme.
	SummarizeOperationSecurity(spec)

	u, parseErr := url.Parse(swaggerURL)
	if parseErr != nil {
		u = &url.URL{}
	}

	// Parse basePath and server info from spec
	// Always extract basePath, even if -T was used (so -T sets host but spec sets path)
	if v, ok := spec["swagger"].(string); ok && strings.HasPrefix(v, "2") {
		// Swagger (v2)
		host, _ := spec["host"].(string)
		bp, _ := spec["basePath"].(string)
		if bp != "" {
			basePath = normalizeBasePath(bp)
		}

		// Only set apiTarget from spec if -T flag wasn't used
		if apiTarget == "" {
			if host != "" && strings.Contains(host, "://") {
				apiTarget = host
			} else {
				if host != "" {
					scheme := u.Scheme
					// If scheme is empty (e.g., local file), try to get from spec's schemes array
					if scheme == "" {
						if schemes, ok := spec["schemes"].([]interface{}); ok && len(schemes) > 0 {
							if s, ok := schemes[0].(string); ok {
								scheme = s
							}
						}
					}
					// Default to https if still no scheme
					if scheme == "" {
						scheme = "https"
					}
					apiTarget = scheme + "://" + host
				}
			}
		}
	} else if v, ok := spec["openapi"].(string); ok && strings.HasPrefix(v, "3") {
		// OpenAPI (v3)
		if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
			if len(servers) > 1 {
				if !quiet && (os.Args[1] != "endpoints") && apiTarget == "" {
					printWarn("Multiple servers detected in documentation. You can manually set a server to test with the -T flag.\nThe detected servers are as follows:")
					for i := range servers {
						if srv, ok := servers[i].(map[string]interface{}); ok {
							if serverURL, ok := srv["url"].(string); ok {
								printInfo("%s\n", serverURL)
							}
						}
					}
				}
			} else {
				if srv, ok := servers[0].(map[string]interface{}); ok {
					if serverURL, ok := srv["url"].(string); ok {
						if strings.Contains(serverURL, "://") {
							// Full URL in server
							if parsedServerURL, err := url.Parse(serverURL); err == nil {
								basePath = normalizeBasePath(parsedServerURL.Path)
								if apiTarget == "" {
									apiTarget = parsedServerURL.Scheme + "://" + parsedServerURL.Host
								}
							}
						} else if serverURL == "/" {
							basePath = ""
						} else {
							// Relative URL - this becomes the basePath
							basePath = normalizeBasePath(serverURL)
							// Only try to construct apiTarget if -T wasn't used
							if apiTarget == "" {
								if u.Scheme != "" && u.Host != "" {
									apiTarget = u.Scheme + "://" + u.Host
								} else {
									// Local file with relative server URL and no -T flag
									// Only fail for commands that need full URLs
									if os.Args[1] != "endpoints" {
										die("Spec has relative server URL '%s' but no base URL available. Use -T to specify target server.", serverURL)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Use the original host at the target if no server found from specification.
	if apiTarget == "" {
		if u.Scheme != "" && u.Host != "" {
			apiTarget = u.Scheme + "://" + u.Host
		} else {
			// No server info and no URL to parse - require user to specify target
			// Only fail for commands that need full URLs
			if os.Args[1] != "endpoints" {
				die("No server information found in spec and no URL provided. Use -T to specify target server.")
			}
		}
	}

	if os.Args[1] != "endpoints" {
		// Prints Title/Description/Version values if they exist
		PrintSpecInfo(spec)
	}

	// Reviews all defined API routes and builds requests as defined
	BuildRequestsFromPaths(spec, client, replayClient)
}

func ResolveRef(spec map[string]interface{}, ref string) map[string]interface{} {
	resolved, _ := ResolveRefWithContext(spec, ref)
	return resolved
}

func normalizeBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "/" {
		return ""
	}
	return path
}

// encodePair returns an encoded "name=value" pair for a query string or an
// x-www-form-urlencoded body. This and replacePathParam are the only places
// the --raw-values escape hatch is honored, so the printed curl command and
// the request that is actually sent can never disagree.
func encodePair(name string, value interface{}) string {
	v := fmt.Sprintf("%v", value)
	if rawValues {
		return name + "=" + v
	}
	return url.QueryEscape(name) + "=" + url.QueryEscape(v)
}

// appendQueryParam returns rawURL with an encoded pair appended, using "?" or
// "&" depending on whether rawURL already carries a query string.
func appendQueryParam(rawURL, name string, value interface{}) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + encodePair(name, value)
}

// replacePathParam substitutes the first {name} placeholder in rawURL with a
// path-escaped value so a "/", "?" or "#" cannot re-segment the URL.
func replacePathParam(rawURL, name, value string) string {
	if !rawValues {
		value = url.PathEscape(value)
	}
	return strings.Replace(rawURL, "{"+name+"}", value, 1)
}

// appendFormField returns an x-www-form-urlencoded body with an encoded pair
// appended, separated by "&" when body is not empty.
func appendFormField(body, name string, value interface{}) string {
	pair := encodePair(name, value)
	if body == "" {
		return pair
	}
	return body + "&" + pair
}

// encodeFormBody serializes obj as an x-www-form-urlencoded body with keys in
// sorted order so repeated runs produce identical output.
func encodeFormBody(obj map[string]interface{}) string {
	var body string
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		body = appendFormField(body, k, obj[k])
	}
	return body
}

// shellSingleQuote wraps s in single quotes for a printed shell command,
// escaping any embedded single quote the POSIX way (close, escape, reopen) so
// the command stays runnable. This is presentation only and is not affected
// by --raw-values.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// escapeXmlText returns s with XML metacharacters escaped so a value
// containing '<', '>' or '&' cannot break out of its element.
func escapeXmlText(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return ""
	}
	return b.String()
}

// ResolveRefWithContext resolves a reference and returns both the resolved schema and the spec it came from
func ResolveRefWithContext(spec map[string]interface{}, ref string) (map[string]interface{}, map[string]interface{}) {
	// Handle external references (e.g., "./schemas/user.yaml#/User")
	if !strings.HasPrefix(ref, "#") {
		// Determine the base directory for resolving this external ref
		baseDir := specBaseDir
		// Check if the current spec is an external file
		for cachedPath, cachedSpec := range externalRefCache {
			if fmt.Sprintf("%p", cachedSpec) == fmt.Sprintf("%p", spec) {
				// This spec is an external file, use its directory as base
				baseDir = filepath.Dir(cachedPath)
				break
			}
		}

		resolved := ResolveExternalRef(ref, baseDir)
		// For external refs, the resolved schema's context is itself
		// (nested refs within it should be resolved in the external doc)
		if resolved != nil {
			// Try to get the cached external spec as context
			filePath := strings.SplitN(ref, "#", 2)[0]
			fullPath := filepath.Clean(filepath.Join(baseDir, filePath))
			if externalSpec, exists := externalRefCache[fullPath]; exists {
				return resolved, externalSpec
			}
		}
		return resolved, spec
	}

	parts := strings.Split(ref[2:], "/")
	var cur interface{} = spec

	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, spec
		}
		cur = m[p]
	}

	resolved, _ := cur.(map[string]interface{})
	// Internal refs stay in the same spec context
	return resolved, spec
}

func ResolveExternalRef(ref string, baseDir string) map[string]interface{} {
	// Parse external reference format: "<file_path>#<json_pointer>"
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) < 1 {
		return nil
	}

	relativePath := parts[0]
	var jsonPointer string
	if len(parts) == 2 {
		jsonPointer = parts[1]
	}

	// Resolve path relative to the provided base directory
	filePath := filepath.Join(baseDir, relativePath)
	filePath = filepath.Clean(filePath)

	// Check cache first
	if cached, exists := externalRefCache[filePath]; exists {
		if jsonPointer == "" {
			return cached
		}
		// Resolve pointer within cached file
		return ResolveRef(cached, "#"+jsonPointer)
	}

	// Load external file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	externalSpec := SafelyUnmarshalSpec(fileData)
	if externalSpec == nil {
		return nil
	}

	// Cache the loaded file
	externalRefCache[filePath] = externalSpec

	// Resolve pointer if present
	if jsonPointer == "" {
		return externalSpec
	}
	return ResolveRef(externalSpec, "#"+jsonPointer)
}

func PrintSpecInfo(spec map[string]interface{}) {
	info, ok := spec["info"].(map[string]interface{})
	if !ok || info == nil {
		if !quiet {
			printInfo("No information defined in the documentation.\n")
		}
	} else {
		title, ok := info["title"].(string)
		if ok && title != "" {
			if outputFormat == "json" {
				specTitle = title
			} else if !quiet {
				printInfo("Title: %s\n", title)
			}
		}

		description, ok := info["description"].(string)
		if ok && description != "" {
			if outputFormat == "json" {
				specDescription = description
			} else if !quiet {
				printInfo("Description: %s\n", description)
			}
		}
	}
}

func SetScheme(swaggerURL string) (scheme string) {
	if strings.HasPrefix(swaggerURL, "http://") {
		scheme = "http"
	} else if strings.HasPrefix(swaggerURL, "https://") {
		scheme = "https"
	} else {
		scheme = "https"
	}
	return scheme
}

// looksLikeJSSpec is a cheap content sniff so we only run the JS-extraction
// regex against bodies that plausibly contain JavaScript. Normal JSON
// (starts with `{`/`[`) and YAML (starts with `openapi:`/`swagger:`/`---`/`#`)
// fall through unchanged.
func looksLikeJSSpec(b []byte) bool {
	if strings.HasSuffix(strings.ToLower(swaggerURL), ".js") ||
		strings.HasSuffix(strings.ToLower(localFile), ".js") ||
		strings.ToLower(format) == "js" {
		return true
	}
	trimmed := bytes.TrimLeft(b, " \t\r\n")
	prefixes := [][]byte{
		[]byte("var "), []byte("let "), []byte("const "),
		[]byte("(function"), []byte("window."),
		[]byte("//"), []byte("/*"),
	}
	for _, p := range prefixes {
		if bytes.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func SafelyUnmarshalSpec(data []byte) map[string]interface{} {

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		die("Failed to unmarshal API documentation: %v", err)
	}

	return doc
}

/*
TrimHostScheme trims the scheme from the provided URL if the '-T' flag is supplied to sj.
*/
func TrimHostScheme(apiTarget, fullUrlHost string) (host string) {
	if apiTarget != "" {
		if strings.HasPrefix(apiTarget, "http://") {
			host = strings.TrimPrefix(apiTarget, "http://")
		} else if strings.HasPrefix(apiTarget, "https://") {
			host = strings.TrimPrefix(apiTarget, "https://")
		} else {
			host = apiTarget
		}
	} else {
		host = fullUrlHost
	}
	return host
}

func XmlFromObject(obj map[string]interface{}) string {
	var b strings.Builder

	// Sorted keys keep the generated body identical between runs.
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		switch val := obj[k].(type) {
		case map[string]interface{}:
			b.WriteString("<" + k + ">")
			b.WriteString(XmlFromObject(val))
			b.WriteString("</" + k + ">")
		case []interface{}:
			for _, item := range val {
				b.WriteString("<" + k + ">")
				if m, ok := item.(map[string]interface{}); ok {
					b.WriteString(XmlFromObject(m))
				} else {
					b.WriteString(escapeXmlText(fmt.Sprintf("%v", item)))
				}
				b.WriteString("</" + k + ">")
			}
		default:
			b.WriteString("<" + k + ">" + escapeXmlText(fmt.Sprintf("%v", val)) + "</" + k + ">")
		}
	}

	return b.String()
}
