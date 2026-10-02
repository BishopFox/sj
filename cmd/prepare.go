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
	RunE: func(cmd *cobra.Command, args []string) error {

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				printWarn("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		_, err := time.Parse("2006-01-02", customDate)
		if err != nil {
			return fmt.Errorf("an invalid date was supplied. Please supply a date in '2006-01-02' format")
		}

		client, _, err := CheckAndConfigureProxy()
		if err != nil {
			return err
		}

		printInfo("\n")
		printInfo("Gathering API details.\n\n")
		if swaggerURL != "" {
			bodyBytes, _, _, err := MakeRequest(client, "GET", swaggerURL, timeout, nil)
			if err != nil {
				return err
			}
			return GenerateRequests(bodyBytes, client, nil)
		}

		specFile, err := os.Open(localFile)
		if err != nil {
			return fmt.Errorf("error opening file: %v", err)
		}
		// Set the base directory for resolving external refs
		specBaseDir = filepath.Dir(localFile)
		if specBaseDir == "." {
			if absPath, err := filepath.Abs(localFile); err == nil {
				specBaseDir = filepath.Dir(absPath)
			}
		}

		specBytes, _ := io.ReadAll(specFile)
		return GenerateRequests(specBytes, client, nil)
	},
}
var prepareFor string

func init() {
	prepareCmd.PersistentFlags().StringVarP(&prepareFor, "external-tool", "e", "curl", "The external tool to prepare commands for. Generates syntax for 'curl' by default.")
}
