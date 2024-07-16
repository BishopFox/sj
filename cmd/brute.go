package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
)

var (
	prefixDirs = []string{
		"/docs",
		"",
		"/swagger",
		"/swagger/docs",
		"/swagger/latest",
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
		"/redoc",
	}
	jsonEndpoints = []string{
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
		"/apispec",
		"/apispec_1",
		"/api-merged",
	}
	javascriptEndpoints = []string{
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
)

func makeURLs(target string, endpoints []string, fileExtension string) []string {
	urls := []string{}
	for _, dir := range prefixDirs {
		for _, endpoint := range endpoints {
			if dir == "" && endpoint == "" {
				continue
			}
			targetURL := target + dir + endpoint + fileExtension
			urls = append(urls, targetURL)

		}
	}
	return urls
}

func findDefinitionFile(urls []string, client http.Client) (bool, *openapi3.T) {
	for i, url := range urls {
		ct := CheckContentType(client, url)
		if strings.Contains(ct, "application/json") {
			bodyBytes, _, _ := MakeRequest(client, "GET", url, timeout, nil)
			if bodyBytes != nil {
				checkSpec := UnmarshalSpec(bodyBytes)
				if (strings.HasPrefix(checkSpec.OpenAPI, "2") || strings.HasPrefix(checkSpec.OpenAPI, "3")) && checkSpec.Paths != nil {
					fmt.Println("")
					log.Infof("Definition file found: %s\n", url)
					return true, checkSpec
				}
			}
		} else if strings.Contains(ct, "application/javascript") {
			bodyBytes, bodyString, _ := MakeRequest(client, "GET", url, timeout, nil)
			if bodyBytes != nil {
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

var bruteCmd = &cobra.Command{
	Use:   "brute",
	Short: "Sends a series of automated requests to discover hidden API operation definitions.",
	Long:  `The brute command sends requests to the target to find operation definitions based on commonly used file locations.`,
	Run: func(cmd *cobra.Command, args []string) {

		client := CheckAndConfigureProxy()

		var allURLs []string
		u, err := url.Parse(swaggerURL)
		if err != nil {
			log.Warnf("Error parsing URL:%s\n", err)
		}
		target := u.Scheme + "://" + u.Host
		if endpointWordlist == "" {
			allURLs = append(allURLs, makeURLs(target, jsonEndpoints, "")...)
			allURLs = append(allURLs, makeURLs(target, javascriptEndpoints, ".js")...)
			allURLs = append(allURLs, makeURLs(target, jsonEndpoints, ".json")...)
			allURLs = append(allURLs, makeURLs(target, jsonEndpoints, "/")...)
		} else {
			endpointWordlistFile, err := os.Open(endpointWordlist)
			if err != nil {
				log.Fatalf("failed to open file: %s", err)
			}
			defer endpointWordlistFile.Close()
			// Create a scanner to read the file
			scanner := bufio.NewScanner(endpointWordlistFile)
			// Read the file line by line
			for scanner.Scan() {
				endpoint := scanner.Text()
				fullURL := target + endpoint
				allURLs = append(allURLs, fullURL)
			}

			// Check for errors during scanning
			if err := scanner.Err(); err != nil {
				log.Fatalf("failed to scan file: %s", err)
			}
		}
		log.Infof("Sending %d requests. This could take a while...\n", len(allURLs))

		specFound, definitionFile := findDefinitionFile(allURLs, client)
		if specFound {
			definedOperations, err := json.Marshal(definitionFile)
			if err != nil {
				log.Errorf("Error parsing definition file:%s\n", err)
			}

			if outfile != "" {

				file, err := os.OpenFile(outfile, os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					log.Errorf("Error opening file: %s\n", err)
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
				fmt.Println(string(definedOperations))
			}
			// TODO: Check if (future implementation) automate flag is true and if so than call the 'sj automate' command with the discovered definition file.
		} else {
			log.Errorf("No definition file found for:\t%s\n", swaggerURL)
		}
	},
}

var endpointWordlist string

func init() {
	// TODO: Add a flag here (boolean) that defaults to false that will cause the program to execute 'sj automate' on the discovered definition file automatically.
	bruteCmd.PersistentFlags().StringVarP(&endpointWordlist, "wordlist", "w", "", "The wordlist containing the paths to brute force.")

}
