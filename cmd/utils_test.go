package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSwaggerV2SchemeHandling(t *testing.T) {
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldSpecBaseDir := specBaseDir
	defer func() {
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		specBaseDir = oldSpecBaseDir
	}()

	specPath := filepath.Join("..", "tests", "test_spec_v2.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	swaggerURL = ""
	apiTarget = ""

	if schemes, ok := spec["schemes"].([]interface{}); ok && len(schemes) > 0 {
		if scheme, ok := schemes[0].(string); ok {
			if scheme != "https" && scheme != "http" {
				t.Errorf("Expected http or https scheme, got: %s", scheme)
			}
		}
	}

	if host, ok := spec["host"].(string); !ok || host == "" {
		t.Error("Expected host field in Swagger v2 spec")
	}
}

func TestExternalReferenceResolution(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldCache := externalRefCache
	defer func() {
		specBaseDir = oldSpecBaseDir
		externalRefCache = oldCache
	}()

	externalRefCache = make(map[string]map[string]interface{})

	specPath := filepath.Join("..", "tests", "test_spec_external.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	ref := "external_schemas.yaml#/components/schemas/Customer"
	resolved := ResolveExternalRef(ref, specBaseDir)

	if resolved == nil {
		t.Fatal("Failed to resolve external reference")
	}

	if resolvedType, ok := resolved["type"].(string); !ok || resolvedType != "object" {
		t.Error("Expected Customer schema to be an object")
	}

	if props, ok := resolved["properties"].(map[string]interface{}); ok {
		if _, hasCustomerId := props["customerId"]; !hasCustomerId {
			t.Error("Expected Customer schema to have 'customerId' property")
		}
		if _, hasContact := props["contactInfo"]; !hasContact {
			t.Error("Expected Customer schema to have 'contactInfo' property")
		}
	} else {
		t.Error("Expected Customer schema to have properties")
	}
}

func TestNestedExternalReferences(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldCache := externalRefCache
	defer func() {
		specBaseDir = oldSpecBaseDir
		externalRefCache = oldCache
	}()

	externalRefCache = make(map[string]map[string]interface{})

	schemaPath := filepath.Join("..", "tests", "external_schemas.yaml")
	absPath, _ := filepath.Abs(schemaPath)

	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Skipf("Test schema file not found: %v", err)
		return
	}

	externalSpec, _ := SafelyUnmarshalSpec(data)
	if externalSpec == nil {
		t.Fatal("Failed to unmarshal external schema")
	}

	externalRefCache[absPath] = externalSpec

	customerSchema, ok := externalSpec["components"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected components in external schema")
	}

	schemas, ok := customerSchema["schemas"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected schemas in components")
	}

	customer, ok := schemas["Customer"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected Customer definition")
	}

	visited := make(map[string]bool)
	expanded := ExpandSchema(externalSpec, customer, visited, externalSpec)

	if expanded == nil {
		t.Fatal("Failed to expand Customer schema")
	}

	if contactInfo, ok := expanded.Properties["contactInfo"]; ok {
		if len(contactInfo.Properties) == 0 {
			t.Error("Expected contactInfo to have expanded properties (email, phone)")
		}
	} else {
		t.Error("Expected Customer to have contactInfo property")
	}
}

func TestQueryObjectParameterHandling(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldCache := externalRefCache
	defer func() {
		specBaseDir = oldSpecBaseDir
		externalRefCache = oldCache
	}()

	externalRefCache = make(map[string]map[string]interface{})

	specPath := filepath.Join("..", "tests", "test_query_object.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected paths in spec")
	}

	searchPath, ok := paths["/search"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected /search path")
	}

	getOp, ok := searchPath["get"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected GET operation")
	}

	params, ok := getOp["parameters"].([]interface{})
	if !ok || len(params) == 0 {
		t.Fatal("Expected parameters in GET /search")
	}

	param0, ok := params[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected first parameter to be a map")
	}

	if in, ok := param0["in"].(string); !ok || in != "query" {
		t.Error("Expected parameter to be in query")
	}

	if schema, ok := param0["schema"].(map[string]interface{}); !ok {
		t.Error("Expected parameter to have schema")
	} else {
		if schemaType, ok := schema["type"].(string); !ok || schemaType != "object" {
			t.Error("Expected parameter schema to be object type")
		}
	}
}

func TestRequestBodyContextPreservation(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldCache := externalRefCache
	defer func() {
		specBaseDir = oldSpecBaseDir
		externalRefCache = oldCache
	}()

	externalRefCache = make(map[string]map[string]interface{})

	specPath := filepath.Join("..", "tests", "test_spec_v3.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected paths in spec")
	}

	foundTest := false
	for _, pathItem := range paths {
		if pathMap, ok := pathItem.(map[string]interface{}); ok {
			for method, op := range pathMap {
				if strings.ToLower(method) == "post" || strings.ToLower(method) == "put" {
					if opMap, ok := op.(map[string]interface{}); ok {
						if reqBody, ok := opMap["requestBody"].(map[string]interface{}); ok {
							if ref, hasRef := reqBody["$ref"].(string); hasRef {
								resolved, contextSpec := ResolveRefWithContext(spec, ref)
								if resolved == nil {
									t.Errorf("Failed to resolve requestBody ref: %s", ref)
								}
								if contextSpec == nil {
									t.Error("Expected context spec to be returned")
								}
								foundTest = true
							}
						}
					}
				}
			}
		}
	}

	if !foundTest {
		t.Skip("No requestBody refs found in spec")
	}
}

func TestDefaultValueHandling(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	defer func() {
		specBaseDir = oldSpecBaseDir
	}()

	specPath := filepath.Join("..", "tests", "test_spec_v2.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected paths in spec")
	}

	usersPath, ok := paths["/users"].(map[string]interface{})
	if !ok {
		t.Skip("No /users path in spec")
	}

	getOp, ok := usersPath["get"].(map[string]interface{})
	if !ok {
		t.Skip("No GET operation on /users")
	}

	params, ok := getOp["parameters"].([]interface{})
	if !ok {
		t.Skip("No parameters on GET /users")
	}

	foundDefault := false
	for _, p := range params {
		if pMap, ok := p.(map[string]interface{}); ok {
			if defaultVal := pMap["default"]; defaultVal != nil {
				foundDefault = true
				if name, ok := pMap["name"].(string); ok {
					if name == "limit" {
						if defaultInt, ok := defaultVal.(int); ok && defaultInt != 10 {
							t.Errorf("Expected default value of 10 for limit, got %d", defaultInt)
						}
					}
				}
			}
		}
	}

	if !foundDefault {
		t.Skip("No parameters with default values found")
	}
}

func TestJSONCurlQuoting(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldBasePath := basePath
	defer func() {
		specBaseDir = oldSpecBaseDir
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		basePath = oldBasePath
	}()

	specPath := filepath.Join("..", "tests", "test_spec_v3.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	// Set up for local file mode
	swaggerURL = ""
	apiTarget = ""
	basePath = ""

	// Simulate GenerateRequests parsing
	if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
		if srv, ok := servers[0].(map[string]interface{}); ok {
			if serverURL, ok := srv["url"].(string); ok {
				if strings.Contains(serverURL, "://") {
					apiTarget = serverURL
				}
			}
		}
	}

	// Find a POST endpoint with JSON request body
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected paths in spec")
	}

	foundJSONPost := false
	var testCurl string
	for pathName, pathItem := range paths {
		if pathMap, ok := pathItem.(map[string]interface{}); ok {
			if postOp, ok := pathMap["post"].(map[string]interface{}); ok {
				if reqBody, ok := postOp["requestBody"].(map[string]interface{}); ok {
					if content, ok := reqBody["content"].(map[string]interface{}); ok {
						if jsonContent, ok := content["application/json"].(map[string]interface{}); ok {
							if schema, ok := jsonContent["schema"].(map[string]interface{}); ok {
								// Generate a minimal curl command to verify quoting
								expanded := ExpandSchema(spec, schema, map[string]bool{}, spec)
								example := GenerateExample(expanded)
								bodyBytes, err := json.Marshal(example)
								if err == nil {
									// This simulates the curl generation logic
									testCurl = fmt.Sprintf("curl -X POST \"%s%s%s\" -H \"Content-Type: application/json\" -d '%s'",
										apiTarget, basePath, pathName, bodyBytes)
									foundJSONPost = true
									break
								}
							}
						}
					}
				}
			}
		}
		if foundJSONPost {
			break
		}
	}

	if !foundJSONPost {
		t.Skip("No POST endpoint with JSON body found")
	}

	// Verify the curl command has proper quoting
	// Should end with -d '...' NOT -d '...'"
	if !strings.Contains(testCurl, "-d '") {
		t.Error("Expected -d ' in curl command")
	}

	// Check that it doesn't have the trailing quote bug: -d '%s'"
	if strings.Contains(testCurl, "'\"") {
		t.Error("Found trailing quote bug: curl has '\" which indicates malformed quoting")
	}

	// Verify it ends with a single quote after the JSON data
	if !strings.HasSuffix(testCurl, "'}") && !strings.HasSuffix(testCurl, "']") && !strings.HasSuffix(testCurl, "'") {
		t.Errorf("Curl command should end with properly closed JSON in single quotes, got: %s", testCurl[len(testCurl)-20:])
	}
}

func TestRelativeServerURLHandling(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldBasePath := basePath
	defer func() {
		specBaseDir = oldSpecBaseDir
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		basePath = oldBasePath
	}()

	specPath := filepath.Join("..", "tests", "test_relative_server.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	// Test 1: Verify that when -T is used with a spec that has a relative URL,
	// the basePath from the spec is preserved
	swaggerURL = ""
	apiTarget = "https://example.com" // Simulating -T flag
	basePath = ""

	// Parse server info from spec (simulating what GenerateRequests does)
	if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
		if srv, ok := servers[0].(map[string]interface{}); ok {
			if serverURL, ok := srv["url"].(string); ok {
				if !strings.Contains(serverURL, "://") && serverURL != "/" {
					// Relative URL - should become basePath
					basePath = serverURL
				}
			}
		}
	}

	// Verify basePath was extracted even though -T was used
	if basePath != "/api/v1" {
		t.Errorf("Expected basePath to be '/api/v1' from spec even with -T flag, got: '%s'", basePath)
	}

	// Verify apiTarget was not overwritten by spec (since -T was used)
	if apiTarget != "https://example.com" {
		t.Errorf("Expected apiTarget to remain 'https://example.com' from -T flag, got: '%s'", apiTarget)
	}

	// Test 2: Verify that endpoints command can work even without apiTarget
	// Reset for this test
	apiTarget = ""
	basePath = ""
	swaggerURL = ""

	// When apiTarget is empty and we have relative URL, it should still extract basePath
	if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
		if srv, ok := servers[0].(map[string]interface{}); ok {
			if serverURL, ok := srv["url"].(string); ok {
				if !strings.Contains(serverURL, "://") && serverURL != "/" {
					basePath = normalizeBasePath(serverURL)
					// For endpoints command, we don't need apiTarget
				}
			}
		}
	}

	if basePath != "/api/v1" {
		t.Errorf("Expected basePath to be extracted for endpoints command, got: '%s'", basePath)
	}
}

func TestTargetFlagPreservesSpecBasePath(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldBasePath := basePath
	defer func() {
		specBaseDir = oldSpecBaseDir
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		basePath = oldBasePath
	}()

	// Test with Swagger v2 spec
	specPath := filepath.Join("..", "tests", "test_spec_v2.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	// Simulate -T flag being used
	swaggerURL = ""
	apiTarget = "https://staging.example.com" // User provided target
	basePath = ""

	// Parse basePath from spec (should happen even with -T)
	if v, ok := spec["swagger"].(string); ok && strings.HasPrefix(v, "2") {
		if bp, ok := spec["basePath"].(string); ok && bp != "/" && bp != "" {
			basePath = bp
		}
	}

	// Verify basePath was extracted
	if basePath != "/v1" {
		t.Errorf("Expected basePath '/v1' to be preserved from spec even with -T flag, got: '%s'", basePath)
	}

	// Verify full constructed path would be correct
	fullPath := apiTarget + basePath + "/users"
	expectedPath := "https://staging.example.com/v1/users"
	if fullPath != expectedPath {
		t.Errorf("Expected full path '%s', got '%s'", expectedPath, fullPath)
	}
}

func TestOpenAPIv3AbsoluteServerURLWithTargetFlag(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldBasePath := basePath
	defer func() {
		specBaseDir = oldSpecBaseDir
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		basePath = oldBasePath
	}()

	// Test with OpenAPI v3 spec that has absolute server URL with path
	specPath := filepath.Join("..", "tests", "test_spec_v3.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	// Simulate -T flag being used (user wants to override host)
	swaggerURL = ""
	apiTarget = "https://staging.example.com" // User provided target
	basePath = ""

	// Parse server info from spec (simulating what GenerateRequests does)
	if v, ok := spec["openapi"].(string); ok && strings.HasPrefix(v, "3") {
		if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
			if srv, ok := servers[0].(map[string]interface{}); ok {
				if serverURL, ok := srv["url"].(string); ok {
					if strings.Contains(serverURL, "://") {
						// This is an absolute URL
						// When -T is set, we should still extract the path
						if apiTarget != "" {
							// Parse the server URL to extract path
							if parsedURL, err := url.Parse(serverURL); err == nil && parsedURL.Path != "" && parsedURL.Path != "/" {
								basePath = normalizeBasePath(parsedURL.Path)
							}
						}
					}
				}
			}
		}
	}

	// Verify basePath was extracted from the absolute server URL
	if basePath != "/v1" {
		t.Errorf("Expected basePath '/v1' to be extracted from absolute server URL even with -T flag, got: '%s'", basePath)
	}

	// Verify apiTarget was not overwritten (should still be from -T flag)
	if apiTarget != "https://staging.example.com" {
		t.Errorf("Expected apiTarget to remain 'https://staging.example.com' from -T flag, got: '%s'", apiTarget)
	}

	// Verify full constructed path would be correct
	fullPath := apiTarget + basePath + "/users"
	expectedPath := "https://staging.example.com/v1/users"
	if fullPath != expectedPath {
		t.Errorf("Expected full path '%s', got '%s'", expectedPath, fullPath)
	}
}

func TestOpenAPIv3AbsoluteServerURLWithoutTargetFlag(t *testing.T) {
	oldSpecBaseDir := specBaseDir
	oldSwaggerURL := swaggerURL
	oldAPITarget := apiTarget
	oldBasePath := basePath
	defer func() {
		specBaseDir = oldSpecBaseDir
		swaggerURL = oldSwaggerURL
		apiTarget = oldAPITarget
		basePath = oldBasePath
	}()

	specPath := filepath.Join("..", "tests", "test_spec_v3.yaml")
	absPath, _ := filepath.Abs(specPath)
	specBaseDir = filepath.Dir(absPath)

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("Test spec not found: %v", err)
		return
	}

	spec, _ := SafelyUnmarshalSpec(data)
	if spec == nil {
		t.Fatal("Failed to unmarshal spec")
	}

	swaggerURL = ""
	apiTarget = ""
	basePath = ""

	if v, ok := spec["openapi"].(string); ok && strings.HasPrefix(v, "3") {
		if servers, ok := spec["servers"].([]interface{}); ok && len(servers) > 0 {
			if srv, ok := servers[0].(map[string]interface{}); ok {
				if serverURL, ok := srv["url"].(string); ok && strings.Contains(serverURL, "://") {
					if parsedURL, err := url.Parse(serverURL); err == nil {
						basePath = normalizeBasePath(parsedURL.Path)
						if apiTarget == "" {
							apiTarget = parsedURL.Scheme + "://" + parsedURL.Host
						}
					}
				}
			}
		}
	}

	if apiTarget != "https://api.example.com" {
		t.Errorf("Expected apiTarget 'https://api.example.com', got '%s'", apiTarget)
	}
	if basePath != "/v1" {
		t.Errorf("Expected basePath '/v1', got '%s'", basePath)
	}
	if endpointPath := basePath + "/users"; endpointPath != "/v1/users" {
		t.Errorf("Expected endpoints path '/v1/users', got '%s'", endpointPath)
	}
}

// withResolveGlobals saves and restores the globals GenerateRequests reads while
// resolving a target, so these tests don't leak state into one another.
func withResolveGlobals(t *testing.T, mode string, fn func()) {
	t.Helper()
	oldMode, oldSwaggerURL, oldAPITarget, oldBasePath, oldQuiet := Mode, swaggerURL, apiTarget, basePath, quiet
	defer func() {
		Mode, swaggerURL, apiTarget, basePath, quiet = oldMode, oldSwaggerURL, oldAPITarget, oldBasePath, oldQuiet
	}()
	Mode = mode
	swaggerURL = ""
	apiTarget = ""
	basePath = ""
	quiet = true
	fn()
}

// TestGenerateRequestsMultipleServersDefaultsToFirst verifies that a spec
// declaring several servers no longer aborts the run (previously a fatal die()):
// GenerateRequests resolves against the first declared server instead.
func TestGenerateRequestsMultipleServersDefaultsToFirst(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"servers":[{"url":"https://api1.example.com/v1"},{"url":"https://api2.example.com/v2"}],"paths":{"/users":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)

	withResolveGlobals(t, "endpoints", func() {
		var err error
		_ = captureStdout(t, func() { err = GenerateRequests(spec, http.Client{}, nil) })
		if err != nil {
			t.Fatalf("expected no error for multiple servers, got: %v", err)
		}
		if apiTarget != "https://api1.example.com" {
			t.Errorf("expected apiTarget to default to first server host, got '%s'", apiTarget)
		}
		if basePath != "/v1" {
			t.Errorf("expected basePath from first server, got '%s'", basePath)
		}
	})
}

// TestGenerateRequestsRelativeServerURLWithoutTargetReturnsError verifies the
// relative-server-URL-with-no-base case now returns an error (it used to die()
// and exit the process), so callers can surface it gracefully.
func TestGenerateRequestsRelativeServerURLWithoutTargetReturnsError(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"servers":[{"url":"/api"}],"paths":{"/users":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)

	withResolveGlobals(t, "automate", func() {
		var err error
		_ = captureStdout(t, func() { err = GenerateRequests(spec, http.Client{}, nil) })
		if err == nil {
			t.Fatal("expected an error for relative server URL with no base and no -T, got nil")
		}
	})
}

// TestGenerateRequestsRelativeServerURLEndpointsModeNoError confirms the
// endpoints-mode exemption is preserved: listing endpoints needs no host, so the
// same ambiguous spec resolves without error.
func TestGenerateRequestsRelativeServerURLEndpointsModeNoError(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"servers":[{"url":"/api"}],"paths":{"/users":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)

	withResolveGlobals(t, "endpoints", func() {
		var err error
		_ = captureStdout(t, func() { err = GenerateRequests(spec, http.Client{}, nil) })
		if err != nil {
			t.Fatalf("expected no error in endpoints mode, got: %v", err)
		}
	})
}

// TestGenerateRequestsNoServerNoURLReturnsError verifies the no-server-info and
// no-URL case now returns an error rather than exiting the process.
func TestGenerateRequestsNoServerNoURLReturnsError(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/users":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)

	withResolveGlobals(t, "automate", func() {
		var err error
		_ = captureStdout(t, func() { err = GenerateRequests(spec, http.Client{}, nil) })
		if err == nil {
			t.Fatal("expected an error when no server info and no URL are available, got nil")
		}
	})
}

func TestNormalizeBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "/", want: ""},
		{in: " /v1/ ", want: "/v1"},
		{in: "/v1/", want: "/v1"},
		{in: "/v1", want: "/v1"},
		{in: "v1/", want: "/v1"},
		{in: "v1", want: "/v1"},
	}

	for _, tc := range cases {
		got := normalizeBasePath(tc.in)
		if got != tc.want {
			t.Errorf("normalizeBasePath(%q): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestExpandSchemaAllOf(t *testing.T) {
	spec := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Base": map[string]interface{}{
					"type":     "object",
					"required": []interface{}{"id"},
					"properties": map[string]interface{}{
						"id": map[string]interface{}{"type": "integer"},
					},
				},
				"Extra": map[string]interface{}{
					"properties": map[string]interface{}{
						"note": map[string]interface{}{"type": "string"},
					},
				},
				"StrEnum": map[string]interface{}{
					"type": "string",
					"enum": []interface{}{"a", "b"},
				},
				"Dict": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}
	ref := func(name string) map[string]interface{} {
		return map[string]interface{}{"$ref": "#/components/schemas/" + name}
	}
	expand := func(schema map[string]interface{}) *SchemaNode {
		return ExpandSchema(spec, schema, map[string]bool{}, spec)
	}

	t.Run("sibling properties and required are merged", func(t *testing.T) {
		node := expand(map[string]interface{}{
			"allOf":    []interface{}{ref("Base")},
			"required": []interface{}{"name"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string"},
			},
		})
		if node.Type != "object" {
			t.Errorf("Type = %q, want object", node.Type)
		}
		for _, k := range []string{"id", "name"} {
			if node.Properties[k] == nil {
				t.Errorf("missing property %q", k)
			}
			if !node.Required[k] {
				t.Errorf("%q should be required", k)
			}
		}
	})

	t.Run("sibling example and enum survive", func(t *testing.T) {
		node := expand(map[string]interface{}{
			"allOf":   []interface{}{ref("StrEnum")},
			"enum":    []interface{}{"x"},
			"example": "x",
		})
		if node.Example != "x" {
			t.Errorf("Example = %v, want x", node.Example)
		}
		if len(node.Enum) != 1 || node.Enum[0] != "x" {
			t.Errorf("Enum = %v, want [x]", node.Enum)
		}
	})

	t.Run("primitive type and enum come from subschema", func(t *testing.T) {
		node := expand(map[string]interface{}{"allOf": []interface{}{ref("StrEnum")}})
		if node.Type != "string" {
			t.Errorf("Type = %q, want string", node.Type)
		}
		if len(node.Enum) != 2 {
			t.Errorf("Enum = %v, want [a b]", node.Enum)
		}
		if got := GenerateExample(node); got != "a" {
			t.Errorf("GenerateExample = %v, want a", got)
		}
	})

	t.Run("sibling items are kept", func(t *testing.T) {
		node := expand(map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "integer"},
			"allOf": []interface{}{map[string]interface{}{"description": "list"}},
		})
		if node.Type != "array" {
			t.Errorf("Type = %q, want array", node.Type)
		}
		if node.Items == nil || node.Items.Type != "integer" {
			t.Errorf("Items = %+v, want integer items", node.Items)
		}
	})

	t.Run("additionalProperties from subschema is carried over", func(t *testing.T) {
		node := expand(map[string]interface{}{"allOf": []interface{}{ref("Dict")}})
		if node.AdditionalProperties == nil || node.AdditionalProperties.Type != "integer" {
			t.Errorf("AdditionalProperties = %+v, want integer", node.AdditionalProperties)
		}
	})

	t.Run("untyped composition defaults to object", func(t *testing.T) {
		node := expand(map[string]interface{}{"allOf": []interface{}{ref("Extra"), ref("Base")}})
		if node.Type != "object" {
			t.Errorf("Type = %q, want object", node.Type)
		}
		if node.Properties["id"] == nil || node.Properties["note"] == nil {
			t.Errorf("Properties = %v, want id and note", node.Properties)
		}
	})
}

func TestExpandSchemaSiblingRefReuse(t *testing.T) {
	spec := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Address": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"street": map[string]interface{}{"type": "string"},
						"city":   map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	ref := map[string]interface{}{"$ref": "#/components/schemas/Address"}
	order := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"billing":  ref,
			"shipping": ref,
		},
	}

	node := ExpandSchema(spec, order, map[string]bool{}, spec)

	for _, name := range []string{"billing", "shipping"} {
		sub := node.Properties[name]
		if sub == nil {
			t.Fatalf("missing property %q", name)
		}
		if len(sub.Properties) == 0 {
			t.Errorf("%q lost its expanded properties; the same ref in a sibling branch should not trip the cycle guard", name)
		}
	}
}

func TestExpandSchemaSelfCycleBounded(t *testing.T) {
	spec := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Node": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"value": map[string]interface{}{"type": "string"},
						"children": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"$ref": "#/components/schemas/Node"},
						},
					},
				},
			},
		},
	}

	done := make(chan *SchemaNode, 1)
	go func() {
		root := spec["components"].(map[string]interface{})["schemas"].(map[string]interface{})["Node"].(map[string]interface{})
		done <- ExpandSchema(spec, root, map[string]bool{}, spec)
	}()

	var node *SchemaNode
	select {
	case node = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ExpandSchema did not terminate on a self-referential schema")
	}

	// The first ref expansion (children.items) is a full Node; the ref nested one
	// level deeper is on the current path, so it collapses to a bare object.
	level1 := node.Properties["children"].Items
	if level1 == nil || len(level1.Properties) == 0 {
		t.Fatalf("first Node ref should expand fully, got %+v", level1)
	}
	level2 := level1.Properties["children"].Items
	if level2 == nil {
		t.Fatal("expected nested children.items node")
	}
	if len(level2.Properties) != 0 {
		t.Errorf("recursive Node ref should collapse to a bare object, got properties %v", level2.Properties)
	}
}

func TestEncodePair(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  string
	}{
		{name: "q", value: "hello world", want: "q=hello+world"},
		{name: "a&b", value: "c=d", want: "a%26b=c%3Dd"},
		{name: "url", value: "https://bishopfox.com", want: "url=https%3A%2F%2Fbishopfox.com"},
		{name: "email", value: "noreply@localhost.localdomain", want: "email=noreply%40localhost.localdomain"},
		{name: "p", value: "a+b", want: "p=a%2Bb"},
		{name: "frag", value: "a#b", want: "frag=a%23b"},
		{name: "u", value: "naïve", want: "u=na%C3%AFve"},
		{name: "n", value: 1, want: "n=1"},
		{name: "t", value: true, want: "t=true"},
		{name: "plain", value: "bishopfox", want: "plain=bishopfox"},
	}

	for _, tc := range cases {
		got := encodePair(tc.name, tc.value)
		if got != tc.want {
			t.Errorf("encodePair(%q, %v): expected %q, got %q", tc.name, tc.value, tc.want, got)
		}
	}
}

func TestAppendQueryParam(t *testing.T) {
	cases := []struct {
		url   string
		name  string
		value interface{}
		want  string
	}{
		{url: "https://h/pet", name: "status", value: "sold", want: "https://h/pet?status=sold"},
		{url: "https://h/pet?a=1", name: "b", value: "2", want: "https://h/pet?a=1&b=2"},
	}

	for _, tc := range cases {
		got := appendQueryParam(tc.url, tc.name, tc.value)
		if got != tc.want {
			t.Errorf("appendQueryParam(%q, %q, %v): expected %q, got %q", tc.url, tc.name, tc.value, tc.want, got)
		}
	}

	// A hostile value must not be able to add parameters or a fragment.
	got := appendQueryParam("https://h/pet", "q", "x&injected=1#frag?y=2")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("appendQueryParam produced an unparseable URL %q: %v", got, err)
	}
	if u.Fragment != "" {
		t.Errorf("expected no fragment in %q, got %q", got, u.Fragment)
	}
	if len(u.Query()) != 1 {
		t.Errorf("expected exactly 1 query parameter in %q, got %d", got, len(u.Query()))
	}
	if u.Query().Get("q") != "x&injected=1#frag?y=2" {
		t.Errorf("value did not round-trip: got %q", u.Query().Get("q"))
	}
}

func TestReplacePathParam(t *testing.T) {
	const base = "https://h/v2/pet/{petId}"

	got := replacePathParam(base, "petId", "a/b?c#d")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("replacePathParam produced an unparseable URL %q: %v", got, err)
	}
	if want := strings.Count("/v2/pet/x", "/"); strings.Count(u.EscapedPath(), "/") != want {
		t.Errorf("value re-segmented the path: %q has %d separators, expected %d", u.EscapedPath(), strings.Count(u.EscapedPath(), "/"), want)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		t.Errorf("value leaked into query/fragment: query=%q fragment=%q", u.RawQuery, u.Fragment)
	}
	if u.Path != "/v2/pet/a/b?c#d" {
		t.Errorf("value did not round-trip: got %q", u.Path)
	}

	if got := replacePathParam(base, "other", "x"); got != base {
		t.Errorf("an unmatched placeholder should be left intact, got %q", got)
	}

	if got := replacePathParam("https://h/{a}/{a}", "a", "x"); got != "https://h/x/{a}" {
		t.Errorf("expected only the first placeholder to be replaced, got %q", got)
	}
}

func TestAppendFormField(t *testing.T) {
	got := appendFormField("", "name", "a b")
	if want := "name=a+b"; got != want {
		t.Errorf("appendFormField on an empty body: expected %q, got %q", want, got)
	}

	got = appendFormField(got, "note", "x&y=z")
	if want := "name=a+b&note=x%26y%3Dz"; got != want {
		t.Errorf("appendFormField on an existing body: expected %q, got %q", want, got)
	}
}

func TestEncodeFormBody(t *testing.T) {
	obj := map[string]interface{}{
		"zeta":  "last",
		"alpha": "first",
		"mid":   2,
	}

	want := "alpha=first&mid=2&zeta=last"
	for i := 0; i < 50; i++ {
		if got := encodeFormBody(obj); got != want {
			t.Fatalf("encodeFormBody is not deterministic: expected %q, got %q", want, got)
		}
	}
}

func TestShellSingleQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "''"},
		{in: "abc", want: "'abc'"},
		{in: `it's`, want: `'it'\''s'`},
		{in: `{"a":"b"}`, want: `'{"a":"b"}'`},
	}

	for _, tc := range cases {
		got := shellSingleQuote(tc.in)
		if got != tc.want {
			t.Errorf("shellSingleQuote(%q): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestXmlFromObjectEscapesValues(t *testing.T) {
	got := XmlFromObject(map[string]interface{}{"note": "a < b & c"})
	if want := "<note>a &lt; b &amp; c</note>"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	// A scalar array element used to be emitted as an empty element.
	got = XmlFromObject(map[string]interface{}{"tag": []interface{}{"x", "y"}})
	if want := "<tag>x</tag><tag>y</tag>"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	obj := map[string]interface{}{"zeta": 1, "alpha": 2, "mid": 3}
	want := "<alpha>2</alpha><mid>3</mid><zeta>1</zeta>"
	for i := 0; i < 50; i++ {
		if got := XmlFromObject(obj); got != want {
			t.Fatalf("XmlFromObject is not deterministic: expected %q, got %q", want, got)
		}
	}
}

func TestRawValuesSkipsEncoding(t *testing.T) {
	oldRawValues := rawValues
	rawValues = true
	defer func() { rawValues = oldRawValues }()

	if got, want := encodePair("a b", "c&d"), "a b=c&d"; got != want {
		t.Errorf("encodePair: expected %q, got %q", want, got)
	}
	if got, want := appendQueryParam("https://h/p", "q", "a b"), "https://h/p?q=a b"; got != want {
		t.Errorf("appendQueryParam: expected %q, got %q", want, got)
	}
	if got, want := appendFormField("", "q", "a b"), "q=a b"; got != want {
		t.Errorf("appendFormField: expected %q, got %q", want, got)
	}
	if got, want := replacePathParam("https://h/{id}", "id", "a/b"), "https://h/a/b"; got != want {
		t.Errorf("replacePathParam: expected %q, got %q", want, got)
	}

	// --raw-values covers percent-encoding only.
	if got, want := shellSingleQuote(`it's`), `'it'\''s'`; got != want {
		t.Errorf("shellSingleQuote should still escape: expected %q, got %q", want, got)
	}
	if got, want := escapeXmlText("a < b"), "a &lt; b"; got != want {
		t.Errorf("escapeXmlText should still escape: expected %q, got %q", want, got)
	}
}

// TestParameterLocationsAreSent drives BuildRequestsFromPaths against a live
// server and asserts that "in: header", "in: cookie", and "in: formData"
// parameters are actually placed on the outgoing request - not merely printed
// in the generated curl command.
func TestParameterLocationsAreSent(t *testing.T) {
	oldMode := Mode
	oldAPITarget := apiTarget
	oldBasePath := basePath
	oldSwaggerURL := swaggerURL
	oldOutputFormat := outputFormat
	oldHeaders := Headers
	oldContentType := contentType
	oldAccept := accept
	oldUA := UserAgent
	oldForce := force
	oldTimeout := timeout
	oldTestString := testString
	oldRandomUA := randomUserAgent
	oldPreview := responsePreviewLength
	oldResults := jsonResultsStringArray
	defer func() {
		Mode = oldMode
		apiTarget = oldAPITarget
		basePath = oldBasePath
		swaggerURL = oldSwaggerURL
		outputFormat = oldOutputFormat
		Headers = oldHeaders
		contentType = oldContentType
		accept = oldAccept
		UserAgent = oldUA
		force = oldForce
		timeout = oldTimeout
		testString = oldTestString
		randomUserAgent = oldRandomUA
		responsePreviewLength = oldPreview
		jsonResultsStringArray = oldResults
	}()

	type captured struct {
		header string
		cookie string
		ctype  string
		body   string
	}
	got := map[string]captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := captured{
			header: r.Header.Get("X-Api-Key"),
			ctype:  r.Header.Get("Content-Type"),
			body:   string(body),
		}
		if ck, err := r.Cookie("session"); err == nil {
			c.cookie = ck.Value
		}
		got[r.Method+" "+r.URL.Path] = c
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	Mode = "automate"
	apiTarget = srv.URL
	basePath = ""
	swaggerURL = ""
	outputFormat = "json"
	Headers = nil
	contentType = ""
	accept = ""
	UserAgent = "sj-test"
	randomUserAgent = false
	force = true // skip the dangerous-keyword prompt
	timeout = 30
	testString = "bishopfox"
	responsePreviewLength = 50
	jsonResultsStringArray = nil

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/things": map[string]interface{}{
				"get": map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{"name": "X-Api-Key", "in": "header", "type": "string"},
						map[string]interface{}{"name": "session", "in": "cookie", "type": "string"},
					},
				},
			},
			"/profile": map[string]interface{}{
				"post": map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{"name": "field1", "in": "formData", "type": "string"},
					},
				},
			},
		},
	}

	BuildRequestsFromPaths(spec, http.Client{}, nil)

	g := got["GET /things"]
	if g.header != "bishopfox" {
		t.Errorf("in:header parameter not sent: X-Api-Key=%q, want %q", g.header, "bishopfox")
	}
	if g.cookie != "bishopfox" {
		t.Errorf("in:cookie parameter not sent: session=%q, want %q", g.cookie, "bishopfox")
	}

	p := got["POST /profile"]
	if !strings.Contains(p.ctype, "application/x-www-form-urlencoded") {
		t.Errorf("in:formData did not set urlencoded Content-Type, got %q", p.ctype)
	}
	if p.body != "field1=bishopfox" {
		t.Errorf("in:formData parameter not sent in body: got %q, want %q", p.body, "field1=bishopfox")
	}
}

// TestDeclaredBodyIsSentForAllMethods asserts that an operation declaring a
// request body transmits it whatever the method - the printed curl command has
// always carried "-d", but only POST used to put the bytes on the wire. It also
// covers the Swagger v2 "in: body" parameter, whose schema is the whole body and
// is therefore serialized as JSON rather than as form fields.
func TestDeclaredBodyIsSentForAllMethods(t *testing.T) {
	oldMode := Mode
	oldAPITarget := apiTarget
	oldBasePath := basePath
	oldSwaggerURL := swaggerURL
	oldOutputFormat := outputFormat
	oldHeaders := Headers
	oldContentType := contentType
	oldAccept := accept
	oldUA := UserAgent
	oldForce := force
	oldTimeout := timeout
	oldTestString := testString
	oldRandomUA := randomUserAgent
	oldPreview := responsePreviewLength
	oldResults := jsonResultsStringArray
	defer func() {
		Mode = oldMode
		apiTarget = oldAPITarget
		basePath = oldBasePath
		swaggerURL = oldSwaggerURL
		outputFormat = oldOutputFormat
		Headers = oldHeaders
		contentType = oldContentType
		accept = oldAccept
		UserAgent = oldUA
		force = oldForce
		timeout = oldTimeout
		testString = oldTestString
		randomUserAgent = oldRandomUA
		responsePreviewLength = oldPreview
		jsonResultsStringArray = oldResults
	}()

	type captured struct {
		ctype     string
		ctPresent bool
		body      string
	}
	// Keyed to a slice, not a single value: an operation that fans out across
	// content types would otherwise overwrite its own capture and the extra
	// requests would go unnoticed.
	got := map[string][]captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, present := r.Header["Content-Type"]
		key := r.Method + " " + r.URL.Path
		got[key] = append(got[key], captured{
			ctype:     r.Header.Get("Content-Type"),
			ctPresent: present,
			body:      string(body),
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// one asserts the operation produced exactly one request and returns it.
	one := func(key string) captured {
		t.Helper()
		reqs := got[key]
		if len(reqs) != 1 {
			t.Fatalf("%s: expected exactly 1 request, got %d", key, len(reqs))
		}
		return reqs[0]
	}

	Mode = "automate"
	apiTarget = srv.URL
	basePath = ""
	swaggerURL = ""
	outputFormat = "json"
	Headers = nil
	contentType = ""
	accept = ""
	UserAgent = "sj-test"
	randomUserAgent = false
	allContentTypes = false
	force = true // skip the dangerous-keyword prompt
	timeout = 30
	testString = "bishopfox"
	responsePreviewLength = 50
	jsonResultsStringArray = nil

	nameObject := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}
	v2BodyParam := []interface{}{
		map[string]interface{}{"name": "payload", "in": "body", "schema": nameObject},
	}
	jsonRequestBody := func(cType string) map[string]interface{} {
		return map[string]interface{}{
			"content": map[string]interface{}{
				cType: map[string]interface{}{"schema": nameObject},
			},
		}
	}

	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/json": map[string]interface{}{
				"put": map[string]interface{}{"requestBody": jsonRequestBody("application/json")},
			},
			"/multipart": map[string]interface{}{
				"put": map[string]interface{}{"requestBody": jsonRequestBody("multipart/form-data")},
			},
			"/v2body": map[string]interface{}{
				"post": map[string]interface{}{"parameters": v2BodyParam},
				"put":  map[string]interface{}{"parameters": v2BodyParam},
			},
			"/v2array": map[string]interface{}{
				"post": map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name": "payload",
							"in":   "body",
							"schema": map[string]interface{}{
								"type":  "array",
								"items": nameObject,
							},
						},
					},
				},
			},
			"/nobody": map[string]interface{}{
				"get": map[string]interface{}{},
			},
		},
	}

	BuildRequestsFromPaths(spec, http.Client{}, nil)

	// A v3 requestBody on a PUT: the regression this test exists for.
	if j := one("PUT /json"); j.body != `{"name":"bishopfox"}` {
		t.Errorf("PUT body not sent: got %q, want %q", j.body, `{"name":"bishopfox"}`)
	} else if j.ctype != "application/json" {
		t.Errorf("PUT Content-Type: got %q, want %q", j.ctype, "application/json")
	}

	// multipart suppresses "-d" in the printed command but must still be sent.
	m := one("PUT /multipart")
	if !strings.Contains(m.ctype, "multipart/form-data; boundary=") {
		t.Errorf("PUT multipart Content-Type: got %q", m.ctype)
	}
	if !strings.Contains(m.body, "bishopfox") {
		t.Errorf("PUT multipart body not sent: got %q", m.body)
	}

	// A Swagger v2 "in: body" param is JSON, and PUT is treated exactly as POST.
	post, put := one("POST /v2body"), one("PUT /v2body")
	if post.body != put.body || post.ctype != put.ctype {
		t.Errorf("v2 in:body differs by method: POST %q/%q, PUT %q/%q", post.ctype, post.body, put.ctype, put.body)
	}
	if put.body != `{"name":"bishopfox"}` {
		t.Errorf("v2 in:body not serialized as JSON: got %q, want %q", put.body, `{"name":"bishopfox"}`)
	}
	if put.ctype != "application/json" {
		t.Errorf("v2 in:body Content-Type: got %q, want %q", put.ctype, "application/json")
	}

	// A non-object body schema is a JSON array, not a "body=[map[...]]" form field.
	if a := one("POST /v2array"); !strings.HasPrefix(a.body, `[{`) {
		t.Errorf("v2 in:body array not serialized as a JSON array: got %q", a.body)
	}

	// The widened Content-Type default must not touch a bodiless request.
	n := one("GET /nobody")
	if n.body != "" {
		t.Errorf("bodiless GET sent a body: got %q", n.body)
	}
	// Absent, not merely empty: enforcing a blank Content-Type would append a
	// bare "Content-Type:" header that survives to the wire.
	if n.ctPresent {
		t.Errorf("bodiless GET declared a Content-Type: got %q", n.ctype)
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
// The "prepare" and "endpoints" modes write their whole result to stdout, so
// there is no other way to assert on them.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	w.Close()
	os.Stdout = old
	return <-done
}

// multiContentTypeSpec is one operation declaring every encodable body type
// plus one sj has no encoder for.
func multiContentTypeSpec() map[string]interface{} {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}
	content := map[string]interface{}{}
	for _, ct := range []string{
		"application/json",
		"application/xml",
		"application/x-www-form-urlencoded",
		"multipart/form-data",
		"application/octet-stream",
	} {
		content[ct] = map[string]interface{}{"schema": schema}
	}
	return map[string]interface{}{
		"paths": map[string]interface{}{
			"/multi": map[string]interface{}{
				"post": map[string]interface{}{
					"requestBody": map[string]interface{}{"content": content},
				},
			},
		},
	}
}

// withPrepareMode sets up the globals BuildRequestsFromPaths reads and restores
// them afterwards.
func withPrepareMode(t *testing.T, mode string, fn func()) {
	t.Helper()
	oldMode, oldTarget, oldBase := Mode, apiTarget, basePath
	oldHeaders, oldCT, oldAll := Headers, contentType, allContentTypes
	oldPrepareFor, oldTestString, oldWarned := prepareFor, testString, forcedContentTypeWarned
	defer func() {
		Mode, apiTarget, basePath = oldMode, oldTarget, oldBase
		Headers, contentType, allContentTypes = oldHeaders, oldCT, oldAll
		prepareFor, testString, forcedContentTypeWarned = oldPrepareFor, oldTestString, oldWarned
	}()

	Mode = mode
	apiTarget = "http://127.0.0.1:9999"
	basePath = ""
	Headers = nil
	contentType = ""
	allContentTypes = false
	prepareFor = "curl"
	testString = "bishopfox"
	forcedContentTypeWarned = map[string]bool{}

	fn()
}

// TestPrepareEmitsOneContentTypePerCommand is the regression test for the
// printed command accumulating a Content-Type header per declared type while
// only one body was actually sent.
func TestPrepareEmitsOneContentTypePerCommand(t *testing.T) {
	spec := multiContentTypeSpec()

	var defaultOut, fanOut string
	withPrepareMode(t, "prepare", func() {
		defaultOut = captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })
		allContentTypes = true
		fanOut = captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })
	})

	defaultLines := strings.Split(strings.TrimSpace(defaultOut), "\n")
	if len(defaultLines) != 1 {
		t.Fatalf("expected 1 command by default, got %d:\n%s", len(defaultLines), defaultOut)
	}
	if !strings.Contains(defaultLines[0], "-H 'Content-Type: application/json'") {
		t.Errorf("default command should use the preferred JSON body: %s", defaultLines[0])
	}

	fanLines := strings.Split(strings.TrimSpace(fanOut), "\n")
	// Four encodable types; application/octet-stream has no encoder.
	if len(fanLines) != 4 {
		t.Fatalf("expected 4 commands under --all-content-types, got %d:\n%s", len(fanLines), fanOut)
	}

	for _, line := range append(defaultLines, fanLines...) {
		if n := strings.Count(line, "-H 'Content-Type:"); n > 1 {
			t.Errorf("command declares %d Content-Type headers:\n%s", n, line)
		}
	}

	wantBodies := []string{
		"-H 'Content-Type: application/json' -d '{\"name\":\"bishopfox\"}'",
		"-H 'Content-Type: application/x-www-form-urlencoded' -d 'name=bishopfox'",
		"-F 'name=bishopfox'",
		"-H 'Content-Type: application/xml' -d '<name>bishopfox</name>'",
	}
	for i, want := range wantBodies {
		if !strings.Contains(fanLines[i], want) {
			t.Errorf("command %d:\n  got  %s\n  want it to contain %s", i, fanLines[i], want)
		}
	}

	// curl generates its own multipart boundary, so echoing sj's would print a
	// command that cannot reproduce the request.
	if strings.Contains(fanLines[2], "Content-Type") {
		t.Errorf("multipart command should not declare a Content-Type: %s", fanLines[2])
	}
}

// TestPrepareForSqlmapSkipsMultipart pins that sj never prints a sqlmap
// command carrying -F, which is not a sqlmap option and cannot be run.
func TestPrepareForSqlmapSkipsMultipart(t *testing.T) {
	spec := multiContentTypeSpec()

	var out string
	withPrepareMode(t, "prepare", func() {
		prepareFor = "sqlmap"
		allContentTypes = true
		out = captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })
	})

	if strings.Contains(out, " -F ") {
		t.Errorf("sqlmap output contains an unrunnable -F argument:\n%s", out)
	}
	if n := strings.Count(out, "sqlmap"); n != 3 {
		t.Errorf("expected 3 sqlmap commands (multipart skipped), got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "--data='{\"name\":\"bishopfox\"}'") {
		t.Errorf("sqlmap body should be rewritten to --data=:\n%s", out)
	}
}

// TestEndpointsPrintsOncePerOperation pins that fanning out across content
// types does not multiply the endpoint listing.
func TestEndpointsPrintsOncePerOperation(t *testing.T) {
	spec := multiContentTypeSpec()

	var out string
	withPrepareMode(t, "endpoints", func() {
		allContentTypes = true
		out = captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })
	})

	if got := strings.TrimSpace(out); got != "/multi" {
		t.Errorf("endpoints output = %q, want %q", got, "/multi")
	}
}

// TestHybridV2BodyAndFormDataAreSeparateVariants covers an operation declaring
// both an "in: body" param and "in: formData" params. The two used to corrupt
// each other: the JSON body was written first, then form fields were appended
// onto it as "&k=v".
func TestHybridV2BodyAndFormDataAreSeparateVariants(t *testing.T) {
	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/hybrid": map[string]interface{}{
				"post": map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name": "payload",
							"in":   "body",
							"schema": map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
							},
						},
						map[string]interface{}{"name": "field1", "in": "formData", "type": "string"},
					},
				},
			},
		},
	}

	var out string
	withPrepareMode(t, "prepare", func() {
		allContentTypes = true
		out = captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 commands, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], `-d '{"name":"bishopfox"}'`) {
		t.Errorf("JSON variant is not clean JSON: %s", lines[0])
	}
	if !strings.Contains(lines[1], "-d 'field1=bishopfox'") {
		t.Errorf("formData variant is not clean urlencoded: %s", lines[1])
	}
}

// TestResponseDescriptionsAreScopedToTheirOperation covers the response
// description path end to end: descriptions were collected keyed by the status
// string but looked up with an int, so every lookup missed; the map was also
// shared across the whole spec, so fixing the key alone would have printed one
// endpoint's description against another's response.
func TestResponseDescriptionsAreScopedToTheirOperation(t *testing.T) {
	oldMode, oldTarget, oldBase := Mode, apiTarget, basePath
	oldFormat, oldHeaders, oldCT := outputFormat, Headers, contentType
	oldForce, oldTimeout, oldPreview := force, timeout, responsePreviewLength
	oldResults, oldVerbose, oldAll := jsonResultsStringArray, verbose, allContentTypes
	defer func() {
		Mode, apiTarget, basePath = oldMode, oldTarget, oldBase
		outputFormat, Headers, contentType = oldFormat, oldHeaders, oldCT
		force, timeout, responsePreviewLength = oldForce, oldTimeout, oldPreview
		jsonResultsStringArray, verbose, allContentTypes = oldResults, oldVerbose, oldAll
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	Mode = "automate"
	apiTarget = srv.URL
	basePath = ""
	outputFormat = "console"
	Headers = nil
	contentType = ""
	force = true
	verbose = false
	allContentTypes = false
	timeout = 30
	responsePreviewLength = 50
	jsonResultsStringArray = nil

	responses := func(pairs map[string]string) map[string]interface{} {
		out := map[string]interface{}{}
		for status, desc := range pairs {
			out[status] = map[string]interface{}{"description": desc}
		}
		return out
	}
	spec := map[string]interface{}{
		"paths": map[string]interface{}{
			"/alpha": map[string]interface{}{
				"get": map[string]interface{}{"responses": responses(map[string]string{"404": "Alpha is missing"})},
			},
			"/beta": map[string]interface{}{
				"get": map[string]interface{}{"responses": responses(map[string]string{"404": "Beta is missing"})},
			},
			// No 404 declared, so the spec's catch-all applies.
			"/gamma": map[string]interface{}{
				"get": map[string]interface{}{"responses": responses(map[string]string{"default": "Something went wrong"})},
			},
			"/ok": map[string]interface{}{
				"get": map[string]interface{}{"responses": responses(map[string]string{"200": "successful operation"})},
			},
		},
	}

	out := captureStdout(t, func() { BuildRequestsFromPaths(spec, http.Client{}, nil) })

	byPath := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		for _, p := range []string{"/alpha", "/beta", "/gamma", "/ok"} {
			if strings.Contains(line, p+" ") || strings.HasSuffix(line, p) {
				byPath[p] = line
			}
		}
	}

	if !strings.Contains(byPath["/alpha"], "Alpha is missing") {
		t.Errorf("/alpha line lost its description: %q", byPath["/alpha"])
	}
	if !strings.Contains(byPath["/beta"], "Beta is missing") {
		t.Errorf("/beta line lost its description: %q", byPath["/beta"])
	}
	// The leak the shared map would cause.
	if strings.Contains(byPath["/beta"], "Alpha is missing") {
		t.Errorf("/beta showed /alpha's description: %q", byPath["/beta"])
	}
	if !strings.Contains(byPath["/gamma"], "Something went wrong") {
		t.Errorf("/gamma did not fall back to the default response: %q", byPath["/gamma"])
	}
	// A 2xx description only repeats what the status already says.
	if strings.Contains(byPath["/ok"], "successful operation") {
		t.Errorf("/ok annotated a successful response: %q", byPath["/ok"])
	}
}
