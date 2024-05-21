package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
)

var (
	swaggerPrefixDirs = []string{
		"/docs",
		"",
		"/swagger",
		"/swagger/docs",
		"/swagger/v1",
		"/swagger/v2",
		"/swagger/v3",
		"/swagger/static",
		"/swagger/ui",
		"/swagger-ui",
		"/api-docs",
		"/api-docs/v1",
		"/api-docs/v2",
		"/apidocs",
		"/api",
		"/api/v1",
		"/api/v2",
		"/api/v3",
		"/v1",
		"/v2",
		"/v3",
		"/doc",
		"/docs/swagger",
		"/docs/swagger/v1",
		"/docs/swagger/v2",
		"/docs/swagger-ui",
		"/docs/swagger-ui/v1",
		"/docs/swagger-ui/v2",
		"/docs/v1",
		"/docs/v2",
		"/docs/v3",
		"/public",
		"/graphql",
	}
	swaggerJsonEndpoints = []string{
		"",
		"/index",
		"/swagger",
		"/swagger-ui",
		"/swagger-resources",
		"/swagger-config",
		"/openapi",
		"/api",
		"/api-docs",
		"/apidocs",
		"/v1",
		"/v2",
		"/v3",
		"/doc",
		"/docs",
		"/graphql",
		"/apispec",
		"/apispec_1",
		"/api-merged",
	}
	swaggerJavascriptEndpoints = []string{
		"/swagger-ui-init",
		"/swagger-ui-bundle",
		"/swagger-ui-standalone-preset",
		"/swagger-ui",
		"/swagger-ui.min",
		"/swagger-ui-es-bundle-core",
		"/swagger-ui-es-bundle",
		"/swagger-ui-standalone-preset",
		"/swagger-ui-layout",
		"/swagger-ui-plugins",
	}
	userAgents = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.150 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.2 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:84.0) Gecko/20100101 Firefox/84.0",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; WOW64; Trident/7.0; rv:11.0) like Gecko",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:73.0) Gecko/20100101 Firefox/73.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_3) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.122 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:74.0) Gecko/20100101 Firefox/74.0",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:75.0) Gecko/20100101 Firefox/75.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.4 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36 Edge/16.16299",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:76.0) Gecko/20100101 Firefox/76.0",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.92 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/54.0.2840.99 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.113 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.3; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:77.0) Gecko/20100101 Firefox/77.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/73.0.3683.103 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.1; WOW64; rv:54.0) Gecko/20100101 Firefox/54.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.140 Safari/537.36 Edge/17.17134",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:78.0) Gecko/20100101 Firefox/78.0",
	}
)

func getRandomUserAgent() string {
	rand.Seed(time.Now().UnixNano())
	return userAgents[rand.Intn(len(userAgents))]
}

func makeUrls(target string, prefixDirs []string, endpoints []string, fileExtension string) []string {
	urls := []string{}
	for _, i1 := range prefixDirs {
		for _, i2 := range endpoints {
			if i1 == "" && i2 == "" {
				continue
			}
			targetURL := target + i1 + i2 + fileExtension
			urls = append(urls, targetURL)

		}
	}
	return urls
}

func makeRequests(urls []string, client *http.Client, contentTypeToFind string) <-chan *http.Response {
	responses := make(chan *http.Response)
	go func() {
		defer close(responses)
		requestCount := 1
		fmt.Printf("[*] SEARCHING FOR Content-Type == %s\n", contentTypeToFind)
		for _, url := range urls {
			//fmt.Printf("[*] Request #%d: \t%s\n", requestCount, url)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Printf("[!] Error creating request for %s: %v", url, err)
				continue
			}
			req.Header.Set("User-Agent", getRandomUserAgent())
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[!] Error fetching %s: %v", url, err)
				continue
			}
			//defer resp.Body.Close() // Ensure the body is closed after reading
			if resp.StatusCode == 200 {
				contentType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
				if contentType == contentTypeToFind {
					fmt.Printf("[*] FOUND %s:\t%s\n", contentTypeToFind, url)
					//responses <- resp

					var responseSize int
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						fmt.Println("Error reading body:", err)
						continue
					}
					responseSize = len(body)
					fmt.Printf("[*] Response length: %d bytes\n", responseSize)

					if responseSize >= 8 {
						// Create a new ReadCloser with the original body content
						resp.Body = io.NopCloser(strings.NewReader(string(body)))
						responses <- resp
					} else {
						resp.Body.Close() // Close the body if the response size is too small
					}
				} else {
					resp.Body.Close() // Close the body if content type does not match
				}
			} else {
				resp.Body.Close() // Close the body if status code is not 200
			}
			requestCount++
		}
	}()
	return responses
}

func lookForSpecIndicator(response *http.Response) bool {
	var jsonResponse map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&jsonResponse); err != nil {
		return false
	}
	if _, ok := jsonResponse["openapi"]; ok {
		return true
	}
	if _, ok := jsonResponse["swagger"]; ok {
		return true
	}
	return false
}

func lookForSpecIndicatorJavaScript(response *http.Response) (bool, map[string]interface{}) {
	defer response.Body.Close() // Ensure the body is closed after reading
	regexPattern := regexp.MustCompile(`(?s)let\s+(\w+)\s*=\s*({.*?});`)

	jsContent, err := io.ReadAll(response.Body)

	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return false, nil
	}

	matches := regexPattern.FindAllStringSubmatch(string(jsContent), -1)
	for _, match := range matches {
		jsonContent := match[2]
		var rawData map[string]interface{}
		if err := json.Unmarshal([]byte(jsonContent), &rawData); err != nil {
			continue
		}
		rawDataKeys := make([]string, 0, len(rawData))
		for key := range rawData {
			rawDataKeys = append(rawDataKeys, key)
		}

		if len(rawDataKeys) != 0 {
			data := rawData[rawDataKeys[0]].(map[string]interface{})
			if _, ok := data["openapi"]; ok {
				return true, data
			}
			if _, ok := data["swagger"]; ok {
				return true, data
			}
		}
	}
	return false, nil
}

func doJsonRequestsLoop(urls []string, client *http.Client) bool {
	for response := range makeRequests(urls, client, "application/json") {
		if response != nil && lookForSpecIndicator(response) {
			fmt.Printf("[*] SPEC FILE FOUND:\t%s\n", response.Request.URL)
			return true
		}
	}
	return false
}

func doJavaScriptRequestsLoop(urls []string, client *http.Client) (bool, map[string]interface{}) {
	for response := range makeRequests(urls, client, "application/javascript") {
		if response != nil {
			isSpec, apiDoc := lookForSpecIndicatorJavaScript(response)
			if isSpec {
				fmt.Printf("[*] FOUND SPEC embedded in JavaScript file:\t%s\n", response.Request.URL)
				return true, apiDoc
			}
		}
	}
	return false, nil
}

var outputSpecFile string
var target string = swaggerURL
var bruteCmd = &cobra.Command{
	Use:   "brute",
	Short: "Sends a series of automated requests to discover the spec file.",
	Long: `The brute command sends requests to the target to find the spec file based on historic file locations.
This will first check for specfiles embedded within javascript and then continue on to look for json specfiles.`,
	Run: func(cmd *cobra.Command, args []string) {

		client := &http.Client{}
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		javascriptUrls := makeUrls(target, swaggerPrefixDirs, swaggerJavascriptEndpoints, ".js")
		specFound, apiDoc := doJavaScriptRequestsLoop(javascriptUrls, client)
		if specFound {
			file, err := json.MarshalIndent(apiDoc, "", "  ")
			if err != nil {
				log.Fatalf("Error marshalling API doc: %v", err)
			}
			if err := os.WriteFile(outputSpecFile, file, 0644); err != nil {
				log.Fatalf("Error writing to file: %v", err)
			}
			return
		}

		jsonUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, ".json")
		if doJsonRequestsLoop(jsonUrls, client) {
			return
		}

		blankUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, "")
		if doJsonRequestsLoop(blankUrls, client) {
			return
		}

		htmlUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, ".html")
		if doJsonRequestsLoop(htmlUrls, client) {
			return
		}

		// Not sure if YAML and YML files will need to be processed differently... could exclude these.
		//yamlUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, ".yaml")
		//if doJsonRequestsLoop(yamlUrls, client) {
		//	return
		//}

		//ymlUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, ".yml")
		//if doJsonRequestsLoop(ymlUrls, client) {
		//	return
		//}

		slashUrls := makeUrls(target, swaggerPrefixDirs, swaggerJsonEndpoints, "/")
		if doJsonRequestsLoop(slashUrls, client) {
			return
		}

		// Should maybe add code to just copy the specfile locally even if it is found to just be a json file because it will make automating easier.

		// Should add a flag here to check if automate is true and if so than call the 'sj automate' function with the specfile that is copied locally.
		fmt.Printf("[!] NO SPECFILE FOUND for:\t%s\n", target)
	},
}

func init() {
	bruteCmd.PersistentFlags().StringVarP(&outputSpecFile, "outfile", "o", "spec_file.json", "Output the results to a file. This defaults to a JSON file unless an output format (-F) is specified.")

	//Should add a flag here called something like 'automate' which is a boolean that defaults to false. But when present it is set to true and then it will cause the program to do 'sj automate' on the found spec file automatically.
}
