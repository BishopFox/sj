package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v3"
)

type SwaggerRequest struct {
	ApiKey      string
	ApiKeyName  string
	ApiInQuery  bool
	BasePath    string
	Body        map[string]any
	BodyData    []byte
	Def         *openapi3.T
	Path        string
	Paths       []string
	Query       url.Values
	RawQuery    string
	ResultsJSON []string
	URL         url.URL
}

var accessibleEndpoints []string
var consoleResults map[int][]any
var jsonResultsStringArray []string
var jsonResultArray []Result
var jsonVerboseResultArray []VerboseResult

func GenerateRequests(bodyBytes []byte, client http.Client) []string {

	var s SwaggerRequest
	s.Def = UnmarshalSpec(bodyBytes)
	s.ApiInQuery, s.ApiKey, s.ApiKeyName = CheckSecDefs(*s.Def)
	u, _ := url.Parse(swaggerURL)
	s.URL = *u

	if os.Args[1] != "endpoints" {
		// Prints Title/Description values if they exist
		PrintSpecInfo(*s.Def.Info)
	}

	if s.Def.Paths == nil {
		log.Fatalf("Could not find any defined operations. Review the file manually.")
	}

	if len(s.Def.Servers) > 1 {
		if !quiet && (os.Args[1] != "endpoints") {
			if apiTarget == "" {
				log.Warn("Multiple servers detected in documentation. You can manually set a server to test with the -T flag.\nThe detected servers are as follows:")
				for i, server := range s.Def.Servers {
					fmt.Printf("Server %d: %s\n", i+1, server.URL)
				}
				fmt.Println()
			}
		}
		if apiTarget == "" {
			for _, server := range s.Def.Servers {
				if outputFormat == "console" && os.Args[1] == "automate" {
					log.Warnf("Results for %s:\n", server.URL)
				}

				if server.URL == "/" {
					s.Path = ""
				}

				if strings.Contains(server.URL, "localhost") || strings.Contains(server.URL, "127.0.0.1") || strings.Contains(server.URL, "::1") {
					log.Warn("The server(s) documented in the definition file contain(s) a local host value and may result in errors. Supply a target manually using the '-T' flag.")
				}

				u, _ := url.Parse(server.URL)
				s.URL = *u
				s = s.IterateOverPaths(client)
				fmt.Println()
			}
		} else {
			if apiTarget != "" {
				u, _ = url.Parse(apiTarget)
			}
			s.URL = *u
			s = s.IterateOverPaths(client)
		}
	} else {
		if apiTarget != "" {
			u, _ = url.Parse(apiTarget)
		} else {
			if swaggerURL == "" {
				u, _ = url.Parse(s.Def.Servers[0].URL)
				if strings.Contains(s.Def.Servers[0].URL, "localhost") || strings.Contains(s.Def.Servers[0].URL, "127.0.0.1") || strings.Contains(s.Def.Servers[0].URL, "::1") {
					log.Warn("The server documented in the definition file contains a local host value and may result in errors. Supply a target manually using the '-T' flag.")
				}
			}
		}
		s.URL = *u
		s = s.IterateOverPaths(client)
	}

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

	if os.Args[1] == "automate" {
		if outfile == "" && getAccessibleEndpoints && strings.ToLower(outputFormat) == "console" {
			var isDuplicateEndpoint bool
			var printedEndpoints []string
			if accessibleEndpoints != nil {
				log.Infof("Accessible endpoints:\n")
				for i := range accessibleEndpoints {
					for j := range printedEndpoints {
						if accessibleEndpoints[i] == printedEndpoints[j] {
							isDuplicateEndpoint = true
						}
					}
					if !isDuplicateEndpoint {
						fmt.Printf("    %s\n", accessibleEndpoints[i])
						printedEndpoints = append(printedEndpoints, accessibleEndpoints[i])
					}
					isDuplicateEndpoint = false
				}
			}
		}
		if strings.ToLower(outputFormat) == "json" {
			writeLog(8899, s.Def.Info.Title, s.Def.Info.Description, "", "") // This is a temporary workaround to get the logging to work as intended.. needs to be fixed eventually.
		} else if strings.ToLower(outputFormat) == "console" && !getAccessibleEndpoints {
			var r []string
			for i := range consoleResults {
				tempString := fmt.Sprintf("%s,%s,%d,%s,%s", consoleResults[i][1], consoleResults[i][2], consoleResults[i][0], consoleResults[i][3], consoleResults[i][4])
				r = append(r, tempString)
			}
			sort.Strings(r)
			for i := range r {
				result := r[i]
				i1 := strings.Index(r[i], ",")
				endpoint := result[:i1]
				result = result[i1+1:]

				i2 := strings.Index(result, ",")
				method := result[:i2]
				result = result[i2+1:]

				i3 := strings.Index(result, ",")
				statusCode, _ := strconv.Atoi(result[:i3])
				result = result[i3+1:]

				i4 := strings.Index(result, ",")
				errorMessage := result[:i4]
				result = result[i4+1:]

				responsePreview := result

				writeLog(statusCode, endpoint, method, errorMessage, responsePreview)
			}
		}
	}
	return s.Paths
}

func (s SwaggerRequest) IterateOverPaths(client http.Client) SwaggerRequest {
	for path, pathItem := range s.Def.Paths {
		operations := map[string]*openapi3.Operation{
			"CONNECT": pathItem.Connect,
			"GET":     pathItem.Get,
			"HEAD":    pathItem.Head,
			"OPTIONS": pathItem.Options,
			"PATCH":   pathItem.Patch,
			"POST":    pathItem.Post,
			"PUT":     pathItem.Put,
			"TRACE":   pathItem.Trace,
		}

		var rateLimiter = rate.NewLimiter(rate.Every(time.Second/(time.Duration(rateLimit))), 1)

		for method, op := range operations {
			// Do all the things here :D
			s.BodyData = nil
			if rateLimit > 0 {
				if err := rateLimiter.Wait(context.Background()); err != nil {
					fmt.Println("Rate limit error...")
				} else {
					if op != nil {
						s.URL.Path = s.Path + path
						s = s.BuildDefinedRequests(client, method, pathItem, op)
					}
				}
			}
		}
	}
	return s
}

func (s SwaggerRequest) BuildDefinedRequests(client http.Client, method string, pathItem *openapi3.PathItem, op *openapi3.Operation) SwaggerRequest {
	s.ApiInQuery = false
	b := make(map[string]any)
	s.Body = b
	s.Query = url.Values{}

	var pathMap = make(map[string]bool)

	basePathResult := s.GetBasePath()
	s.URL.Path = basePathResult + s.URL.Path

	s = s.AddParametersToRequest(op)

	var errorDescriptions = make(map[any]string)
	for status := range op.Responses {
		if op.Responses[status].Ref == "" {
			if op.Responses[status].Value == nil {
				continue
			} else {
				errorDescriptions[status] = *op.Responses[status].Value.Description
			}
		} else {
			continue
		}
	}

	s.URL.RawQuery = s.Query.Encode()
	if os.Args[1] == "automate" {
		tempResponsePreviewLength := responsePreviewLength
		_, resp, sc := MakeRequest(client, method, s.URL.String(), timeout, bytes.NewReader(s.BodyData))
		if len(resp) < responsePreviewLength {
			tempResponsePreviewLength = len(resp)
		}
		if sc == 200 {
			accessibleEndpoints = append(accessibleEndpoints, s.URL.String())
		}
		if strings.ToLower(outputFormat) != "console" {
			curlCommand := fmt.Sprintf("curl -X %s -sk %s", method, s.URL.String())
			if s.BodyData != nil {
				curlCommand = fmt.Sprintf("%s -d '%s'", curlCommand, string(s.BodyData))
			}
			if len(Headers) != 0 {
				curlCommand = fmt.Sprintf("%s -H '%s'", curlCommand, strings.Join(Headers, "' -H '"))
			}
			var result []byte
			if getAccessibleEndpoints {
				if sc == 200 {
					if verbose {
						result, _ = json.Marshal(VerboseResult{Method: method, Preview: resp[:tempResponsePreviewLength], Status: sc, Target: s.URL.String(), Curl: curlCommand})
					} else {
						result, _ = json.Marshal(Result{Method: method, Status: sc, Target: s.URL.String()})
					}
					if jsonResultsStringArray == nil {
						jsonResultsStringArray = append(jsonResultsStringArray, string(result))
					} else {
						jsonResultsStringArray = append(jsonResultsStringArray, ","+string(result))
					}
				}
			} else {
				if verbose {
					result, _ = json.Marshal(VerboseResult{Method: method, Preview: resp[:tempResponsePreviewLength], Status: sc, Target: s.URL.String(), Curl: curlCommand})
				} else {
					result, _ = json.Marshal(Result{Method: method, Status: sc, Target: s.URL.String()})
				}
				jsonResultsStringArray = append(jsonResultsStringArray, string(result))
			}
		} else {
			if !getAccessibleEndpoints {
				if consoleResults == nil {
					consoleResults = make(map[int][]any)
				}
				n := len(consoleResults)
				if n != 0 {
					n = n + 1
				}
				var r []any
				r = append(r, sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)], resp)

				consoleResults[n] = r
			}
		}
	} else if os.Args[1] == "prepare" {
		s.PrintPreparedCommands(method)
	} else if os.Args[1] == "endpoints" {

		for k := range s.Def.Paths {
			if !pathMap[k] {
				s.Paths = append(s.Paths, basePathResult+k)
				pathMap[k] = true
			}
		}
	}
	return s
}

// This whole function needs to be refactored/cleaned up a bit
func (s SwaggerRequest) AddParametersToRequest(op *openapi3.Operation) SwaggerRequest {
	for _, param := range op.Parameters {
		if param.Value == nil {
			continue
		} else if param.Value.In == "path" {
			if param.Value.Schema != nil {
				if param.Value.Schema.Ref != "" {
					s = s.SetParametersFromSchema(param, "path", param.Value.Schema.Ref, nil, 0)
				} else if param.Value.Schema.Value.Type != "" && param.Value.Schema.Value.Type == "string" {
					if strings.Contains(s.URL.Path, param.Value.Name) {
						s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", "test")
					} else {
						s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+strings.ToLower(param.Value.Name)+"}", "test")
					}
				} else {
					if strings.Contains(s.URL.Path, param.Value.Name) {
						s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", "1")
					} else {
						s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+strings.ToLower(param.Value.Name)+"}", "1")
					}
				}
			} else {
				if strings.Contains(s.URL.Path, param.Value.Name) {
					s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", "test")
				} else {
					s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+strings.ToLower(param.Value.Name)+"}", "test")
				}
			}
		} else if param.Value.In == "query" {
			if param.Value.Schema != nil {
				if param.Value.Schema.Ref != "" {
					s = s.SetParametersFromSchema(param, "query", param.Value.Schema.Ref, nil, 0)
				} else {
					if param.Value.Schema.Value.Type != "" && param.Value.Schema.Value.Type == "string" {
						s.Query.Add(param.Value.Name, "test")
					} else {
						s.Query.Add(param.Value.Name, "1")
					}
				}
			} else {
				s.Query.Add(param.Value.Name, "test")
			}

		} else if param.Value.In == "header" && param.Value.Required && strings.ToLower(param.Value.Name) != "content-type" {
			Headers = append(Headers, fmt.Sprintf("%s: %s", param.Value.Name, "1"))
		} else if param.Value.In == "body" || param.Value.In == "formData" {
			if param.Value.Schema.Ref != "" {
				s = s.SetParametersFromSchema(param, "body", param.Value.Schema.Ref, nil, 0)
			}
			if param.Value.Schema.Value.Type == "string" {
				s.Body[param.Value.Name] = "test"
			} else {
				s.Body[param.Value.Name] = 1
			}
			var data []string
			for k, v := range s.Body {
				data = append(data, fmt.Sprintf("%s=%s", k, v))
			}
			s.BodyData = []byte(strings.Join(data, "&"))
		} else {
			continue
		}
	}

	if op.RequestBody != nil {
		if op.RequestBody.Value != nil {
			if op.RequestBody.Value.Content != nil {
				for i := range op.RequestBody.Value.Content {
					if contentType == "" {
						EnforceSingleContentType(i)
					} else {
						EnforceSingleContentType(contentType)
					}
					if op.RequestBody.Value.Content.Get(i).Schema != nil {
						if op.RequestBody.Value.Content.Get(i).Schema.Value == nil {
							s = s.SetParametersFromSchema(nil, "body", op.RequestBody.Value.Content.Get(i).Schema.Ref, op.RequestBody, 0)
							if strings.Contains(i, "json") {
								s.BodyData, _ = json.Marshal(s.Body)
							} else if strings.Contains(i, "x-www-form-urlencoded") {
								var formData []string
								for j := range s.Body {
									formData = append(formData, fmt.Sprintf("%s=%s", j, fmt.Sprint(s.Body[j])))
								}
								s.BodyData = []byte(strings.Join(formData, "&"))
							} else if strings.Contains(i, "xml") {
								type Element struct {
									XMLName xml.Name
									Content any `xml:",chardata"`
								}

								type Root struct {
									XMLName  xml.Name  `xml:"root"`
									Elements []Element `xml:",any"`
								}

								var elements []Element
								for key, value := range s.Body {
									elements = append(elements, Element{
										XMLName: xml.Name{Local: key},
										Content: value,
									})
								}

								root := Root{
									Elements: elements,
								}

								xmlData, err := xml.Marshal(root)
								if err != nil {
									log.Warn("Error marshalling XML data.")
								}
								s.BodyData = xmlData
							} else {
								log.Warnf("Content type not supported. Test this path manually: %s (Content type: %s)\n", s.URL.Path, i)
							}
						} else {
							var formData []string

							for j := range op.RequestBody.Value.Content.Get(i).Schema.Value.Properties {
								if op.RequestBody.Value.Content.Get(i).Schema.Value.Properties[j].Ref != "" {
									s = s.SetParametersFromSchema(nil, "body", op.RequestBody.Value.Content.Get(i).Schema.Value.Properties[j].Ref, op.RequestBody, 0)
								} else {
									var valueType string = op.RequestBody.Value.Content.Get(i).Schema.Value.Properties[j].Value.Type
									if op.RequestBody.Value.Content.Get(i).Schema.Value.Properties[j].Value != nil {
										if valueType == "string" {
											s.Body[j] = "test"
										} else if valueType == "boolean" {
											s.Body[j] = false
										} else if valueType == "integer" || valueType == "number" {
											s.Body[j] = 1
										} else {
											s.Body[j] = "unknown_type_populate_manually"
										}
										if i == "application/x-www-form-urlencoded" {
											formData = append(formData, fmt.Sprintf("%s=%s", j, fmt.Sprint(s.Body[j])))
										}
									}

									if i == "application/x-www-form-urlencoded" {
										s.BodyData = []byte(strings.Join(formData, "&"))
									} else if strings.Contains(i, "json") || i == "*/*" {
										s.BodyData, _ = json.Marshal(s.Body)
									} else if strings.Contains(i, "xml") {
										//
										type Element struct {
											XMLName xml.Name
											Content any `xml:",chardata"`
										}

										type Root struct {
											XMLName  xml.Name  `xml:"root"`
											Elements []Element `xml:",any"`
										}

										var elements []Element
										for key, value := range s.Body {
											elements = append(elements, Element{
												XMLName: xml.Name{Local: key},
												Content: value,
											})
										}

										root := Root{
											Elements: elements,
										}

										xmlData, err := xml.Marshal(root)
										if err != nil {
											log.Warn("Error marshalling XML data.")
										}
										s.BodyData = xmlData
									} else {
										s.Body["test"] = "test"
										s.BodyData = []byte("test=test")
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if s.ApiInQuery && s.ApiKey != "" {
		s.Query.Add(s.ApiKeyName, s.ApiKey)
	}
	return s
}

func (s SwaggerRequest) GetBasePath() string {
	if basePath == "" {
		if s.Def.Servers != nil {
			if strings.Contains(s.Def.Servers[0].URL, ":") {
				var schemeIndex int
				if strings.Contains(s.Def.Servers[0].URL, "://") {
					schemeIndex = strings.Index(s.Def.Servers[0].URL, "://") + 3
				} else {
					schemeIndex = 0
				}
				s.URL.Host = s.Def.Servers[0].URL[schemeIndex:]
			}

			if s.Def.Servers[0].URL == "/" {
				basePath = "/"
			} else if strings.Contains(s.Def.Servers[0].URL, "http") && !strings.Contains(s.Def.Servers[0].URL, s.URL.Host) { // Check to see if the server object being used for the base path contains a different host than the target
				basePath = s.Def.Servers[0].URL
				basePath = strings.ReplaceAll(basePath, "http://", "")
				basePath = strings.ReplaceAll(basePath, "https://", "")
				indexSubdomain := strings.Index(basePath, "/")
				basePath = basePath[indexSubdomain:]
				if !strings.HasSuffix(basePath, "/") {
					basePath = basePath + "/"
				}
			} else {
				basePath = s.Def.Servers[0].URL
				if strings.Contains(basePath, s.URL.Host) || strings.Contains(basePath, "http") {
					basePath = strings.ReplaceAll(basePath, s.URL.Host, "")
					basePath = strings.ReplaceAll(basePath, "http://", "")
					basePath = strings.ReplaceAll(basePath, "https://", "")
				}
			}

		}
		bp, _ := s.Def.Servers.BasePath()
		if bp != "" {
			basePath = bp
		}
	}
	basePath = strings.TrimSuffix(basePath, "/")
	return basePath
}

func PrintSpecInfo(i openapi3.Info) {
	if strings.ToLower(outputFormat) == "console" {
		if i.Title != "" {
			fmt.Println("Title:", i.Title)
		}

		if i.Description != "" {
			fmt.Printf("Description: %s\n\n", i.Description)
		}

		if i.Title == "" && i.Description == "" {
			log.Warnf("Detected possible error in parsing the definition file. Title and description values are empty.\n\n")
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

func UnmarshalSpec(bodyBytes []byte) (newDoc *openapi3.T) {
	var doc openapi2.T
	var doc3 openapi3.T

	format = strings.ToLower(format)
	if format == "js" || strings.HasSuffix(swaggerURL, ".js") {
		bodyBytes = ExtractSpecFromJS(bodyBytes)
	} else if format == "yaml" || format == "yml" || strings.HasSuffix(swaggerURL, ".yaml") || strings.HasSuffix(swaggerURL, ".yml") {
		_ = yaml.Unmarshal(bodyBytes, &doc)
		_ = yaml.Unmarshal(bodyBytes, &doc3)
	}

	_ = json.Unmarshal(bodyBytes, &doc)
	_ = json.Unmarshal(bodyBytes, &doc3)

	if strings.HasPrefix(doc3.OpenAPI, "3") {
		newDoc := &doc3
		return newDoc
	} else if strings.HasPrefix(doc.Swagger, "2") {
		newDoc, err := openapi2conv.ToV3(&doc)
		if err != nil {
			fmt.Printf("Error converting v2 document to v3: %s\n", err)
		}
		return newDoc
	} else if os.Args[1] == "brute" {
		var noDoc openapi3.T
		return &noDoc
	} else {
		log.Fatal("Error parsing definition file.")
		return nil
	}
}

func ExtractSpecFromJS(bodyBytes []byte) []byte {
	var openApiIndex int
	var specClose int
	var bodyString, spec string

	bodyString = string(bodyBytes)
	spec = strings.ReplaceAll(bodyString, "\n", "")
	spec = strings.ReplaceAll(spec, "\t", "")
	spec = strings.ReplaceAll(spec, " ", "")

	if strings.Contains(strings.ReplaceAll(bodyString, " ", ""), `"swagger":"2.0"`) {
		openApiIndex = strings.Index(spec, `"swagger":`) - 1
		specClose = strings.LastIndex(spec, "]}") + 2

		var doc2 openapi2.T
		bodyBytes = []byte(spec[openApiIndex:specClose])
		_ = json.Unmarshal(bodyBytes, &doc2)
		if !strings.Contains(doc2.Swagger, "2") {
			specClose = strings.LastIndex(spec, "}") + 1
			bodyBytes = []byte(spec[openApiIndex:specClose])
			_ = json.Unmarshal(bodyBytes, &doc2)
			if !strings.Contains(doc2.Swagger, "2") {
				log.Error("Error parsing JavaScript file for spec. Try saving the object as a JSON file and reference it locally.")
			}
		}
	} else if strings.Contains(strings.ReplaceAll(bodyString, " ", ""), `"openapi":"3`) {
		openApiIndex = strings.Index(spec, `"openapi":`) - 1

		specClose = strings.LastIndex(spec, "]}") + 2

		var doc3 openapi3.T
		bodyBytes = []byte(spec[openApiIndex:specClose])
		_ = json.Unmarshal(bodyBytes, &doc3)
		if !strings.Contains(doc3.OpenAPI, "3") {
			specClose = strings.LastIndex(spec, "}") + 1
			bodyBytes = []byte(spec[openApiIndex:specClose])
			_ = json.Unmarshal(bodyBytes, &doc3)
			if !strings.Contains(doc3.OpenAPI, "3") {
				log.Error("Error parsing JavaScript file for spec. Try saving the object as a JSON file and reference it locally.")
			}
		}
	} else {
		log.Error("Error parsing JavaScript file for spec. Try saving the object as a JSON file and reference it locally.")
	}

	return bodyBytes
}

func EnforceSingleContentType(newContentType string) {
	newContentType = strings.TrimSpace(newContentType)

	// Remove old 'Content-Type' header
	slices.DeleteFunc(Headers, func(h string) bool {
		return strings.HasPrefix(strings.ToLower(h), "content-type:")
	})

	Headers = append(Headers, "Content-Type: "+newContentType)

	// Remove empty elements to avoid repetitions of "-H ''"
	Headers = slices.DeleteFunc(Headers, func(h string) bool {
		return strings.TrimSpace(h) == ""
	})
}
