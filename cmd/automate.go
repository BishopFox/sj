package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
)

var getAccessibleEndpoints bool
var outputFormat string
var responsePreviewLength int
var testString string
var verbose bool
var enhanced bool
var maxRetries int
var urlFile string
var fallbackBrute bool

var automateCmd = &cobra.Command{
	Use:   "automate",
	Short: "Sends a series of automated requests to the discovered endpoints.",
	Long: `The automate command sends a request to each discovered endpoint and returns the status code of the result.
This enables the user to get a quick look at which endpoints require authentication and which ones do not. If a request
responds in an abnormal way, manual testing should be conducted (prepare manual tests using the "prepare" command).`,
	Run: func(cmd *cobra.Command, args []string) {
		currentCommand = "automate"

		// Check for incompatible flags
		if enhanced && quiet {
			log.Fatal("Cannot use --enhanced with --quiet flag. Enhanced mode requires interactive input.")
		}
		if err := validateAutomateFlags(); err != nil {
			log.Fatal(err)
		}

		if outfile != "" && strings.ToLower(outputFormat) != "" {
			if !strings.HasSuffix(strings.ToLower(outfile), "json") && strings.ToLower(outputFormat) != "json" {
				log.Fatal("Only the JSON output format is supported at the moment.")
			} else if strings.HasSuffix(strings.ToLower(outfile), "json") && strings.ToLower(outputFormat) == "console" {
				outputFormat = "json"
			}
		}

		if randomUserAgent {
			if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
				log.Warnf("A supplied User Agent was detected (%s) while supplying the 'random-user-agent' flag.", UserAgent)
			}
		}

		_, err := time.Parse("2006-01-02", customDate)
		if err != nil {
			fmt.Println("An invalid date was supplied. Please supply a date in '2006-01-02' format.")
			os.Exit(1)
		}

		client := CheckAndConfigureProxy()

		if strings.ToLower(outputFormat) != "json" {
			fmt.Printf("\n")
			log.Infof("Gathering API details.\n")
		}

		if rateLimit > 0 && strings.ToLower(outputFormat) != "json" {
			log.Info("Sending requests at a rate of ", rateLimit, " requests per second.")
		} else if rateLimit == 0 && strings.ToLower(outputFormat) != "json" {
			log.Info("Sending requests with no rate limit (unlimited).")
		}

		// Validate max-retries in enhanced mode
		if enhanced && maxRetries < 1 {
			log.Fatal("Error: --max-retries must be at least 1 in enhanced mode (got ", maxRetries, ").")
		}

		if localFile != "" {
			specFile, err := os.Open(localFile)
			if err != nil {
				log.Fatal("Error opening file:", err)
			}
			defer specFile.Close()
			// Set the base directory for resolving external refs
			specBaseDir = filepath.Dir(localFile)
			if specBaseDir == "." {
				if absPath, err := filepath.Abs(localFile); err == nil {
					specBaseDir = filepath.Dir(absPath)
				}
			}

			bodyBytes, err := io.ReadAll(specFile)
			if err != nil {
				log.Fatal("Error reading file:", err)
			}

			report := ExecuteAutomateSpecBytes(client, bodyBytes, localFile, false)
			if report.Error != "" {
				log.Fatal(report.Error)
			}
			return
		}

		var targets []string
		if urlFile != "" {
			parsedTargets, invalidLines, err := loadURLFileEntries(urlFile)
			if err != nil {
				log.Fatal("Error loading URL file:", err)
			}
			for _, invalid := range invalidLines {
				log.Warnf("Skipping invalid URL file entry: %s", invalid)
			}
			if len(parsedTargets) == 0 {
				log.Fatal("No valid entries found in --url-file")
			}
			targets = parsedTargets
		} else {
			normalizedTarget, err := NormalizeTargetInput(swaggerURL)
			if err != nil {
				log.Fatal(err)
			}
			targets = []string{normalizedTarget}
		}

		aggregateJSON := strings.ToLower(outputFormat) == "json" && len(targets) > 1
		runReports := []AutomateRunReport{}

		for _, target := range targets {
			directSpec, directErr := tryDirectDiscoveredSpec(target, client)
			if directErr == nil {
				suppressFinalize := strings.ToLower(outputFormat) == "json" && aggregateJSON
				report := ExecuteAutomateSpecBytes(client, directSpec.SpecBytes, directSpec.URL, suppressFinalize)
				report.Input = target
				report.SpecURL = directSpec.URL
				report.DiscoveryUsed = false
				report.DiscoveryPhase = directSpec.Phase
				runReports = append(runReports, report)

				// Preserve legacy JSON output behavior for single direct runs.
				if strings.ToLower(outputFormat) == "json" && !aggregateJSON && len(targets) == 1 {
					return
				}
				continue
			}

			if !fallbackBrute {
				errMsg := fmt.Sprintf("failed to load API specification from URL '%s': %v", target, directErr)
				if len(targets) == 1 {
					log.Fatal(errMsg)
				}
				runReports = append(runReports, AutomateRunReport{
					Input:          target,
					SpecURL:        target,
					DiscoveryUsed:  false,
					DiscoveryPhase: "direct",
					Error:          errMsg,
				})
				if strings.ToLower(outputFormat) == "json" {
					aggregateJSON = true
				}
				continue
			}

			discovered, discoverErr := DiscoverSpecs(target, client, "", DiscoveryOptions{
				Continue:     true,
				MaxFound:     0,
				ShowProgress: strings.ToLower(outputFormat) != "json",
				DedupeMode:   DedupeURLAndHash,
			})
			if discoverErr != nil || len(discovered) == 0 {
				errMsg := fmt.Sprintf("fallback discovery failed for '%s': %v", target, discoverErr)
				runReports = append(runReports, AutomateRunReport{
					Input:          target,
					SpecURL:        target,
					DiscoveryUsed:  true,
					DiscoveryPhase: "brute",
					Error:          errMsg,
				})
				if strings.ToLower(outputFormat) == "json" {
					aggregateJSON = true
				} else if len(targets) == 1 {
					log.Error(errMsg)
				}
				continue
			}

			if strings.ToLower(outputFormat) == "json" {
				aggregateJSON = true
			}

			for _, discoveredSpec := range discovered {
				report := ExecuteAutomateSpecBytes(client, discoveredSpec.SpecBytes, discoveredSpec.URL, strings.ToLower(outputFormat) == "json")
				report.Input = target
				report.SpecURL = discoveredSpec.URL
				report.DiscoveryUsed = true
				report.DiscoveryPhase = discoveredSpec.Phase
				runReports = append(runReports, report)
			}
		}

		if strings.ToLower(outputFormat) == "json" && aggregateJSON {
			if err := emitBulkAutomateJSONReport(runReports); err != nil {
				log.Fatal("Error marshalling aggregate automate output:", err)
			}
		}
	},
}

func init() {
	automateCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "F", "console", "The output format. Only 'console' (default) and 'json' are supported at the moment.")
	automateCmd.PersistentFlags().BoolVar(&getAccessibleEndpoints, "get-accessible-endpoints", false, "Only output the accessible endpoints (those that return a 200 status code).")
	automateCmd.PersistentFlags().StringVar(&testString, "test-string", "bishopfox", "The string to use when testing endpoints with string values.")
	automateCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose mode, which shows a preview of each response.")
	automateCmd.PersistentFlags().IntVar(&responsePreviewLength, "response-preview-length", 50, "sets the response preview length when using verbose output.")
	automateCmd.PersistentFlags().BoolVar(&enhanced, "enhanced", false, "Enable interactive mode for ambiguous responses (designed for LLM/MCP integration).")
	automateCmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 5, "Maximum number of retry attempts per endpoint in enhanced mode.")
	automateCmd.PersistentFlags().StringVar(&urlFile, "url-file", "", "Path to a newline-delimited list of target URLs/hosts for bulk automate runs.")
	automateCmd.PersistentFlags().BoolVar(&fallbackBrute, "fallback-brute", false, "If direct spec loading fails, run brute discovery and automate discovered specs.")

}
