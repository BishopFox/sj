package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestCheckSecuritySchemesJSONDiagnostics(t *testing.T) {
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}

	previousStdout, previousStderr := os.Stdout, os.Stderr
	previousFormat, previousQuiet := outputFormat, quiet
	defer func() {
		os.Stdout, os.Stderr = previousStdout, previousStderr
		outputFormat, quiet = previousFormat, previousQuiet
		stdout.Close()
		stderr.Close()
	}()
	os.Stdout, os.Stderr = stdout, stderr
	outputFormat, quiet = "json", true

	CheckSecuritySchemes(map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ExampleBearer": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
	})

	stdoutBytes, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(stdoutBytes) != 0 {
		t.Fatalf("authentication diagnostics written to stdout: %q", stdoutBytes)
	}

	stderrBytes, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ExampleBearer", "bearerFormat: JWT"} {
		if !strings.Contains(string(stderrBytes), expected) {
			t.Errorf("stderr does not contain %q: %q", expected, stderrBytes)
		}
	}
}
