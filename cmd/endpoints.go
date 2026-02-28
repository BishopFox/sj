package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var endpointsCmd = &cobra.Command{
	Use:   "endpoints",
	Short: "Prints a list of endpoints from the target.",
	Long: `The endpoints command allows you to pull a list of endpoints out of a Swagger definition file.
This list contains the raw endpoints (parameter values will not be appended or modified).`,
	Run: func(cmd *cobra.Command, args []string) {
		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				printWarn("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		client := CheckAndConfigureProxy()

		var bodyBytes []byte

		fmt.Printf("\n")
		printInfo("Gathering endpoints.\n\n")

		if swaggerURL != "" {
			bodyBytes, _, _ = MakeRequest(client, "GET", swaggerURL, timeout, nil)
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
			bodyBytes, _ = io.ReadAll(specFile)
			GenerateRequests(bodyBytes, client)
		}
	},
}
