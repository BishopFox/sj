package cmd

import (
	"encoding/json"
	"os"
	"testing"
)

// captureJSONOutput runs writeLog's JSON branch and returns the decoded stdout.
func captureJSONOutput(t *testing.T, isQuiet, isVerbose bool) map[string]interface{} {
	t.Helper()

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()

	previousStdout := os.Stdout
	previousFormat, previousQuiet, previousVerbose := outputFormat, quiet, verbose
	previousTitle, previousDescription := specTitle, specDescription
	previousResults, previousVerboseResults := jsonResultArray, jsonVerboseResultArray
	defer func() {
		os.Stdout = previousStdout
		outputFormat, quiet, verbose = previousFormat, previousQuiet, previousVerbose
		specTitle, specDescription = previousTitle, previousDescription
		jsonResultArray, jsonVerboseResultArray = previousResults, previousVerboseResults
	}()

	os.Stdout = stdout
	outputFormat, quiet, verbose = "json", isQuiet, isVerbose
	specTitle, specDescription = "Example API", "A long spec description."
	jsonResultArray = []Result{{Method: "GET", Status: 200, Target: "/pet/1"}}
	jsonVerboseResultArray = []VerboseResult{{Method: "GET", Status: 200, Target: "/pet/1"}}

	writeLog(8899, "", "", "", "", "", false)

	data, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, data)
	}
	return decoded
}

func TestQuietOmitsSpecMetadataJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verbose bool
	}{
		{"concise", false},
		{"verbose", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded := captureJSONOutput(t, true, tc.verbose)

			if _, ok := decoded["apiTitle"]; ok {
				t.Error("apiTitle should be omitted when the quiet flag is set")
			}
			if _, ok := decoded["description"]; ok {
				t.Error("description should be omitted when the quiet flag is set")
			}

			results, ok := decoded["results"].([]interface{})
			if !ok {
				t.Fatalf("results missing or wrong type: %#v", decoded["results"])
			}
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
		})
	}
}

func TestSpecMetadataPresentWithoutQuiet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verbose bool
	}{
		{"concise", false},
		{"verbose", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded := captureJSONOutput(t, false, tc.verbose)

			if decoded["apiTitle"] != "Example API" {
				t.Errorf("expected apiTitle to be present, got %#v", decoded["apiTitle"])
			}
			if decoded["description"] != "A long spec description." {
				t.Errorf("expected description to be present, got %#v", decoded["description"])
			}
		})
	}
}

func TestPrintSpecInfoQuietSuppressesConsoleOutput(t *testing.T) {
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()

	previousStderr := os.Stderr
	previousFormat, previousQuiet := outputFormat, quiet
	defer func() {
		os.Stderr = previousStderr
		outputFormat, quiet = previousFormat, previousQuiet
	}()

	os.Stderr = stderr
	outputFormat, quiet = "console", true

	PrintSpecInfo(map[string]interface{}{
		"info": map[string]interface{}{
			"title":       "Example API",
			"description": "A long spec description.",
		},
	})

	data, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected no console output when quiet is set, got %q", data)
	}
}
