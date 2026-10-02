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

// TestCheckSecuritySchemesSwaggerV2 verifies that Swagger 2.0 schemes defined
// under the top-level securityDefinitions object are read, not only the
// OpenAPI 3.x components.securitySchemes location.
func TestCheckSecuritySchemesSwaggerV2(t *testing.T) {
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
		"swagger": "2.0",
		"securityDefinitions": map[string]interface{}{
			"apiKeyHeader": map[string]interface{}{
				"type": "apiKey",
				"name": "X-API-Key",
				"in":   "header",
			},
			"basicAuth": map[string]interface{}{
				"type": "basic",
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
	stderrStr := string(stderrBytes)
	if strings.Contains(stderrStr, "No security schemes defined") {
		t.Fatalf("Swagger 2.0 securityDefinitions were not read: %q", stderrStr)
	}
	for _, expected := range []string{"Found security schemes", "apiKeyHeader", "basicAuth"} {
		if !strings.Contains(stderrStr, expected) {
			t.Errorf("stderr does not contain %q: %q", expected, stderrBytes)
		}
	}
}

// runCheckSecuritySchemes runs CheckSecuritySchemes in quiet/json mode with
// stdout and stderr captured, and saves/restores every global the function
// mutates so tests cannot pollute one another. It returns captured stdout and
// stderr.
func runCheckSecuritySchemes(t *testing.T, spec map[string]interface{}) (string, string) {
	t.Helper()

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
	previousHeaders := Headers
	previousQueryParams := authQueryParams
	previousCookieParams := authCookieParams
	defer func() {
		os.Stdout, os.Stderr = previousStdout, previousStderr
		outputFormat, quiet = previousFormat, previousQuiet
		Headers = previousHeaders
		authQueryParams = previousQueryParams
		authCookieParams = previousCookieParams
		stdout.Close()
		stderr.Close()
	}()
	os.Stdout, os.Stderr = stdout, stderr
	outputFormat, quiet = "json", true
	// Start from a clean credential state so length assertions are meaningful.
	Headers = nil
	authQueryParams = nil
	authCookieParams = nil

	CheckSecuritySchemes(spec)

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
	return string(stdoutBytes), string(stderrBytes)
}

// TestCheckSecuritySchemesBearerQuiet verifies that a spec-compliant OpenAPI 3.x
// bearer scheme (type: http, scheme: bearer) is warn-only in quiet mode and does
// not attach an Authorization header without interactive input.
func TestCheckSecuritySchemesBearerQuiet(t *testing.T) {
	_, stderr := runCheckSecuritySchemes(t, map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ExampleBearer": map[string]interface{}{
					"type":   "http",
					"scheme": "bearer",
				},
			},
		},
	})

	if !strings.Contains(stderr, "A bearer token is accepted") {
		t.Errorf("expected bearer warning, got: %q", stderr)
	}
	if len(Headers) != 0 {
		t.Errorf("quiet mode must not attach credentials, got Headers=%v", Headers)
	}
}

// TestCheckSecuritySchemesBearerCasing verifies the HTTP scheme match is
// case-insensitive: a capitalized "Bearer" is still recognized and warned about.
// Under the previous exact-match switch no branch ran and nothing was emitted.
func TestCheckSecuritySchemesBearerCasing(t *testing.T) {
	_, stderr := runCheckSecuritySchemes(t, map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ExampleBearer": map[string]interface{}{
					"type":   "http",
					"scheme": "Bearer",
				},
			},
		},
	})

	if !strings.Contains(stderr, "A bearer token is accepted") {
		t.Errorf("capitalized scheme should be recognized as bearer, got: %q", stderr)
	}
}

// TestCheckSecuritySchemesApiKeyNamedBearer verifies that an apiKey-in-header
// scheme whose map key happens to be "bearer" is treated as an API key, not as
// bearer auth. This guards against regression of the removed name-based match.
func TestCheckSecuritySchemesApiKeyNamedBearer(t *testing.T) {
	_, stderr := runCheckSecuritySchemes(t, map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearer": map[string]interface{}{
					"type": "apiKey",
					"name": "X-API-Key",
					"in":   "header",
				},
			},
		},
	})

	if !strings.Contains(stderr, "An API key can be provided via the header X-API-Key") {
		t.Errorf("apiKey scheme named \"bearer\" should use the apiKey-header path, got: %q", stderr)
	}
	if strings.Contains(stderr, "A bearer token is accepted") {
		t.Errorf("apiKey scheme named \"bearer\" must not trigger the bearer path, got: %q", stderr)
	}
	if len(Headers) != 0 {
		t.Errorf("quiet mode must not attach credentials, got Headers=%v", Headers)
	}
}

// TestCheckSecuritySchemesApiKeyCookie verifies that an apiKey-in-cookie scheme
// is recognized and prompted for, and that quiet mode records no cookie param.
func TestCheckSecuritySchemesApiKeyCookie(t *testing.T) {
	_, stderr := runCheckSecuritySchemes(t, map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"cookieKey": map[string]interface{}{
					"type": "apiKey",
					"name": "session",
					"in":   "cookie",
				},
			},
		},
	})

	if !strings.Contains(stderr, "An API key can be provided via the cookie session") {
		t.Errorf("apiKey in: cookie should be recognized, got: %q", stderr)
	}
	if len(authCookieParams) != 0 {
		t.Errorf("quiet mode must not record cookie params, got %v", authCookieParams)
	}
}
