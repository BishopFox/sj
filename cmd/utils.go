package cmd

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var accessibleEndpoints []string
var jsonResultsStringArray []string
var jsonResultArray []Result
var jsonVerboseResultArray []VerboseResult
var specTitle string
var specDescription string

func PrintSpecInfo(spec map[string]interface{}) {
	info, ok := spec["info"].(map[string]interface{})
	if !ok || info == nil {
		log.Info("No information defined in the documentation.")
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

func GenerateRequests(bodyBytes []byte, client http.Client) {
	// Ingests the specification file
	spec := SafelyUnmarshalSpec(bodyBytes)

	// Checks defined security schemes and prompts for authentication
	CheckSecuritySchemes(spec)

	u, _ := url.Parse(swaggerURL)

	// Gets the target server and base path from the specification file
	if apiTarget == "" {
		if v, ok := spec["swagger"].(string); ok && strings.HasPrefix(v, "2") {
			// Swagger (v2)
			host, _ := spec["host"].(string)
			bp, _ := spec["basePath"].(string)
			if bp != "" {
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
						log.Warn("Multiple servers detected in documentation. You can manually set a server to test with the -T flag.\nThe detected servers are as follows:")
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
							} else {
								basePath = serverURL
								apiTarget = u.Scheme + "://" + u.Host + serverURL
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

func BuildRequestsFromPaths(spec map[string]interface{}, client http.Client) {
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || paths == nil {
		log.Fatalf("Could not find any defined operations. Review the file manually.")
	}

	var errorDescriptions = make(map[any]string)
	for pathName, pathItem := range paths {
		if ops, ok := pathItem.(map[string]interface{}); ok {
			for method, op := range ops {
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
								var pType string
								var pValue string
								if pMap, ok := p.(map[string]interface{}); ok {
									if name, ok := pMap["name"].(string); ok {
										in := pMap["in"].(string)
										if schema, ok := pMap["schema"].(map[string]interface{}); ok {
											if schema["type"] != nil {
												pType = schema["type"].(string)
												// Attempt to prevent version strings from being improperly supplied (i.e. v1)
												if pType == "string" && pMap["name"] != "version" {
													pValue = "bishopfox"
												} else {
													pValue = "1"
												}
											} else if schema["$ref"] != nil {
												ref := schema["$ref"].(string)
												if defs, ok := spec["definitions"].(map[string]interface{}); ok {
													schemaName := strings.TrimPrefix(ref, "#/definitions/")
													if s, ok := defs[schemaName].(map[string]interface{}); ok {
														if properties, ok := s["properties"].(map[string]interface{}); ok {
															for propertyItem := range properties {
																var pValue string
																if propertyName, ok := properties[propertyItem].(map[string]interface{}); ok {
																	if exampleValue, ok := propertyName["example"]; ok {
																		pValue = exampleValue.(string)
																	} else if propertyType, ok := propertyName["type"].(string); ok {
																		// Attempt to prevent version strings from being improperly supplied (i.e. v1)
																		if propertyType == "string" && propertyName["name"] != "version" {
																			pValue = "bishopfox"
																		} else {
																			pValue = "1"
																		}

																	}

																	if strings.Contains(curl, "-d") {
																		bodyData += fmt.Sprintf("&%s=%s", propertyItem, pValue)
																		curl = strings.TrimSuffix(curl, "'")
																		curl += fmt.Sprintf("&%s=%s'", propertyItem, pValue)
																	} else {
																		bodyData += fmt.Sprintf("%s=%s", propertyItem, pValue)
																		curl += fmt.Sprintf(" -d '%s=%s'", propertyItem, pValue)
																	}

																}
															}
														}
													}
												}
											}

										} else if pType, ok := pMap["type"].(string); ok {
											// Attempt to prevent version strings from being improperly supplied (i.e. v1)
											if pType == "string" && pMap["name"] != "version" {
												pValue = "bishopfox"
											} else {
												pValue = "1"
											}
										}

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
											if strings.Contains(curl, "-d") {
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

						// Extracts the expected parameters from the requestBody object
						if reqBody, ok := opMap["requestBody"].(map[string]interface{}); ok {
							if contentTypes, ok := reqBody["content"].(map[string]interface{}); ok {
								for cType := range contentTypes {
									if contentType == "" {
										EnforceSingleContentType(cType)
									} else {
										EnforceSingleContentType(contentType)
									}

									if ct, ok := contentTypes[cType].(map[string]interface{}); ok {
										if schema, ok := ct["schema"].(map[string]interface{}); ok {
											if ref, ok := schema["$ref"].(string); ok {
												if components, ok := spec["components"].(map[string]interface{}); ok {
													if schemas, ok := components["schemas"].(map[string]interface{}); ok {
														schemaName := strings.TrimPrefix(ref, "#/components/schemas/")
														if s, ok := schemas[schemaName].(map[string]interface{}); ok {
															if properties, ok := s["properties"].(map[string]interface{}); ok {

																for propertyItem := range properties {
																	var pValue string
																	if propertyName, ok := properties[propertyItem].(map[string]interface{}); ok {
																		if propertyType, ok := propertyName["type"].(string); ok {
																			switch propertyType {
																			case "string":
																				// Attempt to prevent version strings from being improperly supplied (i.e. v1)
																				if propertyName["name"] != "version" {
																					pValue = "bishopfox"
																				} else {
																					pValue = "1"
																				}
																			case "int":
																				pValue = "1"
																			default:
																				pValue = "1"
																			}

																			switch cType {
																			case "application/json":
																				if strings.Contains(curl, "-d") && strings.Contains(curl, "application/json") {
																					bodyData = ""
																					curl = strings.TrimSuffix(curl, "}'")
																					if propertyType == "string" {
																						bodyData += fmt.Sprintf(",\"%s\":\"%s\"}'", propertyItem, pValue)
																					} else {
																						bodyData += fmt.Sprintf(",\"%s\":%s}'", propertyItem, pValue)
																					}
																					curl += bodyData
																				} else if !strings.Contains(curl, "Content-Type") {
																					if propertyType == "string" {
																						bodyData += fmt.Sprintf("{\"%s\":\"%s\"}", propertyItem, pValue)
																					} else {
																						bodyData += fmt.Sprintf("{\"%s\":%s}", propertyItem, pValue)
																					}

																					curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", contentType, bodyData)
																				}
																			case "application/xml", "text/xml":
																				bodyData = ""
																				type Element struct {
																					XMLName xml.Name
																					Content any `xml:",chardata"`
																				}

																				type Root struct {
																					XMLName  xml.Name  `xml:"root"`
																					Elements []Element `xml:",any"`
																				}

																				var elements []Element
																				elements = append(elements, Element{
																					XMLName: xml.Name{Local: propertyItem},
																					Content: pValue,
																				})

																				root := Root{
																					Elements: elements,
																				}

																				xmlData, err := xml.Marshal(root)
																				if err != nil {
																					log.Warn("Error marshalling XML data.")
																				}

																				if strings.Contains(curl, "-d") && strings.Contains(curl, "xml") {
																					curl = strings.TrimSuffix(curl, "</root>'")
																					bodyData = strings.TrimSuffix(bodyData, "</root>'")
																					bodyData += strings.TrimPrefix(string(xmlData), "<root>")
																					curl += bodyData + "'"
																				} else if !strings.Contains(curl, "Content-Type") {
																					bodyData = string(xmlData)
																					curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", contentType, bodyData)
																				}
																			}
																		}
																	}
																}
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}

						logURL, _ := url.Parse(targetURL)
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
										writeLog(sc, basePath+logURL.Path, strings.ToUpper(method), errorDescriptions[sc], resp[:tempResponsePreviewLength])
									}
								}
							} else {
								if jsonResultsStringArray == nil {
									jsonResultsStringArray = append(jsonResultsStringArray, string(result))
								} else {
									jsonResultsStringArray = append(jsonResultsStringArray, ","+string(result))
								}
								if outputFormat == "console" {
									writeLog(sc, basePath+logURL.Path, strings.ToUpper(method), errorDescriptions[sc], resp[:tempResponsePreviewLength])
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
		slices.Sort(jsonResultsStringArray)
		for r := range jsonResultsStringArray {
			var result Result
			var verboseResult VerboseResult
			if verbose {
				err := json.Unmarshal([]byte(strings.TrimPrefix(jsonResultsStringArray[r], ",")), &verboseResult)
				if err != nil {
					log.Fatal("Error marshalling JSON:", err)
				}
				jsonVerboseResultArray = append(jsonVerboseResultArray, verboseResult)
			} else {
				err := json.Unmarshal([]byte(strings.TrimPrefix(jsonResultsStringArray[r], ",")), &result)
				if err != nil {
					log.Fatal("Error marshalling JSON:", err)
				}
				jsonResultArray = append(jsonResultArray, result)
			}
		}
		writeLog(8899, "", "", "", "")
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

func SafelyUnmarshalSpec(data []byte) map[string]interface{} {

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Printf("Failed to unmarshal API documentation: %v\n", err)
		os.Exit(1)
	}

	return doc
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
