package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ============================================
// Compiled Regexes (package-level for performance)
// ============================================

var (
	// ExtractSpecURLFromJS patterns
	reURLDirect     = regexp.MustCompile(`url:\s*["']([^"']+)["']`)
	reURLsArray     = regexp.MustCompile(`urls:\s*\[\s*{\s*url:\s*["']([^"']+)["']`)
	reConstSpecFile = regexp.MustCompile(`const\s+\w+\s*=\s*["']([^"']+\.(?:json|yaml|yml))["']`)
	reDefaultDefURL = regexp.MustCompile(`defaultDefinitionUrl\s*=\s*["']([^"']+)["']`)
	reDefinitionURL = regexp.MustCompile(`definitionURL\s*=\s*["']([^"']+)["']`)

	// ExtractSwashbuckleConfig patterns
	reSwashbuckleConfig = regexp.MustCompile(`window\.swashbuckleConfig\s*=\s*{([\s\S]*?)};`)
	reDiscoveryPaths    = regexp.MustCompile(`discoveryPaths\s*:\s*\[\s*["']([^"']+)["']`)

	// JSObjectToJSON patterns
	reUnquotedKeys  = regexp.MustCompile(`([{,]\s*)(\w+)\s*:`)
	reTrailingComma = regexp.MustCompile(`,\s*([}\]])`)

	// ExtractEmbeddedSpecFromJS patterns
	reVarAssignment    = regexp.MustCompile(`(?:var|let|const)\s+(\w+)\s*=\s*({[\s\S]*?});`)
	reSimpleAssignment = regexp.MustCompile(`(\w+)\s*=\s*({[\s\S]*?});`)

	// extractSpecURLFromHTML patterns
	reSwaggerUIBundle = regexp.MustCompile(`SwaggerUIBundle\s*\(\s*{\s*url:\s*["']([^"']+)["']`)

	// extractSpecURLWithRegex patterns
	reURLWithExtension       = regexp.MustCompile(`url:\s*["']([^"']+\.(?:json|yaml|yml))["']`)
	reSwaggerUIBundleWithExt = regexp.MustCompile(`SwaggerUIBundle\s*\(\s*{\s*url:\s*["']([^"']+)["']`)
	reSpecURL                = regexp.MustCompile(`spec(?:Url)?:\s*["']([^"']+\.(?:json|yaml|yml))["']`)
	reConfigURL              = regexp.MustCompile(`configUrl:\s*["']([^"']+\.(?:json|yaml|yml))["']`)
)

// ============================================
// JavaScript Spec Extraction Functions
// ============================================

// ExtractSpecURLFromJS searches JavaScript content for OpenAPI/Swagger spec URLs
// using multiple regex patterns. Returns the first matching URL found, or empty string.
func ExtractSpecURLFromJS(jsBody string) string {
	patterns := []*regexp.Regexp{
		reURLDirect,
		reURLsArray,
		reConstSpecFile,
		reDefaultDefURL,
		reDefinitionURL,
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(jsBody)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// ExtractSwashbuckleConfig parses window.swashbuckleConfig from HTML/JS content
// to extract the discoveryPaths array. Returns the first discovery path found.
// Swashbuckle is commonly used in ASP.NET Core applications.
func ExtractSwashbuckleConfig(content string) string {
	// Pattern: window.swashbuckleConfig = { ... discoveryPaths: ["/swagger/v1/swagger.json"] ... };
	matches := reSwashbuckleConfig.FindStringSubmatch(content)

	if len(matches) > 1 {
		configContent := matches[1]
		pathMatches := reDiscoveryPaths.FindStringSubmatch(configContent)

		if len(pathMatches) > 1 {
			return pathMatches[1]
		}
	}

	return ""
}

// JSObjectToJSON converts a JavaScript object literal to valid JSON format
// by replacing single quotes with double quotes, adding quotes to unquoted keys,
// and removing trailing commas before closing braces/brackets.
func JSObjectToJSON(jsObject string) (string, error) {
	cleaned := strings.TrimSpace(jsObject)

	// Replace single quotes with double quotes
	cleaned = strings.ReplaceAll(cleaned, "'", "\"")

	// Add quotes to unquoted keys: {key: -> {"key":
	// This regex finds word characters followed by colon after { or ,
	cleaned = reUnquotedKeys.ReplaceAllString(cleaned, `$1"$2":`)

	// Remove trailing commas before } or ]
	cleaned = reTrailingComma.ReplaceAllString(cleaned, "$1")

	return cleaned, nil
}

// StripJSComments removes JavaScript comments while preserving quoted strings.
// This allows object extraction/parsing without breaking values like "https://...".
func StripJSComments(js string) string {
	var b strings.Builder
	b.Grow(len(js))

	inLineComment := false
	inBlockComment := false
	inSingleQuote := false
	inDoubleQuote := false
	inTemplateQuote := false
	escaped := false

	for i := 0; i < len(js); i++ {
		c := js[i]

		if inLineComment {
			if c == '\n' || c == '\r' {
				inLineComment = false
				b.WriteByte(c)
			}
			continue
		}

		if inBlockComment {
			if c == '*' && i+1 < len(js) && js[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inSingleQuote {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '\'' {
				inSingleQuote = false
			}
			continue
		}

		if inDoubleQuote {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inDoubleQuote = false
			}
			continue
		}

		if inTemplateQuote {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '`' {
				inTemplateQuote = false
			}
			continue
		}

		if c == '/' && i+1 < len(js) {
			next := js[i+1]
			if next == '/' {
				inLineComment = true
				i++
				continue
			}
			if next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		switch c {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '`':
			inTemplateQuote = true
		}

		b.WriteByte(c)
	}

	return b.String()
}

// ExtractEmbeddedSpecFromJS attempts to extract an embedded OpenAPI/Swagger spec
// from JavaScript code by finding variable assignments with object literals.
// Parses JS objects and converts them to valid JSON for spec extraction.
func ExtractEmbeddedSpecFromJS(jsBody string) (*openapi3.T, error) {
	cleaned := StripJSComments(jsBody)

	// Patterns to find variable assignments with objects
	patterns := []*regexp.Regexp{
		reVarAssignment,
		reSimpleAssignment,
	}

	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(cleaned, -1)

		for _, match := range matches {
			if len(match) < 3 {
				continue
			}

			objStr := match[2]

			// Convert JS object to JSON
			jsonStr, err := JSObjectToJSON(objStr)
			if err != nil {
				continue
			}

			// Try to parse as JSON
			var testMap map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &testMap); err != nil {
				continue
			}

			// Check if it looks like an OpenAPI/Swagger spec
			if _, hasOpenAPI := testMap["openapi"]; hasOpenAPI {
				// Try to unmarshal as OpenAPI 3.x spec
				var spec openapi3.T
				if err := json.Unmarshal([]byte(jsonStr), &spec); err == nil {
					return &spec, nil
				}
			}

			if _, hasSwagger := testMap["swagger"]; hasSwagger {
				// It's a Swagger 2.0 spec, return it for conversion
				// The calling code can handle v2->v3 conversion using UnmarshalSpec
				var spec openapi3.T
				if err := json.Unmarshal([]byte(jsonStr), &spec); err == nil {
					return &spec, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no embedded spec found in JavaScript")
}

// ============================================
// URL Resolution Helpers
// ============================================

// ResolveRelativeURL resolves a potentially relative URL against a base URL.
// Handles absolute URLs (returns as-is), protocol-relative (//), and path-relative URLs.
func ResolveRelativeURL(baseURL, relativeURL string) (string, error) {
	// If it's already an absolute URL, return it
	if strings.HasPrefix(relativeURL, "http://") || strings.HasPrefix(relativeURL, "https://") {
		return relativeURL, nil
	}

	// Parse the base URL
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	// Parse the relative URL
	rel, err := url.Parse(relativeURL)
	if err != nil {
		return "", fmt.Errorf("invalid relative URL: %w", err)
	}

	// Resolve and return the full URL
	resolved := base.ResolveReference(rel)
	return resolved.String(), nil
}

// ============================================
// Spec Validation & Fetching
// ============================================

// IsSwaggerSpec checks if the provided bytes contain a valid OpenAPI/Swagger spec
// by looking for the "openapi" or "swagger" version fields.
// Supports both JSON and YAML formats.
func IsSwaggerSpec(bodyBytes []byte) bool {
	var testMap map[string]interface{}

	// Try JSON first
	if err := json.Unmarshal(bodyBytes, &testMap); err == nil {
		if openapi, ok := testMap["openapi"].(string); ok {
			return strings.HasPrefix(openapi, "2") || strings.HasPrefix(openapi, "3")
		}
		if swagger, ok := testMap["swagger"].(string); ok {
			return strings.HasPrefix(swagger, "2")
		}
	}

	// Try YAML if JSON failed
	if err := yaml.Unmarshal(bodyBytes, &testMap); err == nil {
		if openapi, ok := testMap["openapi"].(string); ok {
			return strings.HasPrefix(openapi, "2") || strings.HasPrefix(openapi, "3")
		}
		if swagger, ok := testMap["swagger"].(string); ok {
			return strings.HasPrefix(swagger, "2")
		}
	}

	return false
}

// FetchAndValidateSpec attempts to fetch a URL and validate it as an OpenAPI/Swagger spec.
// Returns the parsed spec if successful, or an error.
func FetchAndValidateSpec(targetURL string, client http.Client) (*openapi3.T, error) {
	bodyBytes, _, statusCode := MakeRequest(client, "GET", targetURL, timeout, nil)

	if bodyBytes == nil || statusCode != 200 {
		return nil, fmt.Errorf("failed to fetch spec (status: %d)", statusCode)
	}

	// Validate it's a spec
	if !IsSwaggerSpec(bodyBytes) {
		return nil, fmt.Errorf("content is not a valid OpenAPI/Swagger spec")
	}

	// Use UnmarshalSpecBytes to handle both v2 and v3, YAML, JSON, etc.
	spec := UnmarshalSpecBytes(bodyBytes)

	if spec == nil || spec.Paths == nil {
		return nil, fmt.Errorf("parsed spec is invalid or has no paths")
	}

	return spec, nil
}

// UnmarshalSpecBytes parses OpenAPI/Swagger spec bytes without relying on global variables.
// Auto-detects JSON vs YAML format and converts Swagger 2.0 to OpenAPI 3.0.
// This function is used by the discovery pipeline to avoid global state dependencies.
func UnmarshalSpecBytes(bodyBytes []byte) *openapi3.T {
	var doc2 openapi2.T
	var doc3 openapi3.T

	// Try JSON first (most common)
	if err := json.Unmarshal(bodyBytes, &doc3); err == nil {
		if strings.HasPrefix(doc3.OpenAPI, "3") {
			return &doc3
		}
	}

	if err := json.Unmarshal(bodyBytes, &doc2); err == nil {
		if strings.HasPrefix(doc2.Swagger, "2") {
			newDoc, err := openapi2conv.ToV3(&doc2)
			if err == nil {
				return newDoc
			}
		}
	}

	// Try YAML if JSON failed
	if err := yaml.Unmarshal(bodyBytes, &doc3); err == nil {
		if strings.HasPrefix(doc3.OpenAPI, "3") {
			return &doc3
		}
	}

	if err := yaml.Unmarshal(bodyBytes, &doc2); err == nil {
		if strings.HasPrefix(doc2.Swagger, "2") {
			newDoc, err := openapi2conv.ToV3(&doc2)
			if err == nil {
				return newDoc
			}
		}
	}

	// Return empty spec if parsing failed
	return &openapi3.T{}
}

// ============================================
// Multi-Phase Discovery Functions
// ============================================

// TryDirectSpec attempts to fetch and validate a URL as a direct spec file.
// This is Phase 1 of discovery - checking if the provided URL is itself a spec.
func TryDirectSpec(targetURL string, client http.Client) (*openapi3.T, error) {
	// Check if URL ends with common spec extensions
	if !strings.HasSuffix(targetURL, ".json") &&
		!strings.HasSuffix(targetURL, ".yaml") &&
		!strings.HasSuffix(targetURL, ".yml") {
		return nil, fmt.Errorf("URL does not appear to be a direct spec file")
	}

	return FetchAndValidateSpec(targetURL, client)
}

// TryJavaScriptExtraction attempts to extract a spec from a JavaScript file
// using both URL extraction and embedded spec extraction methods.
func TryJavaScriptExtraction(targetURL string, client http.Client) (*openapi3.T, error) {
	bodyBytes, bodyString, statusCode := MakeRequest(client, "GET", targetURL, timeout, nil)

	if bodyBytes == nil || statusCode != 200 {
		return nil, fmt.Errorf("failed to fetch JavaScript file (status: %d)", statusCode)
	}

	// First attempt: Try to extract a spec URL from the JS
	specURL := ExtractSpecURLFromJS(bodyString)
	if specURL != "" {
		// Resolve the URL relative to the JavaScript file location
		fullURL, err := ResolveRelativeURL(targetURL, specURL)
		if err == nil {
			// Try to fetch the referenced spec
			spec, err := FetchAndValidateSpec(fullURL, client)
			if err == nil {
				log.Infof("Found spec URL referenced in JavaScript: %s", fullURL)
				return spec, nil
			}
		}
	}

	// Second attempt: Try to extract an embedded spec from the JS
	spec, err := ExtractEmbeddedSpecFromJS(bodyString)
	if err == nil && spec != nil {
		log.Infof("Extracted embedded spec from JavaScript file: %s", targetURL)
		return spec, nil
	}

	return nil, fmt.Errorf("no spec found in JavaScript file")
}

// ============================================
// HTML Parsing & Swagger UI Detection
// ============================================

// Common Swagger UI paths to check for spec discovery
var SwaggerUIPaths = []string{
	"/swagger-ui.html",
	"/swagger-ui/",
	"/swagger/",
	"/swagger/ui/",
	"/swagger/index.html",
	"/api/docs",
	"/api/docs/",
	"/api/swagger-ui.html",
	"/api/swagger/",
	"/api/swagger/ui/",
	"/api-docs",
	"/api-docs/",
	"/docs",
	"/docs/",
	"/apidocs",
	"/apidocs/",
	"/swagger-ui/index.html",
	"/api/swagger-ui/",
	"/api/swagger/index.html",
	"/documentation",
	"/documentation/",
}

// ExtractSpecURLFromHTML parses HTML content to find OpenAPI/Swagger spec URLs.
// It searches for common patterns in script tags, inline JavaScript, and HTML attributes.
func ExtractSpecURLFromHTML(htmlContent string) string {
	// Try to parse the HTML with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		// If goquery fails, fall back to regex patterns
		return extractSpecURLWithRegex(htmlContent)
	}

	// Pattern 1: Look for spec URLs in script tags
	var specURL string
	doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
		scriptContent := s.Text()

		// Check for url: "..." pattern
		if matches := reURLDirect.FindStringSubmatch(scriptContent); len(matches) > 1 {
			specURL = matches[1]
			return false // Break the loop
		}

		// Check for SwaggerUIBundle pattern
		if matches := reSwaggerUIBundle.FindStringSubmatch(scriptContent); len(matches) > 1 {
			specURL = matches[1]
			return false
		}

		// Check for spec URL in variable assignments
		if url := ExtractSpecURLFromJS(scriptContent); url != "" {
			specURL = url
			return false
		}

		return true // Continue
	})

	if specURL != "" {
		return specURL
	}

	// Pattern 2: Check for spec URLs in external script src attributes
	doc.Find("script[src]").EachWithBreak(func(i int, s *goquery.Selection) bool {
		src, exists := s.Attr("src")
		if exists && (strings.Contains(src, "swagger") || strings.Contains(src, "openapi")) {
			// If it's a swagger-related script, we might want to fetch and parse it
			// For now, we'll just check if it's a direct spec file
			if strings.HasSuffix(src, ".json") || strings.HasSuffix(src, ".yaml") || strings.HasSuffix(src, ".yml") {
				specURL = src
				return false
			}
		}
		return true
	})

	if specURL != "" {
		return specURL
	}

	// Pattern 3: Check for data attributes or links
	doc.Find("link[rel='spec'], link[rel='openapi'], link[rel='swagger']").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, exists := s.Attr("href")
		if exists {
			specURL = href
			return false
		}
		return true
	})

	if specURL != "" {
		return specURL
	}

	// Fallback to regex if goquery didn't find anything
	return extractSpecURLWithRegex(htmlContent)
}

// extractSpecURLWithRegex uses regex patterns as a fallback for HTML parsing
func extractSpecURLWithRegex(htmlContent string) string {
	patterns := []*regexp.Regexp{
		reURLWithExtension,
		reSwaggerUIBundleWithExt,
		reSpecURL,
		reConfigURL,
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// TrySwaggerUIDiscoveryDetailed attempts to discover OpenAPI specs by checking common Swagger UI paths.
// It fetches HTML pages, parses them, and attempts to extract spec URLs.
// Returns both the discovered spec URL and the parsed spec.
func TrySwaggerUIDiscoveryDetailed(baseURL string, client http.Client) (string, *openapi3.T, error) {
	for _, path := range SwaggerUIPaths {
		fullURL, err := ResolveRelativeURL(baseURL, path)
		if err != nil {
			continue
		}

		bodyBytes, bodyString, statusCode := MakeRequest(client, "GET", fullURL, timeout, nil)
		if bodyBytes == nil || statusCode != 200 {
			continue
		}

		// Check if the response looks like HTML
		contentLower := strings.ToLower(bodyString)
		if !strings.Contains(contentLower, "<html") && !strings.Contains(contentLower, "swagger") {
			continue
		}

		// First, check for Swashbuckle configuration
		if swashPath := ExtractSwashbuckleConfig(bodyString); swashPath != "" {
			resolvedURL, err := ResolveRelativeURL(fullURL, swashPath)
			if err == nil {
				spec, err := FetchAndValidateSpec(resolvedURL, client)
				if err == nil && spec != nil {
					log.Infof("Found spec via Swashbuckle config at: %s", resolvedURL)
					return resolvedURL, spec, nil
				}
			}
		}

		// Try to extract spec URL from HTML
		specURL := ExtractSpecURLFromHTML(bodyString)
		if specURL != "" {
			resolvedURL, err := ResolveRelativeURL(fullURL, specURL)
			if err == nil {
				spec, err := FetchAndValidateSpec(resolvedURL, client)
				if err == nil && spec != nil {
					log.Infof("Found spec URL in Swagger UI HTML: %s", resolvedURL)
					return resolvedURL, spec, nil
				}
			}
		}

		// Try to extract embedded spec from inline script tags in HTML
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
		if err == nil {
			var foundSpec *openapi3.T

			// Check inline script tags for embedded specs
			doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
				// Skip scripts with src attribute (external files, handled separately)
				if _, hasSrc := s.Attr("src"); hasSrc {
					return true
				}

				scriptContent := s.Text()
				if scriptContent == "" {
					return true
				}

				// Try to extract embedded spec from inline script
				spec, err := ExtractEmbeddedSpecFromJS(scriptContent)
				if err == nil && spec != nil && spec.Paths != nil {
					log.Infof("Found embedded spec in inline HTML script tag at: %s", fullURL)
					foundSpec = spec
					return false // Break
				}

				return true
			})

			if foundSpec != nil {
				return fullURL, foundSpec, nil
			}
		}

		// Check for embedded JavaScript files referenced in the HTML
		doc, err = goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
		if err == nil {
			var foundSpec *openapi3.T
			doc.Find("script[src]").EachWithBreak(func(i int, s *goquery.Selection) bool {
				src, exists := s.Attr("src")
				if !exists {
					return true
				}

				// Skip external CDN scripts
				if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
					parsed, _ := url.Parse(src)
					baseParsed, _ := url.Parse(baseURL)
					if parsed != nil && baseParsed != nil && parsed.Host != baseParsed.Host {
						return true // Skip external scripts
					}
				}

				// If it's a swagger-related script, try to extract from it
				if strings.Contains(src, "swagger") || strings.Contains(src, "init") || strings.Contains(src, "config") {
					jsURL, err := ResolveRelativeURL(fullURL, src)
					if err == nil {
						spec, err := TryJavaScriptExtraction(jsURL, client)
						if err == nil && spec != nil {
							foundSpec = spec
							return false // Break
						}
					}
				}

				return true
			})

			if foundSpec != nil {
				return fullURL, foundSpec, nil
			}
		}
	}

	return "", nil, fmt.Errorf("no spec found via Swagger UI discovery")
}

// TrySwaggerUIDiscovery is a compatibility wrapper for existing callers.
func TrySwaggerUIDiscovery(baseURL string, client http.Client) (*openapi3.T, error) {
	_, spec, err := TrySwaggerUIDiscoveryDetailed(baseURL, client)
	return spec, err
}
