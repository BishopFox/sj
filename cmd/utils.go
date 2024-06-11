package cmd

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var accessibleEndpoints []string
var specJSON string

func CheckAndConfigureProxy() (client http.Client) {
	var proxyUrl *url.URL

	transport := &http.Transport{}

	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if proxy != "NOPROXY" {
		proxyUrl, _ = url.Parse(proxy)
		transport.Proxy = http.ProxyURL(proxyUrl)
	}

	client = http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client
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

func GenerateRequests(bodyBytes []byte, client http.Client, command string) []string {
	var apiKey string
	var apiKeyName string
	var apiInQuery bool = false
	var resultsJSON []string
	var pathMap = make(map[string]bool)
	var paths []string

	fullUrl, _ := url.Parse(swaggerURL)
	newDoc := UnmarshalSpec(bodyBytes)
	basePathResult := GetBasePath(newDoc.Servers, TrimHostScheme(apiTarget, fullUrl.Host))

	// BuildObjectsFromSchemaDefinitions(*newDoc) TODO

	if command != "endpoints" {
		// Prints Title/Description values if they exist
		PrintSpecInfo(*newDoc.Info)
		apiInQuery, apiKey, apiKeyName = CheckSecDefs(*newDoc)
	}

	if command == "automate" && outfile != "" {
		WriteJSONFile(`"results":[`, false)
	}

	if newDoc.Paths == nil {
		log.Fatalf("Could not find any defined operations. Review the file manually.")
	}
	for path, pathItem := range newDoc.Paths {
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

		for method, op := range operations {
			// Do all the things here :D
			if op != nil {
				var newPath string
				newPath = path

				u := url.URL{
					Scheme: SetScheme(swaggerURL),
					Host:   TrimHostScheme(apiTarget, fullUrl.Host),
					Path:   basePathResult,
				}
				if u.Path == "/" {
					u.Path = ""
				}
				query := url.Values{}

				var bodyData []byte
				body := make(map[string]any)

				for _, param := range op.Parameters {
					if param.Ref != "" || param.Value == nil {
						continue
					} else if param.Value.In == "path" {
						newPath = strings.ReplaceAll(newPath, "{"+param.Value.Name+"}", "test")
					} else if param.Value.In == "query" {
						if param.Value.Schema.Ref != "" {
							query.Add(param.Value.Name, "test") // TODO: Implement actual definition/schema
						} else {
							if param.Value.Schema.Value.Type == "string" {
								query.Add(param.Value.Name, "test")
							} else {
								query.Add(param.Value.Name, "1")
							}
						}
					} else if param.Value.In == "header" && param.Value.Required {
						Headers = append(Headers, fmt.Sprintf("%s: %s", param.Value.Name, "1"))
					} else if param.Value.In == "body" {
						if param.Value.Schema.Value.Type == "string" {
							body[param.Value.Name] = "test"
						} else {
							body[param.Value.Name] = 1
						}
						bodyData, _ = json.Marshal(body)
					} else {
						continue
					}
				}

				if op.RequestBody != nil {
					body["test"] = "test"
					bodyData, _ = json.Marshal(body)
				}

				if apiInQuery && apiKey != "" {
					query.Add(apiKeyName, apiKey)
				}

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

				u.Path = u.Path + newPath
				u.RawQuery = query.Encode()
				if command == "automate" {
					_, _, sc := MakeRequest(client, method, u.String(), timeout, bytes.NewReader([]byte(bodyData)), command)
					if sc == 200 {
						accessibleEndpointFound = true
						accessibleEndpoints = append(accessibleEndpoints, u.String())
					}
					if outfile != "" {
						if getAccessibleEndpoints {
							if sc == 200 {
								result := fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"},`, sc, u.String(), method, errorDescriptions[fmt.Sprint(sc)])
								WriteJSONFile(result, false)
							}
						} else {
							result := fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"},`, sc, u.String(), method, errorDescriptions[fmt.Sprint(sc)])
							WriteJSONFile(result, false)
						}
						time.Sleep(1 * time.Second)
					} else if outfile == "" {
						if strings.ToLower(outputFormat) != "console" {
							if getAccessibleEndpoints {
								if sc == 200 {
									resultsJSON = append(resultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"}`, sc, u.String(), method, errorDescriptions[fmt.Sprint(sc)]))
								}
							} else {
								if sc == 1 {
									resultsJSON = append(resultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"Skipped due to dangerous keyword in request"}`, sc, u.String(), method))
								} else {
									resultsJSON = append(resultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"}`, sc, u.String(), method, errorDescriptions[fmt.Sprint(sc)]))
								}
							}
						} else if !getAccessibleEndpoints && strings.ToLower(outputFormat) == "console" {
							writeLog(sc, u.String(), method, errorDescriptions[fmt.Sprint(sc)])
						}
					}
				} else if command == "prepare" {
					if bodyData == nil {
						if len(Headers) == 0 {
							fmt.Printf("curl -sk -X %s '%s'\n", method, u.String())
						} else {
							fmt.Printf("curl -sk -X %s '%s' -H '%s'\n", method, u.String(), strings.Join(Headers, "' -H '"))
						}
					} else {
						if len(Headers) == 0 {
							fmt.Printf("curl -sk -X %s '%s' -d '%s'\n", method, u.String(), bodyData)
						} else {
							fmt.Printf("curl -sk -X %s '%s' -d '%s' -H '%s'\n", method, u.String(), bodyData, strings.Join(Headers, "' -H '"))
						}
					}
				} else if command == "endpoints" {

					for k := range newDoc.Paths {
						if !pathMap[k] {
							paths = append(paths, k)
							pathMap[k] = true
						}
					}
				}

			}
		}
	}

	if command == "automate" {
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
		} else if outfile == "" && strings.ToLower(outputFormat) == "json" {
			results := strings.Join(resultsJSON, ",")
			fmt.Printf(`%s"results":[%s]}%s`, specJSON, results, "\n")
		} else if outfile != "" {
			if accessibleEndpoints == nil {
				WriteJSONFile("]}\n", false)
			} else {
				WriteJSONFile(",", true)
			}
			log.Infof("Results written to %s", outfile)
		}
	}

	return paths
}

func GetBasePath(servers openapi3.Servers, host string) (bp string) {
	if basePath == "" {
		if servers != nil {
			s1, _ := url.Parse(servers[0].URL)

			if len(servers) >= 2 {
				s2, _ := url.Parse(servers[1].URL)
				if s1.Host != s2.Host || len(servers) > 2 {
					log.Warn("Multiple servers detected in documentation. You can manually set a server to test with the -T flag.\nThe detected servers are as follows:")
					for i := range servers {
						fmt.Printf("Server %d: %s\n", i+1, servers[i].URL)
					}
				}
			}
			if servers[0].URL == "/" {
				basePath = "/"
			} else {
				basePath = servers[0].URL
				if strings.Contains(basePath, host) || strings.Contains(basePath, "http") {
					basePath = strings.ReplaceAll(basePath, host, "")
					basePath = strings.ReplaceAll(basePath, "http://", "")
					basePath = strings.ReplaceAll(basePath, "https://", "")
				}
			}

		}

	}
	basePath = strings.TrimSuffix(basePath, "/")
	return basePath
}

func PrintSpecInfo(i openapi3.Info) {
	specJSON = fmt.Sprintf(`{"title":"%s","description":"%s",`, i.Title, i.Description)
	if outfile != "" {
		WriteJSONFile(specJSON, false)
	} else if strings.ToLower(outputFormat) == "console" {
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
	} else {
		log.Fatal("Error parsing definition file.\n")
		return nil
	}
}

func WriteJSONFile(result string, end bool) {
	if end {
		file, err := os.OpenFile(outfile, os.O_RDWR, 0644)

		if err != nil {
			fmt.Println("File does not exist or cannot be created")
			os.Exit(1)
		}
		defer file.Close()

		w := bufio.NewWriter(file)

		fileInfo, err := file.Stat()
		if err != nil {
			log.Error("Failed to get file info:", err)
		}
		fileSize := fileInfo.Size()
		if fileSize != 0 {
			lastChar := fileSize - 1

			_, err = file.Seek(lastChar, 0)
			if err != nil {
				log.Error("Error seeking to last character", err)
			}
			fmt.Fprintf(w, "]}\n")

		}
		w.Flush()
	} else {
		file, err := os.OpenFile(outfile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)

		if err != nil {
			fmt.Println("File does not exist or cannot be created")
			os.Exit(1)
		}
		defer file.Close()

		w := bufio.NewWriter(file)

		fmt.Fprintf(w, "%s", result)
		w.Flush()
	}

}
