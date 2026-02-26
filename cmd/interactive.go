package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// RequestState tracks the current state of a request for interactive modification
type RequestState struct {
	AttemptNumber int
	Method        string
	PathTemplate  string
	PathParams    map[string]string
	QueryParams   map[string]string
	Headers       map[string]string
	Body          interface{}
	ContentType   string
}

// NewRequestState creates a new RequestState from request components
func NewRequestState(method, pathTemplate string, pathParams, queryParams, headers map[string]string, body interface{}, contentType string) *RequestState {
	return &RequestState{
		AttemptNumber: 1,
		Method:        method,
		PathTemplate:  pathTemplate,
		PathParams:    pathParams,
		QueryParams:   queryParams,
		Headers:       headers,
		Body:          body,
		ContentType:   contentType,
	}
}

// BuildURL reconstructs the full URL from the request state
func (rs *RequestState) BuildURL(apiTarget, basePath string) string {
	path := rs.PathTemplate
	// Substitute path parameters
	for key, value := range rs.PathParams {
		path = strings.ReplaceAll(path, "{"+key+"}", value)
	}

	requestURL := apiTarget + basePath + path

	// Add query parameters
	if len(rs.QueryParams) > 0 {
		queryValues := url.Values{}
		for key, value := range rs.QueryParams {
			queryValues.Set(key, value)
		}
		requestURL += "?" + queryValues.Encode()
	}

	return requestURL
}

// FormatStructuredRequest outputs the request in a structured, LLM-parseable format
func (rs *RequestState) FormatStructuredRequest(apiTarget, basePath string, maxRetries int) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("\n=== REQUEST (Attempt %d/%d) ===\n", rs.AttemptNumber, maxRetries))
	output.WriteString(fmt.Sprintf("Method: %s\n", rs.Method))
	output.WriteString(fmt.Sprintf("URL: %s\n", rs.BuildURL(apiTarget, basePath)))

	if len(rs.PathParams) > 0 {
		pathParts := []string{}
		for key, value := range rs.PathParams {
			pathParts = append(pathParts, fmt.Sprintf("%s=%s", key, value))
		}
		output.WriteString(fmt.Sprintf("Path: %s\n", strings.Join(pathParts, ", ")))
	}

	if len(rs.QueryParams) > 0 {
		queryParts := []string{}
		for key, value := range rs.QueryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", key, value))
		}
		output.WriteString(fmt.Sprintf("Query: %s\n", strings.Join(queryParts, ", ")))
	}

	if len(rs.Headers) > 0 {
		headerParts := []string{}
		for key, value := range rs.Headers {
			// Redact sensitive headers
			displayValue := value
			if strings.ToLower(key) == "authorization" && len(value) > 20 {
				displayValue = value[:15] + "..."
			}
			headerParts = append(headerParts, fmt.Sprintf("%s=%s", key, displayValue))
		}
		output.WriteString(fmt.Sprintf("Headers: %s\n", strings.Join(headerParts, ", ")))
	}

	if rs.Body != nil {
		bodyJSON, err := json.MarshalIndent(rs.Body, "", "  ")
		if err == nil {
			output.WriteString(fmt.Sprintf("Body: %s\n", string(bodyJSON)))
		} else {
			output.WriteString(fmt.Sprintf("Body: %v\n", rs.Body))
		}
	}

	return output.String()
}

// FormatStructuredResponse outputs the response in a structured, LLM-parseable format
func FormatStructuredResponse(statusCode int, body string) string {
	statusText := "Unknown"
	switch {
	case statusCode >= 200 && statusCode < 300:
		statusText = "OK"
	case statusCode >= 300 && statusCode < 400:
		statusText = "Redirect"
	case statusCode == 400:
		statusText = "Bad Request"
	case statusCode == 401:
		statusText = "Unauthorized"
	case statusCode == 403:
		statusText = "Forbidden"
	case statusCode == 404:
		statusText = "Not Found"
	case statusCode == 422:
		statusText = "Unprocessable Entity"
	case statusCode >= 500 && statusCode < 600:
		statusText = "Server Error"
	}

	var output strings.Builder
	output.WriteString("\n=== RESPONSE ===\n")
	output.WriteString(fmt.Sprintf("Status: %d %s\n", statusCode, statusText))

	// Try to pretty-print JSON
	var jsonBody interface{}
	if err := json.Unmarshal([]byte(body), &jsonBody); err == nil {
		prettyBody, err := json.MarshalIndent(jsonBody, "", "  ")
		if err == nil {
			output.WriteString(fmt.Sprintf("Body: %s\n", string(prettyBody)))
			return output.String()
		}
	}

	// Not JSON or failed to parse, show raw (truncated if too long)
	if len(body) > 500 {
		output.WriteString(fmt.Sprintf("Body: %s...\n[truncated, %d chars total]\n", body[:500], len(body)))
	} else {
		output.WriteString(fmt.Sprintf("Body: %s\n", body))
	}

	return output.String()
}

// ParseUserInput parses user input and applies modifications to the request state
// Returns: modified (bool), nextEndpoint (bool), quit (bool)
func ParseUserInput(input string, state *RequestState) (bool, bool, bool) {
	input = strings.TrimSpace(input)
	inputLower := strings.ToLower(input)

	// Check for control commands
	if inputLower == "n" || inputLower == "next" {
		return false, true, false
	}
	if inputLower == "q" || inputLower == "quit" {
		return false, false, true
	}

	// Parse modifications
	modified := false

	// Check for explicit prefixes
	if strings.HasPrefix(inputLower, "method:") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) == 2 {
			state.Method = strings.ToUpper(strings.TrimSpace(parts[1]))
			modified = true
		}
	} else if strings.HasPrefix(inputLower, "body:") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) == 2 {
			bodyStr := strings.TrimSpace(parts[1])
			if err := json.Unmarshal([]byte(bodyStr), &state.Body); err != nil {
				fmt.Printf("Error parsing JSON body: %v\n", err)
			} else {
				modified = true
			}
		}
	} else if strings.HasPrefix(inputLower, "path:") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) == 2 {
			kvPart := strings.TrimSpace(parts[1])
			if err := applyKeyValue(kvPart, "path", state); err == nil {
				modified = true
			}
		}
	} else if strings.HasPrefix(inputLower, "query:") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) == 2 {
			kvPart := strings.TrimSpace(parts[1])
			if err := applyKeyValue(kvPart, "query", state); err == nil {
				modified = true
			}
		}
	} else if strings.HasPrefix(inputLower, "header:") || strings.HasPrefix(inputLower, "headers:") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) >= 2 {
			// Skip the first part (Header or Headers)
			remaining := strings.TrimSpace(parts[1])
			// Now find the header name and value
			if strings.Contains(remaining, "=") {
				if err := applyKeyValue(remaining, "header", state); err == nil {
					modified = true
				}
			}
		}
	} else if strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[") {
		// Assume raw JSON body
		if err := json.Unmarshal([]byte(input), &state.Body); err != nil {
			fmt.Printf("Error parsing JSON: %v\n", err)
		} else {
			modified = true
		}
	} else if strings.Contains(input, "=") {
		// Key-value pair, determine target automatically
		if err := applyKeyValue(input, "", state); err != nil {
			fmt.Printf("Error applying modification: %v\n", err)
		} else {
			modified = true
		}
	}

	return modified, false, false
}

// applyKeyValue parses and applies a key=value modification
func applyKeyValue(input, suggestedTarget string, state *RequestState) error {
	parts := strings.SplitN(input, "=", 2)
	if len(parts) < 1 {
		return fmt.Errorf("invalid key=value format")
	}

	key := strings.TrimSpace(parts[0])
	value := ""
	if len(parts) == 2 {
		value = strings.TrimSpace(parts[1])
	}

	// Determine target if not specified
	target := suggestedTarget
	if target == "" {
		target = DetermineFieldTarget(key, state)
	}

	// Apply modification
	return ApplyModification(target, key, value, state)
}

// DetermineFieldTarget intelligently determines whether a field is a path, query, or body parameter
func DetermineFieldTarget(key string, state *RequestState) string {
	// Check path params first
	if _, exists := state.PathParams[key]; exists {
		return "path"
	}
	// Check query params
	if _, exists := state.QueryParams[key]; exists {
		return "query"
	}
	// Check if it's in the body (if body is a map)
	if bodyMap, ok := state.Body.(map[string]interface{}); ok {
		if _, exists := bodyMap[key]; exists {
			return "body"
		}
	}
	// Default to query for new fields (safest option)
	return "query"
}

// ApplyModification applies a modification to the request state
func ApplyModification(target, key, value string, state *RequestState) error {
	switch target {
	case "path":
		if value == "" {
			delete(state.PathParams, key)
		} else {
			if state.PathParams == nil {
				state.PathParams = make(map[string]string)
			}
			state.PathParams[key] = value
		}
	case "query":
		if value == "" {
			delete(state.QueryParams, key)
		} else {
			if state.QueryParams == nil {
				state.QueryParams = make(map[string]string)
			}
			state.QueryParams[key] = value
		}
	case "header":
		if value == "" {
			// Case-insensitive header deletion
			for hKey := range state.Headers {
				if strings.EqualFold(hKey, key) {
					delete(state.Headers, hKey)
					// Clear ContentType field if Content-Type header is deleted
					if strings.EqualFold(hKey, "Content-Type") {
						state.ContentType = ""
					}
					break
				}
			}
		} else {
			if state.Headers == nil {
				state.Headers = make(map[string]string)
			}
			// Case-insensitive header update - remove old key if exists
			for hKey := range state.Headers {
				if strings.EqualFold(hKey, key) {
					if hKey != key {
						delete(state.Headers, hKey)
					}
					break
				}
			}
			state.Headers[key] = value
			// Update ContentType field if Content-Type header is modified
			if strings.EqualFold(key, "Content-Type") {
				state.ContentType = value
			}
		}
	case "body":
		// Handle nested keys with dot notation
		if bodyMap, ok := state.Body.(map[string]interface{}); ok {
			setNestedField(bodyMap, key, value)
		} else {
			// Body is not a map, can't set fields
			return fmt.Errorf("body is not a JSON object, cannot set field %s", key)
		}
	default:
		return fmt.Errorf("unknown target: %s", target)
	}
	return nil
}

// setNestedField sets a field in a nested map using dot notation
func setNestedField(m map[string]interface{}, path, value string) {
	parts := strings.Split(path, ".")
	current := m

	// Navigate to the parent of the target field
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if _, exists := current[part]; !exists {
			current[part] = make(map[string]interface{})
		}
		if nextMap, ok := current[part].(map[string]interface{}); ok {
			current = nextMap
		} else {
			// Can't navigate further, create new map
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	// Set the final field
	finalKey := parts[len(parts)-1]
	if value == "" {
		delete(current, finalKey)
	} else {
		// Try to parse as JSON, number, bool, or use as string
		parsedValue := parseValue(value)
		current[finalKey] = parsedValue
	}
}

// parseValue attempts to parse a string value into the appropriate type
func parseValue(value string) interface{} {
	// Try JSON
	var jsonVal interface{}
	if err := json.Unmarshal([]byte(value), &jsonVal); err == nil {
		return jsonVal
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal
	}

	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}

	// Try boolean
	if boolVal, err := strconv.ParseBool(value); err == nil {
		return boolVal
	}

	// Default to string
	return value
}

// ValidateMutationAgainstSpec validates the request state against the OpenAPI spec
// Returns a list of warnings (empty if no violations)
func ValidateMutationAgainstSpec(state *RequestState, operation map[string]interface{}, spec map[string]interface{}) []string {
	warnings := []string{}

	// Get parameters from operation
	var parameters []interface{}
	if params, ok := operation["parameters"].([]interface{}); ok {
		parameters = params
	}

	// Check path parameters
	for key := range state.PathParams {
		if !isParameterDefined(key, "path", parameters, spec) {
			warnings = append(warnings, fmt.Sprintf("WARNING: Path parameter '%s' not defined in spec", key))
		}
	}

	// Check query parameters
	for key := range state.QueryParams {
		if !isParameterDefined(key, "query", parameters, spec) {
			warnings = append(warnings, fmt.Sprintf("WARNING: Query parameter '%s' not defined in spec", key))
		}
	}

	// Check headers
	for key := range state.Headers {
		// Skip standard headers
		if isStandardHeader(key) {
			continue
		}
		if !isParameterDefined(key, "header", parameters, spec) {
			warnings = append(warnings, fmt.Sprintf("WARNING: Header '%s' not defined in spec", key))
		}
	}

	return warnings
}

// isParameterDefined checks if a parameter is defined in the spec
func isParameterDefined(name, location string, parameters []interface{}, spec map[string]interface{}) bool {
	for _, param := range parameters {
		var paramMap map[string]interface{}

		// Handle $ref
		if paramRef, ok := param.(map[string]interface{}); ok {
			if ref, hasRef := paramRef["$ref"].(string); hasRef {
				resolved := ResolveRef(spec, ref)
				paramMap = resolved
			} else {
				paramMap = paramRef
			}
		}

		if paramMap == nil {
			continue
		}

		paramName, _ := paramMap["name"].(string)
		paramIn, _ := paramMap["in"].(string)

		if paramName == name && paramIn == location {
			return true
		}
	}
	return false
}

// isStandardHeader checks if a header is a standard HTTP header
func isStandardHeader(name string) bool {
	standardHeaders := []string{
		"Accept", "Content-Type", "User-Agent", "Authorization",
		"Accept-Encoding", "Accept-Language", "Cache-Control",
		"Connection", "Cookie", "Host", "Referer",
	}
	for _, h := range standardHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}

// ShowSpecWarnings displays spec validation warnings and prompts for confirmation
// Returns true if user wants to continue, false otherwise
func ShowSpecWarnings(warnings []string) bool {
	if len(warnings) == 0 {
		return true
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	for _, warning := range warnings {
		fmt.Println(warning)
	}
	fmt.Println(strings.Repeat("=", 50))
	fmt.Print("Continue outside spec? [Y/n]: ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	return response == "" || response == "y" || response == "yes"
}

// InteractiveModifyLoop handles the interactive modification loop for a single endpoint
// Returns: shouldResend (bool), shouldQuit (bool)
func InteractiveModifyLoop(state *RequestState, responseStatus int, responseBody string, maxRetries int, spec map[string]interface{}, operation map[string]interface{}, apiTarget, basePath string) (bool, bool) {
	// Iterative loop to avoid stack overflow from recursion
	for {
		// Check if we've hit max retries (allow maxRetries attempts, so stop when > maxRetries)
		if state.AttemptNumber > maxRetries {
			fmt.Printf("\n=== MAX RETRIES REACHED (%d/%d) ===\n", maxRetries, maxRetries)
			fmt.Println("Auto-advancing to next endpoint.")
			return false, false
		}

		// Display the request and response
		fmt.Print(state.FormatStructuredRequest(apiTarget, basePath, maxRetries))
		fmt.Print(FormatStructuredResponse(responseStatus, responseBody))

		// Prompt for modification
		fmt.Print("\n[Modify request or N for next]: ")

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			log.Errorf("Error reading input: %v", err)
			return false, false
		}

		// Parse input
		modified, nextEndpoint, quit := ParseUserInput(input, state)

		if quit {
			return false, true
		}

		if nextEndpoint {
			return false, false
		}

		if modified {
			// Validate against spec
			warnings := ValidateMutationAgainstSpec(state, operation, spec)
			if len(warnings) > 0 {
				if !ShowSpecWarnings(warnings) {
					// User chose not to continue, stay in loop for another attempt
					continue
				}
			}

			// Increment attempt number and resend
			state.AttemptNumber++
			return true, false
		}

		// No modification detected, stay in loop
		fmt.Println("No modification detected. Enter a modification or 'N' to continue.")
		// Continue the loop to prompt again
	}
}
