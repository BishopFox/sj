package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestValidateAutomateFlagsConflicts(t *testing.T) {
	savedSwaggerURL := swaggerURL
	savedURLFile := urlFile
	savedLocalFile := localFile
	savedFallback := fallbackBrute
	defer func() {
		swaggerURL = savedSwaggerURL
		urlFile = savedURLFile
		localFile = savedLocalFile
		fallbackBrute = savedFallback
	}()

	swaggerURL = "https://example.com/openapi.json"
	urlFile = "targets.txt"
	localFile = ""
	fallbackBrute = false
	if err := validateAutomateFlags(); err == nil {
		t.Fatal("expected conflict error for --url and --url-file")
	}

	swaggerURL = ""
	urlFile = ""
	localFile = "spec.yaml"
	fallbackBrute = true
	if err := validateAutomateFlags(); err == nil {
		t.Fatal("expected conflict error for --fallback-brute and --local-file")
	}

	swaggerURL = ""
	urlFile = ""
	localFile = ""
	fallbackBrute = false
	if err := validateAutomateFlags(); err == nil {
		t.Fatal("expected missing target error")
	}

	swaggerURL = "https://example.com/openapi.json"
	urlFile = ""
	localFile = ""
	fallbackBrute = false
	if err := validateAutomateFlags(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateBruteFlagsConflicts(t *testing.T) {
	savedSwaggerURL := swaggerURL
	savedRunAutomate := bruteRunAutomate
	savedEndpointOnly := endpointOnly
	savedContinue := bruteContinue
	savedMaxFound := bruteMaxFound
	defer func() {
		swaggerURL = savedSwaggerURL
		bruteRunAutomate = savedRunAutomate
		endpointOnly = savedEndpointOnly
		bruteContinue = savedContinue
		bruteMaxFound = savedMaxFound
	}()

	swaggerURL = ""
	bruteRunAutomate = false
	endpointOnly = false
	bruteContinue = false
	bruteMaxFound = 0
	if err := validateBruteFlags(); err == nil {
		t.Fatal("expected missing --url error")
	}

	swaggerURL = "https://example.com"
	bruteRunAutomate = true
	endpointOnly = true
	bruteContinue = false
	bruteMaxFound = 0
	if err := validateBruteFlags(); err == nil {
		t.Fatal("expected --run-automate/--endpoint-only conflict")
	}

	swaggerURL = "https://example.com"
	bruteRunAutomate = false
	endpointOnly = false
	bruteContinue = false
	bruteMaxFound = 2
	if err := validateBruteFlags(); err == nil {
		t.Fatal("expected --max-found requires --continue error")
	}

	swaggerURL = "https://example.com"
	bruteRunAutomate = true
	endpointOnly = false
	bruteContinue = true
	bruteMaxFound = 2
	if err := validateBruteFlags(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadURLFileEntriesParsesAndNormalizes(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "targets.txt")
	content := "# comment\nexample.com\nhttps://api.example.com/spec.json\n://bad\n\nhttp://host.local/path\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed writing temp targets file: %v", err)
	}

	targets, invalid, err := loadURLFileEntries(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"https://example.com",
		"https://api.example.com/spec.json",
		"http://host.local/path",
	}
	if len(targets) != len(expected) {
		t.Fatalf("expected %d targets, got %d: %#v", len(expected), len(targets), targets)
	}
	for i := range expected {
		if targets[i] != expected[i] {
			t.Fatalf("target %d mismatch: got %q want %q", i, targets[i], expected[i])
		}
	}
	if len(invalid) != 1 || invalid[0] != "://bad" {
		t.Fatalf("unexpected invalid entries: %#v", invalid)
	}
}

func TestDiscoveryCollectorDedupe(t *testing.T) {
	specABytes := []byte(`{"openapi":"3.0.0","info":{"title":"A","version":"1"},"paths":{"/a":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)
	specBBytes := []byte(`{"openapi":"3.0.0","info":{"title":"B","version":"1"},"paths":{"/b":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)
	specA := UnmarshalSpecBytes(specABytes)
	specB := UnmarshalSpecBytes(specBBytes)

	collector := newDiscoveryCollector(DiscoveryOptions{Continue: true, DedupeMode: DedupeURLAndHash})
	if !collector.add("https://example.com/v1/openapi.json", "brute", specA) {
		t.Fatal("expected first add to succeed")
	}
	if collector.add("https://example.com/v1/openapi.json", "brute", specA) {
		t.Fatal("expected duplicate URL to be dropped")
	}
	if collector.add("https://example.com/v2/openapi.json", "brute", specA) {
		t.Fatal("expected duplicate content hash to be dropped")
	}
	if !collector.add("https://example.com/v3/openapi.json", "brute", specB) {
		t.Fatal("expected unique spec to be added")
	}

	if len(collector.results) != 2 {
		t.Fatalf("expected 2 discovered specs, got %d", len(collector.results))
	}
}

func TestDiscoveryCollectorStopConditions(t *testing.T) {
	specA := UnmarshalSpecBytes([]byte(`{"openapi":"3.0.0","info":{"title":"A","version":"1"},"paths":{"/a":{"get":{"responses":{"200":{"description":"ok"}}}}}}`))
	specB := UnmarshalSpecBytes([]byte(`{"openapi":"3.0.0","info":{"title":"B","version":"1"},"paths":{"/b":{"get":{"responses":{"200":{"description":"ok"}}}}}}`))

	stopFirst := newDiscoveryCollector(DiscoveryOptions{Continue: false, DedupeMode: DedupeURLAndHash})
	stopFirst.add("https://x/a", "brute", specA)
	if !stopFirst.shouldStop() {
		t.Fatal("expected stop-first collector to stop after first result")
	}

	maxTwo := newDiscoveryCollector(DiscoveryOptions{Continue: true, MaxFound: 2, DedupeMode: DedupeURLAndHash})
	maxTwo.add("https://x/a", "brute", specA)
	if maxTwo.shouldStop() {
		t.Fatal("did not expect stop before max-found threshold")
	}
	maxTwo.add("https://x/b", "brute", specB)
	if !maxTwo.shouldStop() {
		t.Fatal("expected stop after hitting max-found threshold")
	}
}

func TestValidateSpecResponseFallbackTriggers(t *testing.T) {
	if _, err := ValidateSpecResponse([]byte(`{"openapi":"3.0.0"}`), 500); err == nil {
		t.Fatal("expected non-200 status to fail")
	}
	if _, err := ValidateSpecResponse([]byte(`<html>not a spec</html>`), 200); err == nil {
		t.Fatal("expected non-spec body to fail")
	}
	if _, err := ValidateSpecResponse([]byte(`{"openapi":"3.0.0","info":{"title":"x","version":"1"}}`), 200); err == nil {
		t.Fatal("expected missing paths to fail")
	}
	if _, err := ValidateSpecResponse([]byte(`{"openapi":"3.0.0","info":{"title":"x","version":"1"},"paths":{"/x":{"get":{"responses":{"200":{"description":"ok"}}}}}}`), 200); err != nil {
		t.Fatalf("expected valid spec to pass, got: %v", err)
	}
}

func TestDiscoverSpecsContinueFindsMultipleAndDedupes(t *testing.T) {
	specA := `{"openapi":"3.0.0","info":{"title":"A","version":"1"},"servers":[{"url":"http://api.example.local/v1"}],"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	specB := `{"openapi":"3.0.0","info":{"title":"B","version":"1"},"servers":[{"url":"http://api.example.local/v3"}],"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/openapi.json", "/v2/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(specA))
		case "/v3/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(specB))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	wordlist := filepath.Join(t.TempDir(), "wl.txt")
	if err := os.WriteFile(wordlist, []byte("/v1/openapi.json\n/v2/openapi.json\n/v3/openapi.json\n"), 0644); err != nil {
		t.Fatalf("failed writing wordlist: %v", err)
	}

	InitRateLimiter(0)
	specs, err := DiscoverSpecs(ts.URL, http.Client{}, wordlist, DiscoveryOptions{
		Continue:     true,
		MaxFound:     0,
		ShowProgress: false,
		DedupeMode:   DedupeURLAndHash,
	})
	if err != nil {
		t.Fatalf("discover specs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 unique specs, got %d", len(specs))
	}

	urls := []string{specs[0].URL, specs[1].URL}
	sort.Strings(urls)
	if !strings.HasSuffix(urls[0], "/v1/openapi.json") || !strings.HasSuffix(urls[1], "/v3/openapi.json") {
		t.Fatalf("unexpected discovered URLs: %#v", urls)
	}
}

func TestDiscoverSpecsOnDiscoveredCallback(t *testing.T) {
	spec1 := `{"openapi":"3.0.0","info":{"title":"One","version":"1"},"paths":{"/a":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	spec2 := `{"openapi":"3.0.0","info":{"title":"Two","version":"1"},"paths":{"/b":{"get":{"responses":{"200":{"description":"ok"}}}}}}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(spec1))
		case "/two.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(spec2))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	wordlist := filepath.Join(t.TempDir(), "wl.txt")
	if err := os.WriteFile(wordlist, []byte("/one.json\n/two.json\n"), 0644); err != nil {
		t.Fatalf("failed writing wordlist: %v", err)
	}

	var callbackURLs []string
	_, err := DiscoverSpecs(ts.URL, http.Client{}, wordlist, DiscoveryOptions{
		Continue:     true,
		ShowProgress: false,
		DedupeMode:   DedupeURLAndHash,
		OnDiscovered: func(spec DiscoveredSpec) {
			callbackURLs = append(callbackURLs, spec.URL)
		},
	})
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if len(callbackURLs) != 2 {
		t.Fatalf("expected 2 callback invocations, got %d", len(callbackURLs))
	}
}

func TestExecuteAutomateSpecBytesWithFallbackDiscoveredSpec(t *testing.T) {
	savedOutputFormat := outputFormat
	savedVerbose := verbose
	savedQuiet := quiet
	savedHeaders := cloneHeaders(Headers)
	defer func() {
		outputFormat = savedOutputFormat
		verbose = savedVerbose
		quiet = savedQuiet
		Headers = savedHeaders
	}()

	outputFormat = "json"
	verbose = false
	quiet = true
	Headers = nil
	InitRateLimiter(0)

	var serverURL string
	spec := func(title, base string) string {
		return fmt.Sprintf(`{"openapi":"3.0.0","info":{"title":"%s","version":"1"},"servers":[{"url":"%s"}],"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`, title, base)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(spec("V1", serverURL+"/v1")))
		case "/v3/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(spec("V3", serverURL+"/v3")))
		case "/v1/ping", "/v3/ping":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	wordlist := filepath.Join(t.TempDir(), "wl.txt")
	if err := os.WriteFile(wordlist, []byte("/v1/openapi.json\n/v3/openapi.json\n"), 0644); err != nil {
		t.Fatalf("failed writing wordlist: %v", err)
	}

	specs, err := DiscoverSpecs(ts.URL+"/wrong", http.Client{}, wordlist, DiscoveryOptions{
		Continue:     true,
		ShowProgress: false,
		DedupeMode:   DedupeURLAndHash,
	})
	if err != nil {
		t.Fatalf("expected fallback discovery success, got %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected two discovered specs, got %d", len(specs))
	}

	for _, discovered := range specs {
		report := ExecuteAutomateSpecBytes(http.Client{}, discovered.SpecBytes, discovered.URL, true)
		if report.APITitle == "" {
			t.Fatalf("expected API title for discovered spec %s", discovered.URL)
		}
		results, ok := report.Results.([]Result)
		if !ok {
			t.Fatalf("expected []Result in report, got %T", report.Results)
		}
		if len(results) == 0 {
			t.Fatalf("expected non-empty automate results for %s", discovered.URL)
		}
	}
}

func TestEmitBulkAutomateJSONReportShape(t *testing.T) {
	runs := []AutomateRunReport{{
		Input:          "https://example.com",
		SpecURL:        "https://example.com/openapi.json",
		DiscoveryUsed:  true,
		DiscoveryPhase: "brute",
		APITitle:       "Example API",
		Description:    "desc",
		Results:        []Result{{Method: "get", Status: 200, Target: "/health"}},
	}}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe setup failed: %v", err)
	}
	os.Stdout = w

	err = emitBulkAutomateJSONReport(runs)
	_ = w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("emitBulkAutomateJSONReport returned error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	var payload map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON emitted: %v\n%s", err, buf.String())
	}
	if payload["mode"] != "bulk_automate" {
		t.Fatalf("unexpected mode: %#v", payload["mode"])
	}
	if int(payload["run_count"].(float64)) != 1 {
		t.Fatalf("unexpected run_count: %#v", payload["run_count"])
	}
	runsField, ok := payload["runs"].([]interface{})
	if !ok || len(runsField) != 1 {
		t.Fatalf("unexpected runs payload: %#v", payload["runs"])
	}
}
