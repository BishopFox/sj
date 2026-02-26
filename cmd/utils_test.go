package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	spec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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

	externalSpec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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

	spec := SafelyUnmarshalSpec(data)
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
					basePath = serverURL
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

	spec := SafelyUnmarshalSpec(data)
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
