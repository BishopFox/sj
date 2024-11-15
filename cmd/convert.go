package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Converts a Swagger definition file to an OpenAPI v3 definition file.",
	Long:  `The convert command converts a provided definition file from the Swagger specification (v2) to the OpenAPI specification (v3) and stores it into an output file.`,
	Run: func(cmd *cobra.Command, args []string) {

		var bodyBytes []byte

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				log.Warnf("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		client := CheckAndConfigureProxy()

		if strings.ToLower(outputFormat) != "json" {
			fmt.Printf("\n")
			log.Infof("Gathering API details.\n\n")
		}

		if swaggerURL != "" {
			bodyBytes, _, _ = MakeRequest(client, "GET", swaggerURL, timeout, nil)
		} else {
			specFile, err := os.Open(localFile)
			if err != nil {
				log.Fatal("Error opening definition file:", err)
			}

			bodyBytes, _ = io.ReadAll(specFile)
		}

		var doc openapi2.T
		var doc3 openapi3.T

		format = strings.ToLower(format)
		if format == "yaml" || format == "yml" || strings.HasSuffix(swaggerURL, ".yaml") || strings.HasSuffix(swaggerURL, ".yml") {
			_ = yaml.Unmarshal(bodyBytes, &doc)
			_ = yaml.Unmarshal(bodyBytes, &doc3)
		} else {
			_ = json.Unmarshal(bodyBytes, &doc)
			_ = json.Unmarshal(bodyBytes, &doc3)
		}

		if strings.HasPrefix(doc3.OpenAPI, "3") {
			log.Warnln("Definition file is already version 3.")
			WriteConvertedDefinitionFile(bodyBytes)
		} else if strings.HasPrefix(doc.Swagger, "2") {
			newDoc, err := openapi2conv.ToV3(&doc)
			if err != nil {
				fmt.Printf("Error converting v2 document to v3: %s\n", err)
			}

			if format == "json" {
				if strings.HasSuffix(outfile, "yaml") || strings.HasSuffix(outfile, "yml") {
					log.Warn("It looks like you're trying to save the file in YAML format. Supply the '-f yaml' option to do so (default: json).")
				}
				converted, err := json.Marshal(newDoc)
				if err != nil {
					log.Fatal("Error converting definition file to v3:", err)
				}
				if !strings.HasSuffix(string(converted), "}") {
					if outfile == "" {
						fmt.Println(string(converted))
					} else {
						WriteConvertedDefinitionFile(converted)
					}
				} else {
					endOfJSON := strings.LastIndex(string(converted), "}") + 1
					if outfile == "" {
						fmt.Println(string(converted)[:endOfJSON])
					} else {
						WriteConvertedDefinitionFile(converted)
					}
				}
			} else if format == "yaml" || format == "yml" {
				converted, err := yaml.Marshal(newDoc)
				if err != nil {
					log.Fatal("Error converting definition file to v3:", err)
				}
				if outfile == "" {
					fmt.Println(string(converted))
				} else {
					WriteConvertedDefinitionFile(converted)
				}
			}

		} else {
			log.Fatal("Error parsing definition file.")
		}
	},
}

func WriteConvertedDefinitionFile(data []byte) {
	file, err := os.OpenFile(outfile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Errorf("Error opening file: %s\n", err)
	}

	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		log.Errorf("Error writing file: %s\n", err)
	} else {
		f, _ := filepath.Abs(outfile)
		log.Infof("Wrote file to %s\n", f)
	}
}
