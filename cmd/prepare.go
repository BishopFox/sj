package cmd

import (
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Prepares a set of commands for manual testing of each endpoint.",
	Long: `The prepare command prepares a set of curl commands for manual testing of each endpoint.
This enables you to test specific API functions for common vulnerabilities or misconfigurations.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := CheckAndConfigureProxy()

		fmt.Printf("\n")
		log.Infof("Gathering API details.\n\n")
		if swaggerURL != "" {
			bodyBytes, _, _ := MakeRequest(client, "GET", swaggerURL, timeout, nil)
			GenerateRequests(bodyBytes, client, "prepare")
		} else {
			specFile, err := os.Open(localFile)
			if err != nil {
				log.Fatal("Error opening file:", err)
			}

			specBytes, _ := io.ReadAll(specFile)
			GenerateRequests(specBytes, client, "prepare")
		}
	},
}
