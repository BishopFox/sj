package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Prepares a set of commands for manual testing of each endpoint.",
	Long: `The prepare command prepares a set of commands for manual testing of each endpoint.
This enables you to test specific API functions for common vulnerabilities or misconfigurations.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := CheckAndConfigureProxy()

		fmt.Printf("\n")
		log.Infof("Gathering API details.\n\n")
		if swaggerURL != "" {
			bodyBytes, _, _ := MakeRequest(client, "GET", swaggerURL, timeout, nil)
			GenerateRequests(bodyBytes, client)
		} else {
			specFile, err := os.Open(localFile)
			if err != nil {
				log.Fatal("Error opening file:", err)
			}

			specBytes, _ := io.ReadAll(specFile)
			GenerateRequests(specBytes, client)
		}
	},
}
var prepareFor string

func (s SwaggerRequest) PrintPreparedCommands(method string) {
	var preparedCommand string
	if strings.ToLower(prepareFor) == "curl" {
		preparedCommand = fmt.Sprintf("curl -sk -X %s '%s'", method, s.URL.String())
	} else if strings.ToLower(prepareFor) == "sqlmap" {
		preparedCommand = fmt.Sprintf("sqlmap --method=%s -u %s", method, s.URL.String())
	} else if strings.ToLower(prepareFor) == "ffuf" {
		log.Fatal("ffuf is not supported yet :(")
		// TODO
	} else {
		log.Fatal("External tool not supported. Only 'curl' and 'sqlmap' are supported options for the '-e' flag at this time.")
	}

	if s.BodyData != nil {
		if strings.ToLower(prepareFor) == "sqlmap" {
			preparedCommand = preparedCommand + fmt.Sprintf(" --data='%s'", s.BodyData)
		} else {
			preparedCommand = preparedCommand + fmt.Sprintf(" -d '%s'", s.BodyData)
		}
	}
	if len(Headers) != 0 {
		preparedCommand = preparedCommand + fmt.Sprintf(" -H '%s'", strings.Join(Headers, "' -H '"))
	}

	fmt.Println(preparedCommand)
}

func init() {
	prepareCmd.PersistentFlags().StringVarP(&prepareFor, "external-tool", "e", "curl", "The external tool to prepare commands for. Generates syntax for 'curl' by default.")
}
