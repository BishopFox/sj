package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
									if ref, hasRef := pMap["$ref"].(string); hasRef {
										resolved := ResolveRef(spec, ref)
										if resolved != nil {
											pMap = resolved
										}
									}

									if name, ok := pMap["name"].(string); ok {
										in := pMap["in"].(string)

										// Handle schema-based parameters (OpenAPI v3 and some v2)
										if schema, ok := pMap["schema"].(map[string]interface{}); ok {
											expanded := ExpandSchema(spec, schema, map[string]bool{})

											// Generate example value from expanded schema
											if expanded.Type == "object" || len(expanded.Properties) > 0 {
												// For object schemas, generate full example and serialize
												example := GenerateExample(expanded)
												if exampleMap, ok := example.(map[string]interface{}); ok {
													for propertyItem, propertyValue := range exampleMap {
														pValue = fmt.Sprintf("%v", propertyValue)
														if strings.Contains(curl, "-d '") {
															bodyData += fmt.Sprintf("&%s=%s", propertyItem, pValue)
															curl = strings.TrimSuffix(curl, "'")
															curl += fmt.Sprintf("&%s=%s'", propertyItem, pValue)
														} else {
															bodyData += fmt.Sprintf("%s=%s", propertyItem, pValue)
															curl += fmt.Sprintf(" -d '%s=%s'", propertyItem, pValue)
														}
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
											if pType == "string" && name != "version" {
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

										// Only process query/path/header parameters with the switch
										// Body parameters with object schemas were already handled above
										if !(in == "body" && len(pValue) == 0) {
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
							if ref, hasRef := reqBody["$ref"].(string); hasRef {
								resolved := ResolveRef(spec, ref)
								if resolved != nil {
									reqBody = resolved
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
											expanded := ExpandSchema(spec, schema, map[string]bool{})
											example := GenerateExample(expanded)

											if cType == "application/json" {
												bodyBytes, err := json.Marshal(example)
												if err == nil {
													curl += fmt.Sprintf(" -H \"Content-Type: application/json\" -d '%s'\"", bodyBytes)
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
) *SchemaNode {
	if schema == nil {
		return &SchemaNode{Type: "object"}
	}

	if ref, ok := schema["$ref"].(string); ok {
		if visited[ref] {
			return &SchemaNode{Type: "object"} // break cycle
		}
		visited[ref] = true

		resolved := ResolveRef(spec, ref)
		if resolved == nil {
			return &SchemaNode{Type: "object"}
		}
		return ExpandSchema(spec, resolved, visited)
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
				node.Properties[name] = ExpandSchema(spec, m, visited)
			}
		}
	}

	if items, ok := schema["items"].(map[string]interface{}); ok {
		node.Items = ExpandSchema(spec, items, visited)
	}

	// Handle additionalProperties
	if addProps, ok := schema["additionalProperties"]; ok {
		if addPropsMap, ok := addProps.(map[string]interface{}); ok {
			node.AdditionalProperties = ExpandSchema(spec, addPropsMap, visited)
		}
	}

	// Handle allOf (merge all schemas)
	if allOf, ok := schema["allOf"].([]interface{}); ok {
		merged := &SchemaNode{Type: "object", Properties: map[string]*SchemaNode{}, Required: map[string]bool{}}
		for _, entry := range allOf {
			if m, ok := entry.(map[string]interface{}); ok {
				sub := ExpandSchema(spec, m, visited)
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
				node.OneOf = append(node.OneOf, ExpandSchema(spec, m, visited))
			}
		}
	}

	// Handle anyOf (expand all options)
	if anyOf, ok := schema["anyOf"].([]interface{}); ok {
		for _, entry := range anyOf {
			if m, ok := entry.(map[string]interface{}); ok {
				node.AnyOf = append(node.AnyOf, ExpandSchema(spec, m, visited))
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

	// Gets the target server and base path from the specification file
	if apiTarget == "" {
		if v, ok := spec["swagger"].(string); ok && strings.HasPrefix(v, "2") {
			// Swagger (v2)
			host, _ := spec["host"].(string)
			bp, _ := spec["basePath"].(string)
			if bp == "/" {
				basePath = ""
			} else if bp != "" {
				basePath = bp
			}

			if host != "" && strings.Contains(host, "://") {
				apiTarget = host
			} else {
				if host != "" {
					apiTarget = u.Scheme + "://" + host
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
								apiTarget = serverURL
							} else if serverURL == "/" {
								basePath = ""
							} else {
								basePath = serverURL
								apiTarget = u.Scheme + "://" + u.Host
							}
						}
					}
				}
			}
		}
	}

	// Use the original host at the target if no server found from specification.
	if apiTarget == "" {
		apiTarget = u.Scheme + "://" + u.Host
	}

	if os.Args[1] != "endpoints" {
		// Prints Title/Description/Version values if they exist
		PrintSpecInfo(spec)
	}

	// Reviews all defined API routes and builds requests as defined
	BuildRequestsFromPaths(spec, client)
}

func ResolveRef(spec map[string]interface{}, ref string) map[string]interface{} {
	// Handle external references (e.g., "./schemas/user.yaml#/User")
	if !strings.HasPrefix(ref, "#") {
		return ResolveExternalRef(ref)
	}

	parts := strings.Split(ref[2:], "/")
	var cur interface{} = spec

	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[p]
	}

	resolved, _ := cur.(map[string]interface{})
	return resolved
}

func ResolveExternalRef(ref string) map[string]interface{} {
	// Parse external reference format: "<file_path>#<json_pointer>"
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) < 1 {
		return nil
	}

	filePath := parts[0]
	var jsonPointer string
	if len(parts) == 2 {
		jsonPointer = parts[1]
	}

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
