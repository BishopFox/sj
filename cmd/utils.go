package cmd

import (
	"bytes"
	"encoding/json"
	stdxml "encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var accessibleEndpoints []string
var jsonResultsStringArray []string
var jsonResultArray []Result
var jsonVerboseResultArray []VerboseResult
var suppressAutomateJSONFinalize bool
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

// IsAmbiguousResponse returns true if the HTTP status code indicates an ambiguous response
// that may benefit from modification in enhanced mode (4xx/5xx except 401, 403, 404).
func IsAmbiguousResponse(statusCode int) bool {
	// Treat all 4xx/5xx as ambiguous except clear authorization/not-found errors
	if statusCode >= 400 && statusCode < 600 {
		if statusCode == 401 || statusCode == 403 || statusCode == 404 {
			return false // Clear auth/not-found errors
		}
		return true
	}
	return false
}

// CreateMultipartBody creates a proper multipart/form-data body with boundary
// Returns the body bytes and the full Content-Type header value with boundary
func CreateMultipartBody(fields map[string]interface{}) ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for key, value := range fields {
		fieldWriter, err := writer.CreateFormField(key)
		if err != nil {
			return nil, "", err
		}
		_, err = fieldWriter.Write([]byte(fmt.Sprintf("%v", value)))
		if err != nil {
			return nil, "", err
		}
	}

	err := writer.Close()
	if err != nil {
		return nil, "", err
	}

	// Return body and Content-Type with boundary
	return body.Bytes(), writer.FormDataContentType(), nil
}

// getHeaderValue performs case-insensitive lookup of a header value
func getHeaderValue(headers map[string]string, headerName string) string {
	for key, value := range headers {
		if strings.EqualFold(key, headerName) {
			return value
		}
	}
	return ""
}

// setHeaderValue performs case-insensitive header replacement while preserving one canonical key.
func setHeaderValue(headers map[string]string, headerName, headerValue string) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}
	for key := range headers {
		if strings.EqualFold(key, headerName) {
			delete(headers, key)
		}
	}
	headers[headerName] = headerValue
	return headers
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func encodeFormBody(fields map[string]interface{}) string {
	values := url.Values{}
	for key, value := range fields {
		values.Set(key, fmt.Sprintf("%v", value))
	}
	return values.Encode()
}

func xmlText(value string) string {
	var b strings.Builder
	if err := stdxml.EscapeText(&b, []byte(value)); err != nil {
		return value
	}
	return b.String()
}

func xmlNodeValue(value interface{}) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		return XmlFromObject(typed)
	case []interface{}:
		return XmlFromValue(typed)
	case nil:
		return ""
	default:
		return xmlText(fmt.Sprintf("%v", typed))
	}
}

func XmlFromValue(value interface{}) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		return XmlFromObject(typed)
	case []interface{}:
		var b strings.Builder
		b.WriteString("<items>")
		for _, item := range typed {
			b.WriteString("<item>")
			b.WriteString(xmlNodeValue(item))
			b.WriteString("</item>")
		}
		b.WriteString("</items>")
		return b.String()
	case nil:
		return "<value></value>"
	default:
		return fmt.Sprintf("<value>%s</value>", xmlText(fmt.Sprintf("%v", typed)))
	}
}

// TryHTTPSUpgrade attempts to upgrade an HTTP target URL to HTTPS.
// It only upgrades if:
// - strictProtocol flag is false (HTTPS upgrade enabled)
// - target URL uses http:// (not already https://)
// - spec was retrieved via HTTPS OR target is on same host as spec
// - target URL doesn't have an explicit port (e.g., :8080)
// Returns the upgraded URL if HTTPS works, otherwise returns the original URL.
func TryHTTPSUpgrade(client http.Client, specURL, targetURL string) string {
	// Skip if strict protocol mode is enabled
	if strictProtocol {
		return targetURL
	}

	// Parse target URL
	parsedTarget, err := url.Parse(targetURL)
	if err != nil || parsedTarget.Scheme != "http" {
		return targetURL // Not HTTP or invalid, return as-is
	}

	// Check if target has explicit port (e.g., example.com:8080)
	// If so, don't upgrade - explicit port indicates specific endpoint
	if parsedTarget.Port() != "" {
		return targetURL
	}

	// Parse spec URL to determine if we should upgrade
	shouldUpgrade := false
	if specURL != "" {
		parsedSpec, err := url.Parse(specURL)
		if err == nil {
			// Upgrade if spec was retrieved via HTTPS
			if parsedSpec.Scheme == "https" {
				shouldUpgrade = true
			}
			// Also upgrade if target is on same host as spec (regardless of spec protocol)
			// Use Hostname() to exclude port, EqualFold for case-insensitive DNS comparison
			if strings.EqualFold(parsedSpec.Hostname(), parsedTarget.Hostname()) {
				shouldUpgrade = true
			}
		}
	}

	if !shouldUpgrade {
		return targetURL
	}

	// Construct HTTPS version of the URL
	httpsURL := strings.Replace(targetURL, "http://", "https://", 1)

	// Test HTTPS connectivity with HEAD request (5 second timeout)
	testClient := client
	testClient.Timeout = 5 * time.Second

	req, err := http.NewRequest("HEAD", httpsURL, nil)
	if err != nil {
		return targetURL
	}

	resp, err := testClient.Do(req)
	if err != nil {
		// HTTPS failed, fall back to HTTP silently
		return targetURL
	}
	defer resp.Body.Close()

	// HTTPS works! Use it
	if !quiet {
		log.Infof("Upgraded HTTP target to HTTPS: %s", httpsURL)
	}
	return httpsURL
}

func BuildRequestsFromPaths(spec map[string]interface{}, client http.Client, apiTarget string, enhanced bool, maxRetries int) {
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
						// Initialize parameter tracking for enhanced mode
						pathParams := make(map[string]string)
						queryParams := make(map[string]string)
						requestHeaders := make(map[string]string)
						requestBodyMap := make(map[string]interface{})
						var requestBody interface{}
						var requestContentType string

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
										in, ok := pMap["in"].(string)
										if !ok {
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
														for propertyItem, propertyValue := range exampleMap {
															pVal := fmt.Sprintf("%v", propertyValue)
															if strings.Contains(curl, "?") || strings.Contains(targetURL, "?") {
																targetURL += fmt.Sprintf("&%s=%s", propertyItem, pVal)
															} else {
																targetURL += fmt.Sprintf("?%s=%s", propertyItem, pVal)
															}
															// Track in queryParams map for enhanced mode
															queryParams[propertyItem] = pVal
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
															// Track in requestBodyMap for enhanced mode
															requestBodyMap[propertyItem] = propertyValue
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
												queryParams[name] = pValue
											case "path":
												targetURL = strings.Replace(targetURL, "{"+name+"}", pValue, 1)
												pathParams[name] = pValue
											case "header":
												curl += fmt.Sprintf(" -H \"%s: %s\"", name, pValue)
												requestHeaders[name] = pValue
											case "body":
												if strings.Contains(curl, "-d '") {
													bodyData += fmt.Sprintf("&%s=%s", name, pValue)
													curl = strings.TrimSuffix(curl, "'")
													curl += fmt.Sprintf("&%s=%s'", name, pValue)
												} else {
													bodyData += fmt.Sprintf("%s=%s", name, pValue)
													curl += fmt.Sprintf(" -d '%s=%s'", name, pValue)
												}
												requestBodyMap[name] = pValue
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
													// Store the full example, not just objects
													requestBody = example
													requestContentType = cType
												}
											}
											if cType == "application/xml" || cType == "text/xml" {
												xml := XmlFromValue(example)
												curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", cType, xml)
												requestBody = example
												requestContentType = cType
											}
											if cType == "application/x-www-form-urlencoded" {
												if obj, ok := example.(map[string]interface{}); ok {
													formData := encodeFormBody(obj)
													curl += fmt.Sprintf(" -H \"Content-Type: %s\" -d '%s'", cType, formData)
													requestBody = obj
													requestContentType = cType
												}
											}
											if cType == "multipart/form-data" {
												if obj, ok := example.(map[string]interface{}); ok {
													// For multipart, we'll store the object and encode it properly when making the request
													// Curl representation shows simplified form for display
													formData := encodeFormBody(obj)
													curl += fmt.Sprintf(" -H \"Content-Type: multipart/form-data\" -d '%s'", formData)
													requestBody = obj
													requestContentType = cType
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

						// Handle endpoints command separately (doesn't need full URL parsing)
						if currentCommand == "endpoints" {
							fmt.Println(basePath + pathName)
							continue
						}

						logURL, parseErr := url.Parse(targetURL)
						if parseErr != nil || logURL == nil {
							log.Printf("Error parsing URL '%s': %v - skipping endpoint.", targetURL, parseErr)
							continue
						}
						switch currentCommand {
						case "automate":
							var postBodyData string
							var bodyReader io.Reader
							if strings.Contains(curl, "-d") {
								dataIndex := strings.Index(curl, "'")
								postBodyData = curl[dataIndex+1 : len(curl)-1]

								// Handle multipart/form-data encoding properly
								if requestContentType == "multipart/form-data" && requestBody != nil {
									if bodyMap, ok := requestBody.(map[string]interface{}); ok {
										multipartBody, multipartCT, err := CreateMultipartBody(bodyMap)
										if err == nil {
											bodyReader = bytes.NewReader(multipartBody)
											// Update Content-Type header with boundary
											requestHeaders = setHeaderValue(requestHeaders, "Content-Type", multipartCT)
										} else {
											// Fallback to plain text on error
											bodyReader = bytes.NewReader([]byte(postBodyData))
										}
									} else {
										bodyReader = bytes.NewReader([]byte(postBodyData))
									}
								} else {
									bodyReader = bytes.NewReader([]byte(postBodyData))
								}
							} else {
								bodyReader = bytes.NewReader([]byte{})
							}

							// Make initial request with custom headers
							_, resp, sc := MakeRequestWithHeaders(client, strings.ToUpper(method), targetURL, timeout, bodyReader, requestHeaders)

							// Enhanced mode: iterative loop for ambiguous responses
							if enhanced {
								// Track current values for iteration
								currentResp := resp
								currentSC := sc
								currentTargetURL := targetURL
								attemptCount := 1
								currentMethod := strings.ToUpper(method)
								currentPathParams := cloneStringMap(pathParams)
								currentQueryParams := cloneStringMap(queryParams)
								currentHeaders := cloneStringMap(requestHeaders)
								var currentBody interface{}
								if requestBody != nil {
									currentBody = requestBody
								} else if len(requestBodyMap) > 0 {
									currentBody = requestBodyMap
								}
								currentContentType := requestContentType

								for IsAmbiguousResponse(currentSC) {
									// Rebuild state from the most recently modified request so changes persist across attempts.
									state := NewRequestState(
										currentMethod,
										pathName,
										cloneStringMap(currentPathParams),
										cloneStringMap(currentQueryParams),
										cloneStringMap(currentHeaders),
										currentBody,
										currentContentType,
									)
									state.AttemptNumber = attemptCount

									// Enter interactive modification loop
									shouldResend, shouldQuit := InteractiveModifyLoop(
										state,
										currentSC,
										currentResp,
										maxRetries,
										spec,
										opMap,
										apiTarget,
										basePath,
									)

									if shouldQuit {
										return // User requested to quit
									}

									if !shouldResend {
										break // User chose to move to next endpoint
									}

									// Persist user changes so future ambiguous loops keep modified method/body/headers.
									currentMethod = state.Method
									currentPathParams = cloneStringMap(state.PathParams)
									currentQueryParams = cloneStringMap(state.QueryParams)
									currentHeaders = cloneStringMap(state.Headers)
									currentBody = state.Body
									currentContentType = state.ContentType
									attemptCount = state.AttemptNumber

									// Stop before sending if the next resend would exceed max retries.
									if attemptCount > maxRetries {
										fmt.Printf("\n=== MAX RETRIES REACHED (%d/%d) ===\n", maxRetries, maxRetries)
										fmt.Println("Auto-advancing to next endpoint.")
										break
									}

									// Rebuild request from modified state
									// FIX: Pass apiTarget (not targetURL) to BuildURL
									modifiedURL := state.BuildURL(apiTarget, basePath)
									currentTargetURL = modifiedURL

									// Prepare body for resend
									var modifiedBodyReader io.Reader
									if state.Body != nil {
										// Get current Content-Type from headers (may have been changed)
										effectiveContentType := getHeaderValue(state.Headers, "Content-Type")
										if effectiveContentType == "" {
											effectiveContentType = state.ContentType
										}

										// Handle object bodies
										if bodyMap, ok := state.Body.(map[string]interface{}); ok && len(bodyMap) > 0 {
											if strings.Contains(effectiveContentType, "application/json") {
												bodyBytes, err := json.Marshal(bodyMap)
												if err == nil {
													modifiedBodyReader = bytes.NewReader(bodyBytes)
												}
											} else if strings.Contains(effectiveContentType, "application/xml") || strings.Contains(effectiveContentType, "text/xml") {
												modifiedBodyReader = bytes.NewReader([]byte(XmlFromValue(bodyMap)))
											} else if strings.Contains(effectiveContentType, "application/x-www-form-urlencoded") {
												// URL-encoded form data
												formData := encodeFormBody(bodyMap)
												modifiedBodyReader = bytes.NewReader([]byte(formData))
											} else if strings.Contains(effectiveContentType, "multipart/form-data") {
												// Proper multipart encoding with boundary
												multipartBody, multipartCT, err := CreateMultipartBody(bodyMap)
												if err == nil {
													modifiedBodyReader = bytes.NewReader(multipartBody)
													// Update Content-Type header with boundary in state
													state.Headers = setHeaderValue(state.Headers, "Content-Type", multipartCT)
												} else {
													// Fallback to URL-encoded on error
													formData := encodeFormBody(bodyMap)
													modifiedBodyReader = bytes.NewReader([]byte(formData))
												}
											} else {
												// Default to JSON for unknown content types with object bodies
												bodyBytes, err := json.Marshal(bodyMap)
												if err == nil {
													modifiedBodyReader = bytes.NewReader(bodyBytes)
												}
											}
										} else {
											// Handle non-object JSON bodies (arrays, scalars, etc.)
											// These should be serialized as-is according to Content-Type
											if strings.Contains(effectiveContentType, "application/json") {
												bodyBytes, err := json.Marshal(state.Body)
												if err == nil {
													modifiedBodyReader = bytes.NewReader(bodyBytes)
												}
											} else if strings.Contains(effectiveContentType, "application/xml") || strings.Contains(effectiveContentType, "text/xml") {
												modifiedBodyReader = bytes.NewReader([]byte(XmlFromValue(state.Body)))
											} else {
												// For other content types, serialize as JSON
												bodyBytes, err := json.Marshal(state.Body)
												if err == nil {
													modifiedBodyReader = bytes.NewReader(bodyBytes)
												}
											}
										}
									}

									// Make request with modified parameters and custom headers
									_, currentResp, currentSC = MakeRequestWithHeaders(client, state.Method, modifiedURL, timeout, modifiedBodyReader, state.Headers)

									// Keep state synchronized for the next ambiguous loop.
									currentMethod = state.Method
									currentPathParams = cloneStringMap(state.PathParams)
									currentQueryParams = cloneStringMap(state.QueryParams)
									currentHeaders = cloneStringMap(state.Headers)
									currentBody = state.Body
									currentContentType = state.ContentType

									// Update final values for logging below
									resp = currentResp
									sc = currentSC
									targetURL = currentTargetURL
								}
							}

							tempResponsePreviewLength := responsePreviewLength
							if len(resp) <= responsePreviewLength {
								tempResponsePreviewLength = len(resp)
							}

							var result []byte

							if verbose {
								// Try to parse preview as JSON if content-type indicates JSON
								var preview interface{}
								if strings.Contains(strings.ToLower(responseContentType), "application/json") {
									var previewObj interface{}
									if err := json.Unmarshal([]byte(resp[:tempResponsePreviewLength]), &previewObj); err == nil {
										preview = previewObj
									} else {
										// JSON content-type but invalid JSON or truncated, keep as string
										preview = string(resp[:tempResponsePreviewLength])
									}
								} else {
									preview = string(resp[:tempResponsePreviewLength])
								}
								result, _ = json.Marshal(VerboseResult{Method: method, Preview: preview, Status: sc, ContentType: responseContentType, Target: logURL.Path, Curl: curl})
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
	if currentCommand == "automate" && outputFormat == "json" {
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
		if !suppressAutomateJSONFinalize {
			writeLog(8899, "", "", "", "")
		}
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

func GenerateRequests(bodyBytes []byte, client http.Client, enhanced bool, maxRetries int) {
	// Ingests the specification file
	spec := SafelyUnmarshalSpec(bodyBytes)

	// Checks defined security schemes and prompts for authentication
	// Skip for endpoints command since we're just listing paths, not making requests
	if currentCommand != "endpoints" {
		CheckSecuritySchemes(spec)
	}

	u, parseErr := url.Parse(swaggerURL)
	if parseErr != nil {
		u = &url.URL{}
	}

	// Track if user manually set target with -T flag
	userSetTarget := apiTarget != ""

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
				if !quiet && (currentCommand != "endpoints") && apiTarget == "" {
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
									if currentCommand != "endpoints" {
										log.Fatalf("Spec has relative server URL '%s' but no base URL available. Use -T to specify target server.", serverURL)
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
			if currentCommand != "endpoints" {
				log.Fatal("No server information found in spec and no URL provided. Use -T to specify target server.")
			}
		}
	}

	// Attempt HTTPS upgrade if target was extracted from spec (not user-provided with -T)
	if !userSetTarget && apiTarget != "" {
		apiTarget = TryHTTPSUpgrade(client, swaggerURL, apiTarget)
	}

	if currentCommand != "endpoints" {
		// Prints Title/Description/Version values if they exist
		PrintSpecInfo(spec)
	}

	// Reviews all defined API routes and builds requests as defined
	BuildRequestsFromPaths(spec, client, apiTarget, enhanced, maxRetries)
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
			// Validate path is within baseDir (prevent traversal)
			cleanBase := filepath.Clean(baseDir)
			if !strings.HasPrefix(fullPath, cleanBase+string(filepath.Separator)) && fullPath != cleanBase {
				// Path traversal attempt detected, return without external spec context
				return resolved, spec
			}
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

	// Validate path is within baseDir (prevent path traversal attacks)
	cleanBase := filepath.Clean(baseDir)
	if !strings.HasPrefix(filePath, cleanBase+string(filepath.Separator)) && filePath != cleanBase {
		// Path traversal attempt detected
		return nil
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
				b.WriteString(xmlNodeValue(item))
				b.WriteString("</" + k + ">")
			}
		default:
			b.WriteString(fmt.Sprintf("<%s>%s</%s>", k, xmlText(fmt.Sprintf("%v", val)), k))
		}
	}

	return b.String()
}
