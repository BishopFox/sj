package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// AutomateRunReport captures a single automate execution context and result set.
type AutomateRunReport struct {
	Input          string      `json:"input"`
	SpecURL        string      `json:"spec_url,omitempty"`
	DiscoveryUsed  bool        `json:"discovery_used"`
	DiscoveryPhase string      `json:"discovery_phase,omitempty"`
	APITitle       string      `json:"api_title,omitempty"`
	Description    string      `json:"description,omitempty"`
	Results        interface{} `json:"results,omitempty"`
	Error          string      `json:"error,omitempty"`
}

// BulkAutomateReport captures all runs for bulk/fallback workflows.
type BulkAutomateReport struct {
	Mode     string              `json:"mode"`
	RunCount int                 `json:"run_count"`
	Runs     []AutomateRunReport `json:"runs"`
}

func cloneHeaders(headers []string) []string {
	cloned := make([]string, len(headers))
	copy(cloned, headers)
	return cloned
}

func buildDiscoveredSpec(specURL, phase string, spec *openapi3.T, specBytes []byte) (DiscoveredSpec, error) {
	if spec == nil {
		return DiscoveredSpec{}, fmt.Errorf("nil spec")
	}
	if len(specBytes) == 0 {
		var err error
		specBytes, err = json.Marshal(spec)
		if err != nil {
			return DiscoveredSpec{}, err
		}
	}
	hash := sha256.Sum256(specBytes)
	return DiscoveredSpec{
		URL:         specURL,
		Phase:       phase,
		SpecBytes:   specBytes,
		ContentHash: hex.EncodeToString(hash[:]),
		Spec:        spec,
	}, nil
}

func tryDirectDiscoveredSpec(targetURL string, client http.Client) (DiscoveredSpec, error) {
	bodyBytes, _, statusCode := MakeRequest(client, http.MethodGet, targetURL, timeout, nil)
	spec, err := ValidateSpecResponse(bodyBytes, statusCode)
	if err != nil {
		return DiscoveredSpec{}, err
	}
	return buildDiscoveredSpec(targetURL, "direct", spec, bodyBytes)
}

func loadURLFileEntries(path string) ([]string, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var targets []string
	var invalid []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalized, err := NormalizeTargetInput(line)
		if err != nil {
			invalid = append(invalid, line)
			continue
		}
		targets = append(targets, normalized)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return targets, invalid, nil
}

func validateAutomateFlags() error {
	if strings.TrimSpace(swaggerURL) != "" && strings.TrimSpace(urlFile) != "" {
		return fmt.Errorf("cannot use -u/--url with --url-file")
	}
	if strings.TrimSpace(localFile) != "" && strings.TrimSpace(urlFile) != "" {
		return fmt.Errorf("cannot use --local-file with --url-file")
	}
	if fallbackBrute && strings.TrimSpace(localFile) != "" {
		return fmt.Errorf("cannot use --fallback-brute with --local-file")
	}
	if strings.TrimSpace(swaggerURL) == "" && strings.TrimSpace(localFile) == "" && strings.TrimSpace(urlFile) == "" {
		return fmt.Errorf("a target is required: provide --url, --local-file, or --url-file")
	}
	return nil
}

// ExecuteAutomateSpecBytes executes automate request generation for a single spec payload.
// It protects global mutable state so repeated runs (bulk/discovery) do not leak between runs.
func ExecuteAutomateSpecBytes(client http.Client, specBytes []byte, specURL string, suppressJSONFinalize bool) AutomateRunReport {
	report := AutomateRunReport{SpecURL: specURL}

	savedCurrentCommand := currentCommand
	savedBasePath := basePath
	savedAPITarget := apiTarget
	savedSwaggerURL := swaggerURL
	savedSpecBaseDir := specBaseDir
	savedHeaders := cloneHeaders(Headers)
	savedExternalRefCache := externalRefCache
	savedSpecToFilePath := specToFilePath
	savedAvoidDangerousRequests := avoidDangerousRequests
	savedRiskSurveyed := riskSurveyed
	savedResponseContentType := responseContentType
	savedSuppressFinalize := suppressAutomateJSONFinalize

	defer func() {
		currentCommand = savedCurrentCommand
		basePath = savedBasePath
		apiTarget = savedAPITarget
		swaggerURL = savedSwaggerURL
		specBaseDir = savedSpecBaseDir
		Headers = savedHeaders
		externalRefCache = savedExternalRefCache
		specToFilePath = savedSpecToFilePath
		avoidDangerousRequests = savedAvoidDangerousRequests
		riskSurveyed = savedRiskSurveyed
		responseContentType = savedResponseContentType
		suppressAutomateJSONFinalize = savedSuppressFinalize
	}()

	currentCommand = "automate"
	basePath = savedBasePath
	apiTarget = savedAPITarget
	swaggerURL = specURL
	suppressAutomateJSONFinalize = suppressJSONFinalize
	avoidDangerousRequests = ""
	riskSurveyed = false
	responseContentType = ""

	Headers = cloneHeaders(savedHeaders)
	externalRefCache = make(map[string]map[string]interface{})
	specToFilePath = make(map[*interface{}]string)
	accessibleEndpoints = nil
	jsonResultsStringArray = nil
	jsonResultArray = nil
	jsonVerboseResultArray = nil
	specTitle = ""
	specDescription = ""

	if specURL != "" && !strings.HasPrefix(specURL, "http://") && !strings.HasPrefix(specURL, "https://") {
		specBaseDir = filepath.Dir(specURL)
		if specBaseDir == "." {
			if absPath, err := filepath.Abs(specURL); err == nil {
				specBaseDir = filepath.Dir(absPath)
			}
		}
	}

	GenerateRequests(specBytes, client, enhanced, maxRetries)

	report.APITitle = specTitle
	report.Description = specDescription
	if verbose {
		results := make([]VerboseResult, len(jsonVerboseResultArray))
		copy(results, jsonVerboseResultArray)
		report.Results = results
	} else {
		results := make([]Result, len(jsonResultArray))
		copy(results, jsonResultArray)
		report.Results = results
	}

	return report
}

func emitBulkAutomateJSONReport(reports []AutomateRunReport) error {
	bulk := BulkAutomateReport{
		Mode:     "bulk_automate",
		RunCount: len(reports),
		Runs:     reports,
	}
	encoded, err := json.MarshalIndent(bulk, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
