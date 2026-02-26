package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	log "github.com/sirupsen/logrus"
)

var endpointOnly bool
var endpointWordlist string
var bruteContinue bool
var bruteMaxFound int
var bruteRunAutomate bool

var prefixDirs []string = []string{"", "/swagger", "/swagger/docs", "/swagger/latest", "/swagger/v1", "/swagger/v2", "/swagger/v3", "/swagger/static", "/swagger/ui", "/swagger-ui", "/api-docs", "/api-docs/v1", "/api-docs/v2", "/apidocs", "/api", "/api/v1", "/api/v2", "/api/v3", "/v1", "/v2", "/v3", "/doc", "/docs", "/docs/swagger", "/docs/swagger/v1", "/docs/swagger/v2", "/docs/swagger-ui", "/docs/swagger-ui/v1", "/docs/swagger-ui/v2", "/docs/v1", "/docs/v2", "/docs/v3", "/public", "/redoc"}
var jsonEndpoints []string = []string{"", "/index", "/swagger", "/swagger-ui", "/swagger-resources", "/swagger-config", "/openapi", "/api", "/api-docs", "/apidocs", "/v1", "/v2", "/v3", "/doc", "/docs", "/apispec", "/apispec_1", "/api-merged"}
var javascriptEndpoints []string = []string{"/swagger-ui-init", "/swagger-ui-bundle", "/swagger-ui-standalone-preset", "/swagger-ui", "/swagger-ui.min", "/swagger-ui-es-bundle-core", "/swagger-ui-es-bundle", "/swagger-ui-standalone-preset", "/swagger-ui-layout", "/swagger-ui-plugins"}
var priorityURLs []string = []string{"/swagger.json", "/openapi.json", "/api-docs", "/swagger", "/docs", "/api/swagger.json", "/api/openapi.json", "/api-docs/swagger.json", "/api/schema/", "/webjars/swagger-ui/index.html", "/API/swagger/ui/index", "/swagger/ui/index", "/v2/swagger.json", "/v2/openapi.json", "/v2/api-docs", "/v3/api-docs", "/v3/openapi.json", "/public/api-merged.json", "/analytics/v1/swagger", "/api.json", "/api/4.0/swagger.json", "/api/api-doc/openapi.json", "/api/api-doc/openapi.yaml", "/api/doc.json", "/api/docs.json", "/api/swagger", "/api/swagger/ui/index", "/api/v1/swagger", "/api/v2/api-docs", "/api/v2/openapi.json", "/api/v2/swagger.json", "/api/v3/api-docs", "/api/v3/apispec", "/api/workorder/openapi.json", "/apidocs", "/audiences/v1/swagger", "/audittrail/v1/swagger", "/certification/v1/swagger", "/citrixapi/store/swagger.json", "/conferencetool/v1/swagger", "/course/v1/swagger", "/dcl_swagger.yaml", "/doc/doc.json", "/doc/swagger.json", "/docs/swagger.json", "/docs/v1/swagger.json", "/ecommerce/v1/swagger", "/enrollment/v1/swagger", "/externalids/v1/swagger", "/impact/v1/swagger", "/learn/v1/swagger", "/learningplan/v1/swagger", "/manage/v1/swagger", "/management/info", "/marketplace/v1/swagger", "/messenger/v1/swagger", "/notifications/v1/swagger", "/openapi", "/openapi/spec.json", "/openapi2.json", "/openapi2.yaml", "/otj/v1/swagger", "/pages/v1/swagger", "/poweruser/v1/swagger", "/proctoring/v1/swagger", "/report/v1/swagger", "/rest/swagger.json", "/rest/swagger.yaml", "/rest-api/swagger.json", "/spec/swagger.json", "/spec/swagger.yaml", "/spec/openapi.json", "/spec/openapi.yaml", "/swagger-ui/index.html", "/swagger-ui/openapi.json", "/swagger.yaml", "/swagger/0.1.0/swagger.json", "/swagger/doc.json", "/swagger/latest/swagger.json", "/swagger/swagger.json", "/swagger/test/swagger.json", "/swagger/ui/index.html", "/swagger/v1/openapiv2.json", "/swagger/v1/swagger.json", "/swagger/v2/swagger.json", "/swagger/v4/swagger.json", "/v1/openapi.json", "/v1/swagger", "/v1/swagger.json", "/swagger/docs/v1", "/swagger/docs/v1.json", "/Api/swagger/docs/v1", "/swagger/v1/swagger.json", "/api/api-docs/swagger.json", "/api/docs/", "/api/docs", "/swagger-ui"}

var bruteCmd = &cobra.Command{
	Use:   "brute",
	Short: "Sends a series of automated requests to discover hidden API operation definitions.",
	Long:  `The brute command sends requests to the target to find operation definitions based on commonly used file locations.`,
	Run: func(cmd *cobra.Command, args []string) {
		currentCommand = "brute"
		if err := validateBruteFlags(); err != nil {
			log.Fatal(err)
		}

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				log.Warnf("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		client := CheckAndConfigureProxy()

		options := DiscoveryOptions{
			Continue:     bruteContinue,
			MaxFound:     bruteMaxFound,
			ShowProgress: true,
			DedupeMode:   DedupeURLAndHash,
		}
		if bruteRunAutomate {
			options.OnDiscovered = func(discovered DiscoveredSpec) {
				log.Infof("Discovered spec [%s]: %s", discovered.Phase, discovered.URL)
				report := ExecuteAutomateSpecBytes(client, discovered.SpecBytes, discovered.URL, true)
				if report.Error != "" {
					log.Errorf("Automate run failed for %s: %s", discovered.URL, report.Error)
				}
			}
		}

		log.Info("Starting multi-phase discovery...")
		specs, err := DiscoverSpecs(swaggerURL, client, endpointWordlist, options)
		if err != nil {
			log.Errorf("\nNo definition file found for:\t%s\n", swaggerURL)
			return
		}

		if bruteRunAutomate {
			return
		}

		emitBruteDiscoveryResults(specs)
	},
}

func makeURLs(target string, endpoints []string, fileExtension string, skipPrefix bool) []string {
	urls := []string{}
	if !skipPrefix {
		for _, dir := range prefixDirs {
			for _, endpoint := range endpoints {
				if dir == "" && endpoint == "" {
					continue
				}
				targetURL := target + dir + endpoint + fileExtension
				urls = append(urls, targetURL)

			}
		}
	} else {
		for _, endpoint := range endpoints {
			if endpoint == "" {
				continue
			}
			targetURL := target + endpoint + fileExtension
			urls = append(urls, targetURL)
		}
	}
	return urls
}

func validateBruteFlags() error {
	if strings.TrimSpace(swaggerURL) == "" {
		return fmt.Errorf("the --url flag is required")
	}
	if bruteRunAutomate && endpointOnly {
		return fmt.Errorf("cannot use --run-automate with --endpoint-only")
	}
	if bruteMaxFound > 0 && !bruteContinue {
		return fmt.Errorf("--max-found requires --continue")
	}
	return nil
}

func emitBruteDiscoveryResults(specs []DiscoveredSpec) {
	if len(specs) == 0 {
		return
	}

	for _, discovered := range specs {
		log.Infof("Definition file found [%s]: %s", discovered.Phase, discovered.URL)
	}

	if endpointOnly {
		var urls []string
		for _, discovered := range specs {
			urls = append(urls, discovered.URL)
			fmt.Println(discovered.URL)
		}
		if outfile != "" {
			contents := []byte(strings.Join(urls, "\n") + "\n")
			if err := os.WriteFile(outfile, contents, 0644); err != nil {
				log.Errorf("Error writing file: %s\n", err)
			}
		}
		return
	}

	if len(specs) == 1 {
		handleSpecFound(specs[0].Spec)
		return
	}

	rawSpecs := make([]json.RawMessage, 0, len(specs))
	for _, discovered := range specs {
		rawSpecs = append(rawSpecs, json.RawMessage(discovered.SpecBytes))
	}
	combined, err := json.Marshal(rawSpecs)
	if err != nil {
		log.Errorf("Error marshalling discovered specs: %v", err)
		return
	}

	if outfile != "" {
		if err := os.WriteFile(outfile, combined, 0644); err != nil {
			log.Errorf("Error writing file: %s\n", err)
			return
		}
		if f, err := filepath.Abs(outfile); err == nil {
			log.Infof("Wrote file to %s\n", f)
		}
		return
	}

	fmt.Println(string(combined))
}

func findDefinitionFile(urls []string, client http.Client) (bool, *openapi3.T) {

	for i, url := range urls {
		ct := CheckContentType(client, url)
		// Check for JSON content type
		if strings.Contains(ct, "application/json") {
			bodyBytes, _, _ := MakeRequest(client, "GET", url, timeout, nil)
			if bodyBytes != nil {
				// Use UnmarshalSpecBytes for content-type-based parsing
				checkSpec := UnmarshalSpecBytes(bodyBytes)
				if (strings.HasPrefix(checkSpec.OpenAPI, "2") || strings.HasPrefix(checkSpec.OpenAPI, "3")) && checkSpec.Paths != nil {
					fmt.Println("")
					log.Infof("Definition file found: %s\n", url)
					return true, checkSpec
				}
			}
		} else if strings.Contains(ct, "application/yaml") || strings.Contains(ct, "application/x-yaml") || strings.Contains(ct, "text/yaml") || strings.Contains(ct, "text/yml") {
			// Check for YAML content type
			bodyBytes, _, _ := MakeRequest(client, "GET", url, timeout, nil)
			if bodyBytes != nil {
				// Use UnmarshalSpecBytes which handles YAML without relying on global URL
				checkSpec := UnmarshalSpecBytes(bodyBytes)
				if (strings.HasPrefix(checkSpec.OpenAPI, "2") || strings.HasPrefix(checkSpec.OpenAPI, "3")) && checkSpec.Paths != nil {
					fmt.Println("")
					log.Infof("Definition file found: %s\n", url)
					return true, checkSpec
				}
			}
		} else if strings.Contains(ct, "application/javascript") {
			bodyBytes, bodyString, _ := MakeRequest(client, "GET", url, timeout, nil)
			if bodyBytes != nil {
				// First attempt: Check for Swashbuckle configuration
				swashPath := ExtractSwashbuckleConfig(bodyString)
				if swashPath != "" {
					fullURL, err := ResolveRelativeURL(url, swashPath)
					if err == nil {
						swashSpec, err := FetchAndValidateSpec(fullURL, client)
						if err == nil && swashSpec != nil {
							log.Infof("\nFound spec via Swashbuckle config at: %s\n", fullURL)
							return true, swashSpec
						}
					}
				}

				// Second attempt: Extract spec URL from JavaScript
				specURL := ExtractSpecURLFromJS(bodyString)
				if specURL != "" {
					fullURL, err := ResolveRelativeURL(url, specURL)
					if err == nil {
						// Fetch the referenced spec
						refSpec, err := FetchAndValidateSpec(fullURL, client)
						if err == nil && refSpec != nil {
							log.Infof("\nFound spec URL referenced in JavaScript: %s\n", fullURL)
							return true, refSpec
						}
					}
				}

				// Third attempt: Extract embedded spec from JavaScript (original logic + enhanced)
				embeddedSpec, err := ExtractEmbeddedSpecFromJS(bodyString)
				if err == nil && embeddedSpec != nil && embeddedSpec.Paths != nil {
					log.Infof("\nFound embedded spec in JavaScript file at %s\n", url)
					return true, embeddedSpec
				}

				// Fallback: Try original simple regex pattern for backward compatibility
				regexPattern := regexp.MustCompile(`(?s)let\s+(\w+)\s*=\s*({.*?});`)
				matches := regexPattern.FindAllStringSubmatch(bodyString, -1)
				for _, match := range matches {
					jsonContent := match[2]
					checkSpec := UnmarshalSpec([]byte(jsonContent))
					if strings.HasPrefix(checkSpec.OpenAPI, "2") || strings.HasPrefix(checkSpec.OpenAPI, "3") {
						log.Infof("\nFound operation definitions embedded in JavaScript file at %s\n", url)
						return true, checkSpec
					}
				}
			}
		}
		if i == len(urls) {
			fmt.Printf("\033[2K\r%s%d\n", "Request: ", i+1)
		} else {
			fmt.Printf("\033[2K\r%s%d", "Request: ", i+1)
		}
	}
	return false, nil
}

func init() {
	bruteCmd.PersistentFlags().StringVarP(&endpointWordlist, "wordlist", "w", "", "The file containing a list of paths to brute force for discovery.")
	bruteCmd.Flags().BoolVarP(&endpointOnly, "endpoint-only", "e", false, "Only return the identified endpoint")
	bruteCmd.Flags().BoolVar(&bruteContinue, "continue", false, "Continue brute-force scanning after first discovered spec.")
	bruteCmd.Flags().IntVar(&bruteMaxFound, "max-found", 0, "Maximum number of unique specs to process in --continue mode (0 = unlimited).")
	bruteCmd.Flags().BoolVar(&bruteRunAutomate, "run-automate", false, "Run automate checks immediately for each discovered spec.")
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

// handleSpecFound processes a discovered spec by marshaling and outputting it
func handleSpecFound(spec *openapi3.T) {
	definedOperations, err := json.Marshal(spec)
	if err != nil {
		log.Errorf("Error parsing definition file: %s\n", err)
		return
	}

	if outfile != "" {
		file, err := os.OpenFile(outfile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			log.Errorf("Error opening file: %s\n", err)
			return
		}
		defer file.Close()

		_, err = file.Write(definedOperations)
		if err != nil {
			log.Errorf("Error writing file: %s\n", err)
		} else {
			f, _ := filepath.Abs(outfile)
			log.Infof("Wrote file to %s\n", f)
		}
	} else {
		if endpointOnly {
			return
		}
		fmt.Println(string(definedOperations))
	}
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
	} else if currentCommand == "brute" {
		var noDoc openapi3.T
		return &noDoc
	} else {
		log.Fatal("Error parsing definition file.")
		return nil
	}
}
