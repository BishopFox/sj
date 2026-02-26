package cmd

import (
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
