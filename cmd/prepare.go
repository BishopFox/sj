package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Prepares a set of commands for manual testing of each endpoint.",
	Long: `The prepare command prepares a set of commands for manual testing of each endpoint.
This enables you to test specific API functions for common vulnerabilities or misconfigurations.`,
	Run: func(cmd *cobra.Command, args []string) {

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				printWarn("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		_, err := time.Parse("2006-01-02", customDate)
		if err != nil {
			fmt.Println("An invalid date was supplied. Please supply a date in '2006-01-02' format.")
			os.Exit(1)
		}

		client := CheckAndConfigureProxy()

		fmt.Printf("\n")
		printInfo("Gathering API details.\n\n")
		if swaggerURL != "" {
			bodyBytes, _, _ := MakeRequest(client, "GET", swaggerURL, timeout, nil)
			GenerateRequests(bodyBytes, client)
		} else {
			specFile, err := os.Open(localFile)
			if err != nil {
				die("Error opening file: %v", err)
			}
			// Set the base directory for resolving external refs
			specBaseDir = filepath.Dir(localFile)
			if specBaseDir == "." {
				if absPath, err := filepath.Abs(localFile); err == nil {
					specBaseDir = filepath.Dir(absPath)
				}
			}

			specBytes, _ := io.ReadAll(specFile)
			GenerateRequests(specBytes, client)
		}
	},
}
var prepareFor string

func init() {
	prepareCmd.PersistentFlags().StringVarP(&prepareFor, "external-tool", "e", "curl", "The external tool to prepare commands for. Generates syntax for 'curl' by default.")
}
