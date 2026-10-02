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
	RunE: func(cmd *cobra.Command, args []string) error {
		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				printWarn("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		client, _, err := CheckAndConfigureProxy()
		if err != nil {
			return err
		}

		var bodyBytes []byte

		printInfo("\n")
		printInfo("Gathering endpoints.\n\n")

		if swaggerURL != "" {
			bodyBytes, _, _, err = MakeRequest(client, "GET", swaggerURL, timeout, nil)
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
		bodyBytes, _ = io.ReadAll(specFile)
		return GenerateRequests(bodyBytes, client, nil)
	},
}
