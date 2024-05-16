package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
)

var outputFile string
var outputFormat string
var automateCmd = &cobra.Command{
	Use:   "automate",
	Short: "Sends a series of automated requests to the discovered endpoints.",
	Long: `The automate command sends a request to each discovered endpoint and returns the status code of the result.
This enables the user to get a quick look at which endpoints require authentication and which ones do not. If a request
responds in an abnormal way, manual testing should be conducted (prepare manual tests using the "prepare" command).`,
	Run: func(cmd *cobra.Command, args []string) {

		if outputFile != "" && strings.ToLower(outputFormat) != "json" && strings.ToLower(outputFormat) != "" {
			log.Fatal("Only the JSON output format is supported at the moment.")
		}

		var bodyBytes []byte

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
				log.Fatal("Error opening file:", err)
			}

			bodyBytes, _ = io.ReadAll(specFile)
		}
		GenerateRequests(bodyBytes, client, "automate")
	},
}

func init() {
	automateCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "F", "json", "The output format. Only 'console' (default) and 'json' are supported at the moment.")
	automateCmd.PersistentFlags().StringVarP(&outputFile, "outfile", "o", "", "Output the results to a file. This defaults to a JSON file unless an output format (-F) is specified.")
}
