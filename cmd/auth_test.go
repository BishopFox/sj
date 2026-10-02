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

// bearerSpec returns a spec declaring a global bearerAuth requirement and the
// matching scheme definition, with the supplied paths spliced in.
func bearerSpec(paths map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"openapi":  "3.0.0",
		"security": []interface{}{map[string]interface{}{"bearerAuth": []interface{}{}}},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{"type": "http", "scheme": "bearer"},
				"apiKeyAuth": map[string]interface{}{"type": "apiKey", "name": "X-API-Key", "in": "header"},
			},
		},
		"paths": paths,
	}
}

func TestResolveOperationSecurity(t *testing.T) {
	spec := bearerSpec(nil)

	tests := []struct {
		name      string
		op        map[string]interface{}
		wantState securityState
		wantLabel string
		undefined []string
	}{
		{
			name:      "inherits global requirement",
			op:        map[string]interface{}{},
			wantState: secRequired,
			wantLabel: "bearerAuth",
		},
		{
			name:      "operation-level public override",
			op:        map[string]interface{}{"security": []interface{}{}},
			wantState: secPublic,
			wantLabel: "public",
		},
		{
			name: "operation-level different scheme",
			op: map[string]interface{}{"security": []interface{}{
				map[string]interface{}{"apiKeyAuth": []interface{}{}},
			}},
			wantState: secRequired,
			wantLabel: "apiKeyAuth",
		},
		{
			name: "optional when anonymous entry present",
			op: map[string]interface{}{"security": []interface{}{
				map[string]interface{}{},
				map[string]interface{}{"bearerAuth": []interface{}{}},
			}},
			wantState: secOptional,
			wantLabel: "optional: bearerAuth",
		},
		{
			name: "references undefined scheme",
			op: map[string]interface{}{"security": []interface{}{
				map[string]interface{}{"oldKey": []interface{}{}},
			}},
			wantState: secRequired,
			wantLabel: "oldKey",
			undefined: []string{"oldKey"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveOperationSecurity(spec, tc.op)
			if got.state != tc.wantState {
				t.Errorf("state = %d, want %d", got.state, tc.wantState)
			}
			if got.label() != tc.wantLabel {
				t.Errorf("label = %q, want %q", got.label(), tc.wantLabel)
			}
			if strings.Join(got.undefined, ",") != strings.Join(tc.undefined, ",") {
				t.Errorf("undefined = %v, want %v", got.undefined, tc.undefined)
			}
		})
	}
}

// TestResolveOperationSecurityUndeclared verifies that with no spec-level and no
// operation-level security, the operation is undeclared and yields no label.
func TestResolveOperationSecurityUndeclared(t *testing.T) {
	spec := map[string]interface{}{"openapi": "3.0.0", "paths": map[string]interface{}{}}
	got := resolveOperationSecurity(spec, map[string]interface{}{})
	if got.state != secUndeclared {
		t.Errorf("state = %d, want secUndeclared", got.state)
	}
	if got.label() != "" {
		t.Errorf("label = %q, want empty", got.label())
	}
}

func TestRequirementSatisfiedByCredentials(t *testing.T) {
	spec := bearerSpec(nil)
	bearerOp := resolveOperationSecurity(spec, map[string]interface{}{})

	if requirementSatisfiedByCredentials(bearerOp, spec, nil) {
		t.Error("bearer requirement must not be satisfied with no headers")
	}
	if !requirementSatisfiedByCredentials(bearerOp, spec, []string{"Authorization: Bearer abc"}) {
		t.Error("bearer requirement should be satisfied by an Authorization: Bearer header")
	}
	// A basic header does not satisfy a bearer requirement.
	if requirementSatisfiedByCredentials(bearerOp, spec, []string{"Authorization: Basic abc"}) {
		t.Error("basic header must not satisfy a bearer requirement")
	}

	// apiKey-in-query requirement satisfied via authQueryParams.
	querySpec := map[string]interface{}{
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"qk": map[string]interface{}{"type": "apiKey", "name": "api_key", "in": "query"},
			},
		},
	}
	queryOp := resolveOperationSecurity(querySpec, map[string]interface{}{
		"security": []interface{}{map[string]interface{}{"qk": []interface{}{}}},
	})

	previousQueryParams := authQueryParams
	defer func() { authQueryParams = previousQueryParams }()

	authQueryParams = nil
	if requirementSatisfiedByCredentials(queryOp, querySpec, nil) {
		t.Error("query apiKey requirement must not be satisfied with no params")
	}
	authQueryParams = []queryAuthParam{{name: "api_key", value: "x"}}
	if !requirementSatisfiedByCredentials(queryOp, querySpec, nil) {
		t.Error("query apiKey requirement should be satisfied by a recorded query param")
	}
}

// runSummarizeOperationSecurity captures stdout and stderr around a call to
// SummarizeOperationSecurity, restoring the streams afterward.
func runSummarizeOperationSecurity(t *testing.T, spec map[string]interface{}) (stdout, stderr string) {
	t.Helper()

	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}

	previousStdout, previousStderr := os.Stdout, os.Stderr
	defer func() {
		os.Stdout, os.Stderr = previousStdout, previousStderr
		outFile.Close()
		errFile.Close()
	}()
	os.Stdout, os.Stderr = outFile, errFile

	SummarizeOperationSecurity(spec)

	outBytes, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	errBytes, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(outBytes), string(errBytes)
}

func TestSummarizeOperationSecurity(t *testing.T) {
	spec := bearerSpec(map[string]interface{}{
		"/users": map[string]interface{}{
			"get": map[string]interface{}{},
		},
		"/health": map[string]interface{}{
			"get": map[string]interface{}{"security": []interface{}{}},
		},
		"/legacy": map[string]interface{}{
			"post": map[string]interface{}{"security": []interface{}{
				map[string]interface{}{"oldKey": []interface{}{}},
			}},
		},
	})

	stdout, stderr := runSummarizeOperationSecurity(t, spec)

	if stdout != "" {
		t.Fatalf("summary written to stdout: %q", stdout)
	}
	for _, expected := range []string{
		"Operation security requirements",
		"requires: bearerAuth",
		"PUBLIC",
		"references undefined scheme",
		"oldKey",
	} {
		if !strings.Contains(stderr, expected) {
			t.Errorf("stderr does not contain %q: %q", expected, stderr)
		}
	}
}
