package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	// DedupeURLAndHash deduplicates discovered specs by URL and content hash.
	DedupeURLAndHash = "url_and_hash"
)

// DiscoveryOptions controls discovery workflow behavior.
type DiscoveryOptions struct {
	Continue     bool
	MaxFound     int
	ShowProgress bool
	DedupeMode   string
	OnDiscovered func(DiscoveredSpec)
}

// DiscoveredSpec describes a discovered API specification.
type DiscoveredSpec struct {
	URL         string      `json:"url"`
	Phase       string      `json:"phase"`
	SpecBytes   []byte      `json:"-"`
	ContentHash string      `json:"content_hash"`
	Spec        *openapi3.T `json:"-"`
}

type discoveryCollector struct {
	options    DiscoveryOptions
	seenURLs   map[string]struct{}
	seenHashes map[string]struct{}
	results    []DiscoveredSpec
}

func newDiscoveryCollector(options DiscoveryOptions) *discoveryCollector {
	if options.DedupeMode == "" {
		options.DedupeMode = DedupeURLAndHash
	}
	if options.MaxFound < 0 {
		options.MaxFound = 0
	}
	return &discoveryCollector{
		options:    options,
		seenURLs:   make(map[string]struct{}),
		seenHashes: make(map[string]struct{}),
		results:    []DiscoveredSpec{},
	}
}

func (c *discoveryCollector) shouldStop() bool {
	if len(c.results) == 0 {
		return false
	}
	if !c.options.Continue {
		return true
	}
	if c.options.MaxFound > 0 && len(c.results) >= c.options.MaxFound {
		return true
	}
	return false
}

func (c *discoveryCollector) add(urlStr, phase string, spec *openapi3.T) bool {
	if spec == nil {
		return false
	}

	canonicalURL := strings.TrimSpace(urlStr)
	if canonicalURL == "" {
		return false
	}
	if _, exists := c.seenURLs[canonicalURL]; exists {
		return false
	}

	specBytes, err := json.Marshal(spec)
	if err != nil {
		return false
	}
	hashBytes := sha256.Sum256(specBytes)
	hash := hex.EncodeToString(hashBytes[:])

	if c.options.DedupeMode == DedupeURLAndHash {
		if _, exists := c.seenHashes[hash]; exists {
			return false
		}
		c.seenHashes[hash] = struct{}{}
	}

	c.seenURLs[canonicalURL] = struct{}{}

	d := DiscoveredSpec{
		URL:         canonicalURL,
		Phase:       phase,
		SpecBytes:   specBytes,
		ContentHash: hash,
		Spec:        spec,
	}
	c.results = append(c.results, d)
	if c.options.OnDiscovered != nil {
		c.options.OnDiscovered(d)
	}
	return true
}

// NormalizeTargetInput normalizes a user-provided target (host or URL).
// If the scheme is omitted, https:// is assumed.
func NormalizeTargetInput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("target is empty")
	}
	if strings.HasPrefix(trimmed, "#") {
		return "", fmt.Errorf("target is a comment")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid target '%s': %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid target '%s': missing scheme or host", raw)
	}
	return parsed.String(), nil
}

func schemeHostOnly(input string) (string, error) {
	parsed, err := url.Parse(input)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid input '%s'", input)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// ParseURLFile reads newline-delimited targets from disk.
// Blank lines and comment lines (#...) are skipped.
func ParseURLFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var targets []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalized, err := NormalizeTargetInput(line)
		if err != nil {
			// Keep behavior non-fatal for bulk mode: invalid lines are skipped.
			continue
		}
		targets = append(targets, normalized)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}

// ValidateSpecResponse validates fetched bytes as an OpenAPI/Swagger spec with paths.
func ValidateSpecResponse(bodyBytes []byte, statusCode int) (*openapi3.T, error) {
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", statusCode)
	}
	if len(bodyBytes) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	if !IsSwaggerSpec(bodyBytes) {
		return nil, fmt.Errorf("response is not OpenAPI/Swagger content")
	}
	spec := UnmarshalSpecBytes(bodyBytes)
	if spec == nil || spec.Paths == nil {
		return nil, fmt.Errorf("parsed spec is invalid or has no paths")
	}
	return spec, nil
}

// TryDirectSpecAny attempts to fetch and parse the target URL directly as an API spec.
func TryDirectSpecAny(targetURL string, client http.Client) (*openapi3.T, error) {
	bodyBytes, _, statusCode := MakeRequest(client, http.MethodGet, targetURL, timeout, nil)
	return ValidateSpecResponse(bodyBytes, statusCode)
}

func buildBruteCandidateURLs(target, wordlistPath string) ([]string, error) {
	if strings.TrimSpace(wordlistPath) == "" {
		urls := []string{}
		urls = append(urls, makeURLs(target, priorityURLs, "", true)...)
		urls = append(urls, makeURLs(target, jsonEndpoints, "", false)...)
		urls = append(urls, makeURLs(target, javascriptEndpoints, ".js", false)...)
		urls = append(urls, makeURLs(target, jsonEndpoints, ".json", false)...)
		urls = append(urls, makeURLs(target, jsonEndpoints, "/", false)...)
		return urls, nil
	}

	file, err := os.Open(wordlistPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		endpoint := strings.TrimSpace(scanner.Text())
		if endpoint == "" || strings.HasPrefix(endpoint, "#") {
			continue
		}
		urls = append(urls, target+endpoint)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

func findSpecAtCandidateURL(candidateURL string, client http.Client) (string, *openapi3.T) {
	ct := strings.ToLower(CheckContentType(client, candidateURL))

	if strings.Contains(ct, "application/json") || strings.Contains(ct, "text/json") || strings.Contains(ct, "application/yaml") || strings.Contains(ct, "application/x-yaml") || strings.Contains(ct, "text/yaml") || strings.Contains(ct, "text/yml") {
		if spec, err := FetchAndValidateSpec(candidateURL, client); err == nil {
			return candidateURL, spec
		}
		return "", nil
	}

	if strings.Contains(ct, "application/javascript") || strings.Contains(ct, "text/javascript") {
		bodyBytes, bodyString, statusCode := MakeRequest(client, http.MethodGet, candidateURL, timeout, nil)
		if bodyBytes == nil || statusCode != http.StatusOK {
			return "", nil
		}

		if swashPath := ExtractSwashbuckleConfig(bodyString); swashPath != "" {
			if fullURL, err := ResolveRelativeURL(candidateURL, swashPath); err == nil {
				if spec, err := FetchAndValidateSpec(fullURL, client); err == nil {
					return fullURL, spec
				}
			}
		}

		if specURL := ExtractSpecURLFromJS(bodyString); specURL != "" {
			if fullURL, err := ResolveRelativeURL(candidateURL, specURL); err == nil {
				if spec, err := FetchAndValidateSpec(fullURL, client); err == nil {
					return fullURL, spec
				}
			}
		}

		if embeddedSpec, err := ExtractEmbeddedSpecFromJS(bodyString); err == nil && embeddedSpec != nil && embeddedSpec.Paths != nil {
			return candidateURL, embeddedSpec
		}
	}

	// Final fallback: try parsing as a direct spec even with unknown content-type.
	if spec, err := FetchAndValidateSpec(candidateURL, client); err == nil {
		return candidateURL, spec
	}

	return "", nil
}

// DiscoverSpecs executes multi-phase spec discovery with dedupe and stop controls.
func DiscoverSpecs(seedTarget string, client http.Client, wordlistPath string, options DiscoveryOptions) ([]DiscoveredSpec, error) {
	normalizedSeed, err := NormalizeTargetInput(seedTarget)
	if err != nil {
		return nil, err
	}

	baseTarget, err := schemeHostOnly(normalizedSeed)
	if err != nil {
		return nil, err
	}

	collector := newDiscoveryCollector(options)

	if spec, err := TryDirectSpecAny(normalizedSeed, client); err == nil {
		collector.add(normalizedSeed, "direct", spec)
		if collector.shouldStop() {
			return collector.results, nil
		}
	}

	if discoveredURL, spec, err := TrySwaggerUIDiscoveryDetailed(baseTarget, client); err == nil && spec != nil {
		collector.add(discoveredURL, "swagger_ui", spec)
		if collector.shouldStop() {
			return collector.results, nil
		}
	}

	candidateURLs, err := buildBruteCandidateURLs(baseTarget, wordlistPath)
	if err != nil {
		return collector.results, err
	}

	for i, candidateURL := range candidateURLs {
		if foundURL, spec := findSpecAtCandidateURL(candidateURL, client); spec != nil {
			collector.add(foundURL, "brute", spec)
			if collector.shouldStop() {
				break
			}
		}

		if options.ShowProgress {
			if i == len(candidateURLs)-1 {
				fmt.Printf("\033[2K\r%s%d\n", "Request: ", i+1)
			} else {
				fmt.Printf("\033[2K\r%s%d", "Request: ", i+1)
			}
		}
	}

	if len(collector.results) == 0 {
		return nil, fmt.Errorf("no definition file found for %s", seedTarget)
	}

	return collector.results, nil
}
