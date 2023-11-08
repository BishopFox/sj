package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/mpvl/unique"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var endpointsCmd = &cobra.Command{
	Use:   "endpoints",
	Short: "Prints a list of endpoints from the target.",
	Long: `The endpoints command allows you to pull a list of endpoints out of a Swagger definition file.
This list contains the raw endpoints (parameter values will not be appended or modified).`,
	Run: func(cmd *cobra.Command, args []string) {
		client := CheckAndConfigureProxy()

		var bodyBytes []byte
		var paths []string

		fmt.Printf("\n")
		log.Infof("Gathering endpoints.\n\n")

		if swaggerURL != "" {
			bodyBytes, _, _ := MakeRequest(client, "GET", swaggerURL, timeout, nil)
			paths = GenerateRequests(bodyBytes, client, "endpoints")
		} else {
			specFile, err := os.Open(localFile)
			if err != nil {
				log.Fatal("Error opening file:", err)
			}
			bodyBytes, _ = io.ReadAll(specFile)
			paths = GenerateRequests(bodyBytes, client, "endpoints")
		}
		unique.Sort(unique.StringSlice{&paths})
		for _, v := range paths {
			fmt.Println(v)
		}
	},
}
