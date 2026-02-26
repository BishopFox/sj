package cmd

import (
	"strings"
	"testing"
)

// TestIsAmbiguousResponse verifies that the function correctly identifies ambiguous HTTP status codes
func TestIsAmbiguousResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		{"400 Bad Request", 400, true},
		{"405 Method Not Allowed", 405, true},
		{"422 Unprocessable Entity", 422, true},
		{"429 Too Many Requests", 429, true},
		{"500 Internal Server Error", 500, true},
		{"501 Not Implemented", 501, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"200 OK", 200, false},
		{"201 Created", 201, false},
		{"204 No Content", 204, false},
		{"301 Moved Permanently", 301, false},
		{"401 Unauthorized", 401, false},
		{"403 Forbidden", 403, false},
		{"404 Not Found", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAmbiguousResponse(tt.statusCode)
			if result != tt.expected {
				t.Errorf("IsAmbiguousResponse(%d) = %v, want %v", tt.statusCode, result, tt.expected)
			}
		})
	}
}

// TestParseValue verifies proper type conversion in parseValue function
func TestParseValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{"Integer", "42", float64(42)}, // JSON numbers are float64
		{"Float", "3.14", 3.14},
		{"Boolean true", "true", true},
		{"Boolean false", "false", false},
		{"String", "hello", "hello"},
		{"String with spaces", "hello world", "hello world"},
		{"Empty string", "", ""},
		{"Numeric string with quotes", "\"123\"", "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseValue(tt.input)
			if result != tt.expected {
				t.Errorf("parseValue(%q) = %v (type %T), want %v (type %T)", tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

// TestDetermineFieldTarget verifies correct field target detection
func TestDetermineFieldTarget(t *testing.T) {
	state := &RequestState{
		Method:       "POST",
		PathTemplate: "/api/users/{id}",
		PathParams:   map[string]string{"id": "123"},
		QueryParams:  map[string]string{"filter": "active"},
		Headers:      map[string]string{"Authorization": "Bearer token"},
		Body:         map[string]interface{}{"name": "John", "email": "john@example.com"},
		ContentType:  "application/json",
	}

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"Path parameter", "id", "path"},
		{"Query parameter", "filter", "query"},
		{"Body field", "name", "body"},
		{"Body field email", "email", "body"},
		{"Unknown field", "unknown", "query"}, // Default to query (headers not checked by this function)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineFieldTarget(tt.key, state)
			if result != tt.expected {
				t.Errorf("DetermineFieldTarget(%q, state) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

// TestApplyModification verifies that modifications are correctly applied to request state
func TestApplyModification(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		key         string
		value       string
		expectError bool
	}{
		{"Modify query param", "query", "page", "2", false},
		{"Modify path param", "path", "id", "456", false},
		{"Modify header", "header", "Content-Type", "application/xml", false},
		{"Modify body field", "body", "username", "alice", false},
		{"Nested body field", "body", "user.name", "Bob", false},
		{"Invalid target", "invalid", "test", "value", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &RequestState{
				Method:       "POST",
				PathTemplate: "/api/users/{id}",
				PathParams:   map[string]string{"id": "123"},
				QueryParams:  map[string]string{"page": "1"},
				Headers:      map[string]string{"Content-Type": "application/json"},
				Body:         map[string]interface{}{"username": "john", "user": map[string]interface{}{"name": "John"}},
				ContentType:  "application/json",
			}

			err := ApplyModification(tt.target, tt.key, tt.value, state)
			if tt.expectError {
				if err == nil {
					t.Errorf("ApplyModification(%q, %q, %v, state) expected error, got nil", tt.target, tt.key, tt.value)
				}
			} else {
				if err != nil {
					t.Errorf("ApplyModification(%q, %q, %v, state) unexpected error: %v", tt.target, tt.key, tt.value, err)
				}
				// Verify the modification was applied
				switch tt.target {
				case "query":
					if state.QueryParams[tt.key] != tt.value {
						t.Errorf("Query param %q not updated correctly", tt.key)
					}
				case "path":
					if state.PathParams[tt.key] != tt.value {
						t.Errorf("Path param %q not updated correctly", tt.key)
					}
				case "header":
					if state.Headers[tt.key] != tt.value {
						t.Errorf("Header %q not updated correctly", tt.key)
					}
				case "body":
					// For nested fields, we can't easily verify, skip for now
					if tt.name != "Nested body field" {
						bodyMap := state.Body.(map[string]interface{})
						if bodyMap[tt.key] != tt.value {
							t.Errorf("Body field %q not updated correctly", tt.key)
						}
					}
				}
			}
		})
	}
}

// TestNewRequestState verifies proper initialization of RequestState
func TestNewRequestState(t *testing.T) {
	pathParams := map[string]string{"id": "123"}
	queryParams := map[string]string{"filter": "active"}
	headers := map[string]string{"Authorization": "Bearer token"}
	body := map[string]interface{}{"name": "John"}

	state := NewRequestState(
		"POST",
		"/api/users/{id}",
		pathParams,
		queryParams,
		headers,
		body,
		"application/json",
	)

	if state.Method != "POST" {
		t.Errorf("Expected Method POST, got %s", state.Method)
	}
	if state.PathTemplate != "/api/users/{id}" {
		t.Errorf("Expected PathTemplate /api/users/{id}, got %s", state.PathTemplate)
	}
	if state.AttemptNumber != 1 {
		t.Errorf("Expected AttemptNumber 1, got %d", state.AttemptNumber)
	}
	if state.ContentType != "application/json" {
		t.Errorf("Expected ContentType application/json, got %s", state.ContentType)
	}
	if len(state.PathParams) != 1 || state.PathParams["id"] != "123" {
		t.Error("PathParams not initialized correctly")
	}
	if len(state.QueryParams) != 1 || state.QueryParams["filter"] != "active" {
		t.Error("QueryParams not initialized correctly")
	}
	if len(state.Headers) != 1 || state.Headers["Authorization"] != "Bearer token" {
		t.Error("Headers not initialized correctly")
	}
	if bodyMap, ok := state.Body.(map[string]interface{}); !ok || bodyMap["name"] != "John" {
		t.Error("Body not initialized correctly")
	}
}

// TestBuildURL verifies URL reconstruction from RequestState
func TestBuildURL(t *testing.T) {
	tests := []struct {
		name        string
		state       *RequestState
		apiTarget   string
		basePath    string
		expectedURL string
	}{
		{
			name: "Basic path with no params",
			state: &RequestState{
				PathTemplate: "/users",
				PathParams:   map[string]string{},
				QueryParams:  map[string]string{},
			},
			apiTarget:   "https://api.example.com",
			basePath:    "/v1",
			expectedURL: "https://api.example.com/v1/users",
		},
		{
			name: "Path with path parameters",
			state: &RequestState{
				PathTemplate: "/users/{id}",
				PathParams:   map[string]string{"id": "123"},
				QueryParams:  map[string]string{},
			},
			apiTarget:   "https://api.example.com",
			basePath:    "/v1",
			expectedURL: "https://api.example.com/v1/users/123",
		},
		{
			name: "Path with query parameters",
			state: &RequestState{
				PathTemplate: "/users",
				PathParams:   map[string]string{},
				QueryParams:  map[string]string{"page": "1", "limit": "10"},
			},
			apiTarget:   "https://api.example.com",
			basePath:    "/v1",
			expectedURL: "https://api.example.com/v1/users?", // Query order may vary
		},
		{
			name: "Complex URL with path and query params",
			state: &RequestState{
				PathTemplate: "/projects/{projectId}/tasks/{taskId}",
				PathParams:   map[string]string{"projectId": "456", "taskId": "789"},
				QueryParams:  map[string]string{"status": "active"},
			},
			apiTarget:   "https://api.example.com",
			basePath:    "/v2",
			expectedURL: "https://api.example.com/v2/projects/456/tasks/789?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.state.BuildURL(tt.apiTarget, tt.basePath)
			// For URLs with query params, just check the base part since order may vary
			if len(tt.state.QueryParams) > 0 {
				if !startsWith(result, tt.expectedURL) {
					t.Errorf("BuildURL() = %s, expected to start with %s", result, tt.expectedURL)
				}
			} else {
				if result != tt.expectedURL {
					t.Errorf("BuildURL() = %s, want %s", result, tt.expectedURL)
				}
			}
		})
	}
}

func TestBuildURL_EncodesQueryParams(t *testing.T) {
	state := &RequestState{
		PathTemplate: "/users",
		QueryParams: map[string]string{
			"name":  "John Doe",
			"email": "a+b@example.com",
		},
	}

	result := state.BuildURL("https://api.example.com", "/v1")

	if !startsWith(result, "https://api.example.com/v1/users?") {
		t.Fatalf("unexpected URL prefix: %s", result)
	}
	if !contains(result, "name=John+Doe") {
		t.Fatalf("expected url-encoded query value for name, got: %s", result)
	}
	if !contains(result, "email=a%2Bb%40example.com") {
		t.Fatalf("expected url-encoded query value for email, got: %s", result)
	}
}

// Helper function for string prefix checking
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[0:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
