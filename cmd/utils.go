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

func GenerateRequests(bodyBytes []byte, client http.Client) []string {

	var s SwaggerRequest
	s.Def = UnmarshalSpec(bodyBytes)
	s.ApiInQuery, s.ApiKey, s.ApiKeyName = CheckSecDefs(*s.Def)
	u, _ := url.Parse(swaggerURL)
	s.URL = *u

	// BuildObjectsFromSchemaDefinitions(*newDoc) TODO

	if os.Args[1] != "endpoints" {
		// Prints Title/Description values if they exist
		PrintSpecInfo(*s.Def.Info)
	}

	if os.Args[1] == "automate" && outfile != "" {
		WriteJSONFile(`"results":[`, false)
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
		if len(s.Def.Servers) > 1 && apiTarget == "" {
			for _, server := range s.Def.Servers {
				if outputFormat == "console" && os.Args[1] == "automate" {
					log.Warnf("Results for %s:\n", server.URL)
				}

				if server.URL == "/" {
					s.Path = ""
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
		}
		s.URL = *u
		s = s.IterateOverPaths(client)
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
		} else if outfile == "" && strings.ToLower(outputFormat) == "json" {
			results := strings.Join(s.ResultsJSON, ",")
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

		for method, op := range operations {
			// Do all the things here :D
			if op != nil {
				s.URL.Path = s.Path + path
				s = s.BuildDefinedRequests(client, method, pathItem, op)
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
		_, _, sc := MakeRequest(client, method, s.URL.String(), timeout, bytes.NewReader(s.BodyData))
		if sc == 200 {
			accessibleEndpointFound = true
			accessibleEndpoints = append(accessibleEndpoints, s.URL.String())
		}
		if outfile != "" {
			if getAccessibleEndpoints {
				if sc == 200 {
					result := fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"},`, sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)])
					WriteJSONFile(result, false)
				}
			} else {
				result := fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"},`, sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)])
				WriteJSONFile(result, false)
			}
			time.Sleep(1 * time.Second)
		} else if outfile == "" {
			if strings.ToLower(outputFormat) != "console" {
				if getAccessibleEndpoints {
					if sc == 200 {
						s.ResultsJSON = append(s.ResultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"}`, sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)]))
					}
				} else {
					if sc == 1 {
						s.ResultsJSON = append(s.ResultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"Skipped due to dangerous keyword in request"}`, sc, s.URL.String(), method))
					} else {
						s.ResultsJSON = append(s.ResultsJSON, fmt.Sprintf(`{"status_code":"%d","url":"%s","method":"%s","details":"%s"}`, sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)]))
					}
				}
			} else if !getAccessibleEndpoints && strings.ToLower(outputFormat) == "console" {
				writeLog(sc, s.URL.String(), method, errorDescriptions[fmt.Sprint(sc)])
			}
		}
	} else if os.Args[1] == "prepare" {
		if strings.ToLower(prepareFor) == "curl" {
			if s.BodyData == nil {
				if len(Headers) == 0 {
					fmt.Printf("curl -sk -X %s '%s'\n", method, s.URL.String())
				} else {
					fmt.Printf("curl -sk -X %s '%s' -H '%s'\n", method, s.URL.String(), strings.Join(Headers, "' -H '"))
				}
			} else {
				if len(Headers) == 0 {
					fmt.Printf("curl -sk -X %s '%s' -d '%s'\n", method, s.URL.String(), s.BodyData)
				} else {
					fmt.Printf("curl -sk -X %s '%s' -d '%s' -H '%s'\n", method, s.URL.String(), s.BodyData, strings.Join(Headers, "' -H '"))
				}
			}
		} else if strings.ToLower(prepareFor) == "sqlmap" {
			if s.BodyData == nil {
				if len(Headers) == 0 {
					fmt.Printf("sqlmap -u %s\n", s.URL.String())
				} else {
					fmt.Printf("sqlmap -u %s -H '%s'\n", s.URL.String(), strings.Join(Headers, "' -H '"))
				}
			} else {
				if len(Headers) == 0 {
					fmt.Printf("sqlmap -u %s --data='%s'\n", s.URL.String(), s.BodyData)
				} else {
					fmt.Printf("sqlmap -u %s --data='%s' -H '%s'\n", s.URL.String(), s.BodyData, strings.Join(Headers, "' -H '"))
				}
			}
		} else if strings.ToLower(prepareFor) == "ffuf" {
			// TODO
		} else {
			log.Fatal("External tool not supported. Only 'curl' and 'sqlmap' are supported options for the '-e' flag at this time.")
		}

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

func (s SwaggerRequest) AddParametersToRequest(op *openapi3.Operation) SwaggerRequest {
	for _, param := range op.Parameters {
		if param.Ref != "" || param.Value == nil {
			continue
		} else if param.Value.In == "path" {
			s.Path = strings.ReplaceAll(s.Path, "{"+param.Value.Name+"}", "test")
		} else if param.Value.In == "query" {
			if param.Value.Schema.Ref != "" {
				s.Query.Add(param.Value.Name, "test") // TODO: Implement actual definition/schema
			} else {
				if param.Value.Schema.Value.Type == "string" {
					s.Query.Add(param.Value.Name, "test")
				} else {
					s.Query.Add(param.Value.Name, "1")
				}
			}
		} else if param.Value.In == "header" && param.Value.Required {
			Headers = append(Headers, fmt.Sprintf("%s: %s", param.Value.Name, "1"))
		} else if param.Value.In == "body" {
			if param.Value.Schema.Value.Type == "string" {
				s.Body[param.Value.Name] = "test"
			} else {
				s.Body[param.Value.Name] = 1
			}
			s.BodyData, _ = json.Marshal(s.Body)
		} else {
			continue
		}
	}

	if op.RequestBody != nil {
		s.Body["test"] = "test"
		s.BodyData, _ = json.Marshal(s.Body)
	}

	if s.ApiInQuery && s.ApiKey != "" {
		s.Query.Add(s.ApiKeyName, s.ApiKey)
	}
	return s
}

func (s SwaggerRequest) GetBasePath() string {
	if strings.Contains(s.URL.Host, ":") {
		hostPortStart := strings.Index(s.URL.Host, ":")
		s.URL.Host = s.URL.Host[0:hostPortStart]
	}
	if basePath == "" {
		if s.Def.Servers != nil {
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
	err := json.Unmarshal(bodyBytes, &doc)
	if err != nil {
		err = json.Unmarshal(bodyBytes, &doc3)
		if err != nil {
			log.Fatalf("Error unmarshalling API definitions: %s\n", err)
		}
	}
	err = json.Unmarshal(bodyBytes, &doc3)

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
		log.Fatalf("Error parsing definition file: %s\n", doc3.OpenAPI)
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
