package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

func BuildRequestsFromPaths(spec map[string]interface{}, client http.Client) {
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || paths == nil {
		die("Could not find any defined operations. Review the file manually.")
	}

	pathKeys := make([]string, 0, len(paths))
	for k := range paths {
		pathKeys = append(pathKeys, k)
	}
	slices.Sort(pathKeys)

	var errorDescriptions = make(map[any]string)
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
						if responses, ok := opMap["responses"].(map[string]interface{}); ok {
							for status, respItem := range responses {
								if respMap, ok := respItem.(map[string]interface{}); ok {
									if desc, ok := respMap["description"].(string); ok {
										errorDescriptions[status] = desc
									}
								}

							}
						}

						targetURL := fmt.Sprintf("%s%s%s", apiTarget, basePath, pathName)
						curl := fmt.Sprintf("curl -X %s \"%s\"", strings.ToUpper(method), targetURL)
						var bodyData string

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
										in := pMap["in"].(string)

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
														for propertyItem, propertyValue := range exampleMap {
															pVal := fmt.Sprintf("%v", propertyValue)
															if strings.Contains(curl, "?") || strings.Contains(targetURL, "?") {
																targetURL += fmt.Sprintf("&%s=%s", propertyItem, pVal)
															} else {
																targetURL += fmt.Sprintf("?%s=%s", propertyItem, pVal)
															}
														}
														handledAsObject = true
													} else if in == "body" {
														// Body params with object schema: add each property to body data
														for propertyItem, propertyValue := range exampleMap {
															pVal := fmt.Sprintf("%v", propertyValue)
															if strings.Contains(curl, "-d '") {
																bodyData += fmt.Sprintf("&%s=%s", propertyItem, pVal)
																curl = strings.TrimSuffix(curl, "'")
																curl += fmt.Sprintf("&%s=%s'", propertyItem, pVal)
															} else {
																bodyData += fmt.Sprintf("%s=%s", propertyItem, pVal)
																curl += fmt.Sprintf(" -d '%s=%s'", propertyItem, pVal)
															}
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

										// Only process parameters that weren't already handled as objects
										if !handledAsObject {
											switch in {
											case "query":
												if strings.Contains(curl, "?") || strings.Contains(targetURL, "?") {
													targetURL += fmt.Sprintf("&%s=%s", name, pValue)
												} else {
													targetURL += fmt.Sprintf("?%s=%s", name, pValue)
												}
											case "path":
												targetURL = strings.Replace(targetURL, "{"+name+"}", pValue, 1)
											case "header":
												curl += fmt.Sprintf(" -H \"%s: %s\"", name, pValue)
											case "body":
												if strings.Contains(curl, "-d '") {
													bodyData += fmt.Sprintf("&%s=%s", name, pValue)
													curl = strings.TrimSuffix(curl, "'")
													curl += fmt.Sprintf("&%s=%s'", name, pValue)
												} else {
													bodyData += fmt.Sprintf("%s=%s", name, pValue)
													curl += fmt.Sprintf(" -d '%s=%s'", name, pValue)
												}
											}
										}
									}
								}
							}
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

							if contentTypes, ok := reqBody["content"].(map[string]interface{}); ok {
								for cType := range contentTypes {
									if contentType == "" {
										EnforceSingleContentType(cType)
									} else {
										EnforceSingleContentType(contentType)
									}

									if ct, ok := contentTypes[cType].(map[string]interface{}); ok {
										if schema, ok := ct["schema"].(map[string]interface{}); ok {
											expanded := ExpandSchema(spec, schema, map[string]bool{}, reqBodyContextSpec)
											example := GenerateExample(expanded)

											if cType == "application/json" {
												bodyBytes, err := json.Marshal(example)
												if err == nil {
													curl += fmt.Sprintf(" -H \"Content-Type: application/json\" -d '%s'", bodyBytes)
												}
											}
											if cType == "application/xml" || cType == "text/xml" {
												if obj, ok := example.(map[string]interface{}); ok {
													xml := XmlFromObject(obj)
													curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", cType, xml)
												}
											}
											if cType == "application/x-www-form-urlencoded" || cType == "multipart/form-data" {
												if obj, ok := example.(map[string]interface{}); ok {
													var formParts []string
													for k, v := range obj {
														formParts = append(formParts, fmt.Sprintf("%s=%v", k, v))
													}
													formData := strings.Join(formParts, "&")
													curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", cType, formData)
												}
											}
										}
									}
								}
							}
						}

						// Update the curl command with the final targetURL (which may have been modified with query params)
						// Extract and replace the URL in quotes
						curlParts := strings.SplitN(curl, "\"", 3)
						if len(curlParts) >= 3 {
							curl = curlParts[0] + "\"" + targetURL + "\"" + curlParts[2]
						}

						logURL, parseErr := url.Parse(targetURL)
						if parseErr != nil || logURL == nil {
							printWarn("Error parsing URL '%s': %v - skipping endpoint.", targetURL, parseErr)
							continue
						}
						switch os.Args[1] {
						case "automate":
							var postBodyData string
							if strings.ToLower(method) == "post" && strings.Contains(curl, "-d") {
								dataIndex := strings.Index(curl, "'")
								postBodyData = curl[dataIndex+1 : len(curl)-1]
							}

							_, resp, sc := MakeRequest(client, strings.ToUpper(method), targetURL, timeout, bytes.NewReader([]byte(postBodyData)))

							tempResponsePreviewLength := responsePreviewLength
							if len(resp) <= responsePreviewLength {
								tempResponsePreviewLength = len(resp)
							}

							var result []byte

							if verbose {
								result, _ = json.Marshal(VerboseResult{Method: method, Preview: resp[:tempResponsePreviewLength], Status: sc, Target: logURL.Path, Curl: curl})
							} else {
								result, _ = json.Marshal(Result{Method: method, Status: sc, Target: logURL.Path})
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
										writeLog(sc, logURL.Path, strings.ToUpper(method), errorDescriptions[sc], resp[:tempResponsePreviewLength])
									}
								}
							} else {
								if jsonResultsStringArray == nil {
									jsonResultsStringArray = append(jsonResultsStringArray, string(result))
								} else {
									jsonResultsStringArray = append(jsonResultsStringArray, ","+string(result))
								}
								if outputFormat == "console" {
									writeLog(sc, logURL.Path, strings.ToUpper(method), errorDescriptions[sc], resp[:tempResponsePreviewLength])
								}
							}

						case "endpoints":
							fmt.Println(basePath + pathName)
						case "prepare":
							var preparedCommand string = curl
							if strings.ToLower(prepareFor) == "sqlmap" {
								preparedCommand = strings.Replace(preparedCommand, "curl", "sqlmap", 1)
								preparedCommand = strings.Replace(preparedCommand, "-X "+strings.ToUpper(method), "--method="+strings.ToUpper(method)+" -u", 1)
								if bodyData != "" {
									preparedCommand = strings.Replace(preparedCommand, "-d '"+bodyData+"'", "--data='"+bodyData+"'", 1)
								}
								preparedCommand = "$ " + preparedCommand
							} else if prepareFor == "curl" {
								preparedCommand = "$ " + curl
							}
							fmt.Println(preparedCommand)
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
		writeLog(8899, "", "", "", "")
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
			return &SchemaNode{Type: "object"} // break cycle
		}
		visited[ref] = true

		// Resolve ref in the context spec (could be external)
		resolved, resolvedSpec := ResolveRefWithContext(contextSpec, ref)
		if resolved == nil {
			return &SchemaNode{Type: "object"}
		}
		// Continue expansion using the resolved spec as context
		return ExpandSchema(spec, resolved, visited, resolvedSpec)
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

	// Handle allOf (merge all schemas)
	if allOf, ok := schema["allOf"].([]interface{}); ok {
		merged := &SchemaNode{Type: "object", Properties: map[string]*SchemaNode{}, Required: map[string]bool{}}
		for _, entry := range allOf {
			if m, ok := entry.(map[string]interface{}); ok {
				sub := ExpandSchema(spec, m, visited, contextSpec)
				for k, v := range sub.Properties {
					merged.Properties[k] = v
				}
				for k, v := range sub.Required {
					merged.Required[k] = v
				}
			}
		}
		return merged
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

func GenerateRequests(bodyBytes []byte, client http.Client) {
	// Ingests the specification file
	spec := SafelyUnmarshalSpec(bodyBytes)

	// Checks defined security schemes and prompts for authentication
	CheckSecuritySchemes(spec)

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
									if strings.Contains(serverURL, "://") {
										fmt.Println(serverURL)
									} else {
										fmt.Println(apiTarget + serverURL)
									}
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
										log.Fatalf("Spec has relative server URL '%s' but no base URL available. Use -T to specify target server.", serverURL)
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
				log.Fatal("No server information found in spec and no URL provided. Use -T to specify target server.")
			}
		}
	}

	if os.Args[1] != "endpoints" {
		// Prints Title/Description/Version values if they exist
		PrintSpecInfo(spec)
	}

	// Reviews all defined API routes and builds requests as defined
	BuildRequestsFromPaths(spec, client)
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
		printInfo("No information defined in the documentation.\n")
	} else {
		title, ok := info["title"].(string)
		if ok && title != "" {
			if outputFormat == "json" {
				specTitle = title
			} else {
				fmt.Printf("Title: %s\n", title)
			}
		}

		description, ok := info["description"].(string)
		if ok && description != "" {
			if outputFormat == "json" {
				specDescription = description
			} else {
				fmt.Printf("Description: %s\n", description)
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

func SafelyUnmarshalSpec(data []byte) map[string]interface{} {

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Printf("Failed to unmarshal API documentation: %v\n", err)
		os.Exit(1)
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

	for k, v := range obj {
		switch val := v.(type) {
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
					b.WriteString(XmlFromObject(m))
				}
				b.WriteString("</" + k + ">")
			}
		default:
			b.WriteString(fmt.Sprintf("<%s>%v</%s>", k, val, k))
		}
	}

	return b.String()
}
