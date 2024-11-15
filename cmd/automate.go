package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
)

var getAccessibleEndpoints bool
var outputFormat string
var responsePreviewLength int
var verbose bool

var automateCmd = &cobra.Command{
	Use:   "automate",
	Short: "Sends a series of automated requests to the discovered endpoints.",
	Long: `The automate command sends a request to each discovered endpoint and returns the status code of the result.
This enables the user to get a quick look at which endpoints require authentication and which ones do not. If a request
responds in an abnormal way, manual testing should be conducted (prepare manual tests using the "prepare" command).`,
	Run: func(cmd *cobra.Command, args []string) {
		if outfile != "" && strings.ToLower(outputFormat) != "" {
			if !strings.HasSuffix(strings.ToLower(outfile), "json") && strings.ToLower(outputFormat) != "json" {
				log.Fatal("Only the JSON output format is supported at the moment.")
			} else if strings.HasSuffix(strings.ToLower(outfile), "json") && strings.ToLower(outputFormat) == "console" {
				outputFormat = "json"
			}
		}

		if rateLimit <= 0 {
			log.Fatal("Invalid rate supplied. Must be a positive number")
		}

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				log.Warnf("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		var bodyBytes []byte

		client := CheckAndConfigureProxy()

		if strings.ToLower(outputFormat) != "json" {
			fmt.Printf("\n")
			log.Infof("Gathering API details.\n")
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
		if rateLimit > 0 {
			log.Info("Sending requests at a rate of ", rateLimit, " requests per second.")
		}
		GenerateRequests(bodyBytes, client)
	},
}

func init() {
	automateCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "F", "console", "The output format. Only 'console' (default) and 'json' are supported at the moment.")
	automateCmd.PersistentFlags().BoolVar(&getAccessibleEndpoints, "get-accessible-endpoints", false, "Only output the accessible endpoints (those that return a 200 status code).")
	automateCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose mode, which shows a preview of each response.")
	automateCmd.PersistentFlags().IntVar(&responsePreviewLength, "response-preview-length", 50, "sets the response preview length when using verbose output.")

}
