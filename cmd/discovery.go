package cmd

import (
	"net/url"
	"regexp"
	"strings"
)

// Helpers for following spec references out of Swagger UI HTML pages and their
// initializer scripts. Extraction is permissive: candidates are captured and
// validated by the caller (fetch + parse), so custom document names still work.

// url / "url" / configUrl / specUrl assignment values (also each entry of a urls: [...] array).
var htmlSpecKeyRE = regexp.MustCompile(`(?i)"?(?:config|spec)?url"?\s*:\s*["']([^"']+)["']`)

// spec-url / href / src attribute values.
var htmlSpecAttrRE = regexp.MustCompile(`(?i)(?:spec-url|href|src)\s*=\s*["']([^"']+)["']`)

// Swashbuckle discoveryPaths: arrayFrom('a|b') first argument.
var swashbuckleDiscoveryRE = regexp.MustCompile(`(?i)discoveryPaths\s*:\s*arrayFrom\(\s*['"]([^'"]+)['"]`)

// url: assignments inside an initializer script; a { or , before url avoids matching unrelated keys.
var jsSpecURLAssignRE = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:{\s*|,\s*)url\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`(?i)(?:{\s*|,\s*)url\s*:\s*'([^']+)'`),
	regexp.MustCompile(`(?i)(?:{\s*|,\s*)"url"\s*:\s*"([^"]+)"`),
}

// ExtractSpecURLsFromHTML returns the spec/initializer URLs referenced by a
// Swagger UI page (or swagger-config JSON), resolved against pageURL and deduped.
func ExtractSpecURLsFromHTML(htmlContent, pageURL string) []string {
	var out []string
	add := func(ref string) {
		if abs, ok := resolveReferenceURL(pageURL, ref); ok {
			out = append(out, abs)
		}
	}

	for _, m := range htmlSpecKeyRE.FindAllStringSubmatch(htmlContent, -1) {
		if len(m) > 1 && looksLikeSpecReference(m[1]) {
			add(m[1])
		}
	}

	// Keep spec-like href/src refs, plus initializer scripts (followed for the url they configure).
	for _, m := range htmlSpecAttrRE.FindAllStringSubmatch(htmlContent, -1) {
		if len(m) > 1 && (looksLikeSpecReference(m[1]) || looksLikeSwaggerInitCandidate(m[1])) {
			add(m[1])
		}
	}

	// Swashbuckle discoveryPaths are relative to the site root.
	if dm := swashbuckleDiscoveryRE.FindStringSubmatch(htmlContent); len(dm) > 1 {
		if paths := strings.Split(dm[1], "|"); len(paths) > 0 {
			first := strings.TrimSpace(paths[0])
			if first != "" {
				if !strings.HasPrefix(first, "/") && !strings.Contains(first, "://") {
					first = "/" + first
				}
				add(first)
			}
		}
	}

	return dedupeURLs(out)
}

// extractJSSpecURLs pulls url: assignments from an initializer script body.
func extractJSSpecURLs(js string) []string {
	var out []string
	for _, re := range jsSpecURLAssignRE {
		for _, m := range re.FindAllStringSubmatch(js, -1) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// looksLikeSpecReference accepts references that plausibly point at an
// OpenAPI/Swagger document (including custom .json/.yaml names) and rejects UI
// assets and bundles. Matches are still validated by fetching and parsing.
func looksLikeSpecReference(ref string) bool {
	s := strings.TrimSpace(ref)
	if s == "" {
		return false
	}
	lo := strings.ToLower(s)
	if strings.HasPrefix(lo, "javascript:") || strings.HasPrefix(lo, "data:") || strings.HasPrefix(lo, "#") {
		return false
	}
	for _, ext := range []string{".js", ".css", ".map", ".html", ".htm", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf"} {
		if strings.HasSuffix(lo, ext) {
			return false
		}
	}
	if strings.Contains(lo, "swagger-ui-bundle") || strings.Contains(lo, "swagger-ui-standalone") {
		return false
	}
	if strings.Contains(lo, "api-docs") || strings.Contains(lo, "api_docs") || strings.Contains(lo, "apidocs") {
		return true
	}
	if strings.Contains(lo, "openapi") {
		return true
	}
	if strings.Contains(lo, "swagger.json") || strings.Contains(lo, "swagger.yaml") || strings.Contains(lo, "swagger.yml") {
		return true
	}
	if strings.Contains(lo, "api-json") || strings.Contains(lo, "api_json") {
		return true
	}
	if strings.HasSuffix(lo, ".json") || strings.HasSuffix(lo, ".yaml") || strings.HasSuffix(lo, ".yml") {
		return true
	}
	if strings.HasSuffix(lo, "-json") || strings.HasSuffix(lo, "_json") {
		return true
	}
	if strings.Contains(lo, "swagger-resources") {
		return true
	}
	return false
}

// Library bundles are never followed as a config source: they are large and full of internal url: strings.
var swaggerUILibraryScripts = []string{
	"swagger-ui-bundle", "swagger-ui-standalone", "swagger-ui-es-bundle",
	"swagger-ui-layout", "swagger-ui-plugins", "swagger-ui.min", "swagger-ui.js",
	"redoc.standalone", "redoc.min", "redoc.js",
}

// looksLikeSwaggerInitCandidate reports whether a .js reference is a plausible
// initializer (swagger-initializer.js, swagger-ui-init.js, index.js, ...) rather
// than a library bundle.
func looksLikeSwaggerInitCandidate(rawURL string) bool {
	p := strings.ToLower(urlPath(rawURL))
	if !strings.HasSuffix(p, ".js") {
		return false
	}
	for _, lib := range swaggerUILibraryScripts {
		if strings.Contains(p, lib) {
			return false
		}
	}
	return true
}

// urlPath returns the path component of a URL, or the input unchanged if it does
// not parse (so relative references still work).
func urlPath(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}

// bodyHasEmbeddedSpec is a cheap sniff for a spec inlined in a JavaScript body.
func bodyHasEmbeddedSpec(body string) bool {
	lo := strings.ToLower(body)
	return strings.Contains(lo, `"paths"`) && (strings.Contains(lo, `"openapi"`) || strings.Contains(lo, `"swagger"`))
}

// looksLikeHTMLDocument reports whether a response body is an HTML document.
func looksLikeHTMLDocument(body string) bool {
	t := strings.TrimSpace(strings.ToLower(body))
	return strings.HasPrefix(t, "<!doctype") || strings.HasPrefix(t, "<html")
}

// Substrings found in Cloudflare/WAF browser-challenge interstitial pages.
var challengeMarkers = []string{
	"just a moment...",
	"_cf_chl_opt",
	"challenge-platform",
	"cf-browser-verification",
	"cf_chl_",
	"enable javascript and cookies to continue",
	"checking your browser before accessing",
	"attention required! | cloudflare",
	"ddos protection by cloudflare",
	"/cdn-cgi/challenge-platform",
}

// looksLikeChallengeResponse reports whether a response body is a WAF/anti-bot challenge page.
func looksLikeChallengeResponse(body string) bool {
	lo := strings.ToLower(body)
	for _, m := range challengeMarkers {
		if strings.Contains(lo, m) {
			return true
		}
	}
	return false
}

// isExternalDemoSpecURL guards against following the SwaggerUIBundle default demo specs.
func isExternalDemoSpecURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "petstore.swagger.io", "generator.swagger.io", "swagger.io", "www.swagger.io":
		return true
	}
	return false
}

// resolveReferenceURL resolves a (possibly relative) reference against pageURL.
func resolveReferenceURL(pageURL, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", false
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(r)
	if resolved.Scheme == "" || resolved.Host == "" {
		return "", false
	}
	return resolved.String(), true
}

// dedupeURLs removes duplicate and empty entries, preserving order.
func dedupeURLs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
