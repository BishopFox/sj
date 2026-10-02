package cmd

import (
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
)

var (
	autoApplyAPIKey    string = "n"
	autoApplyBasicAuth string = "n"
	autoApplyBearer    string = "n"
	basicAuthUser      string
	basicAuthPass      string
	basicAuth          []byte
	basicAuthString    string
	bearerToken        string
	// authQueryParams holds API keys that a security scheme places in the query
	// string (apiKey with "in: query"). BuildRequestsFromPaths appends each one
	// to every operation's URL, mirroring how Headers carries header-based auth.
	authQueryParams []queryAuthParam
	// authCookieParams holds API keys that a security scheme places in a cookie
	// (apiKey with "in: cookie"). BuildRequestsFromPaths merges each one into the
	// single Cookie header it already builds from per-operation cookie params.
	authCookieParams []queryAuthParam
)

// queryAuthParam is a single name/value pair to add to request query strings.
type queryAuthParam struct {
	name  string
	value string
}

// securitySchemeSource returns the security-scheme definitions from a parsed
// spec, normalized to the OpenAPI 3.x shape CheckSecuritySchemes understands. It
// prefers the v3 location (components.securitySchemes) and falls back to the
// Swagger 2.0 top-level securityDefinitions, so both spec versions are handled
// by the same downstream switch. Returns nil when neither location defines any.
func securitySchemeSource(spec map[string]interface{}) map[string]interface{} {
	if components, ok := spec["components"].(map[string]interface{}); ok && components != nil {
		if schemes, ok := components["securitySchemes"].(map[string]interface{}); ok && len(schemes) > 0 {
			return schemes
		}
	}

	// Swagger 2.0 keeps schemes under a top-level securityDefinitions object.
	if defs, ok := spec["securityDefinitions"].(map[string]interface{}); ok && len(defs) > 0 {
		normalized := make(map[string]interface{}, len(defs))
		for name, def := range defs {
			if scheme, ok := def.(map[string]interface{}); ok {
				normalized[name] = normalizeV2Scheme(scheme)
			} else {
				normalized[name] = def
			}
		}
		return normalized
	}

	return nil
}

// normalizeV2Scheme rewrites a Swagger 2.0 security scheme into the OpenAPI 3.x
// shape. Swagger 2.0 expresses HTTP Basic as its own top-level type ("basic"),
// whereas 3.x models it as type "http" with scheme "basic". apiKey schemes use
// the same name/in/type fields in both versions, so they pass through unchanged;
// oauth2 is left as-is (handled no differently than in a 3.x spec today).
func normalizeV2Scheme(scheme map[string]interface{}) map[string]interface{} {
	if typ, _ := scheme["type"].(string); typ != "basic" {
		return scheme
	}
	converted := make(map[string]interface{}, len(scheme)+1)
	for k, v := range scheme {
		converted[k] = v
	}
	converted["type"] = "http"
	converted["scheme"] = "basic"
	return converted
}

func CheckSecuritySchemes(spec map[string]interface{}) {
	securitySchemes := securitySchemeSource(spec)
	if len(securitySchemes) == 0 {
		printInfo("No security schemes defined.\n")
		return
	}

	printInfo("Found security schemes:\n")
	var apiKey string
	var apiKeyName string

	for mechanism, value := range securitySchemes {
		printInfo("  - %s\n", mechanism)
		scheme, ok := value.(map[string]interface{})
		if !ok {
			continue
		}

		if typ, ok := scheme["type"].(string); ok {
			switch typ {
			case "http":
				schemeName, _ := scheme["scheme"].(string)
				switch strings.ToLower(schemeName) {
				case "basic":
					if quiet {
						autoApplyBasicAuth = "n"
						printWarn("A basic authentication header is accepted. Review the spec and craft a header manually using the -H flag.")
					} else {
						printInfo("Basic Authentication is accepted. Supply a username and password? (y/N)\n")
						fmt.Scanln(&autoApplyBasicAuth)
						autoApplyBasicAuth = strings.ToLower(autoApplyBasicAuth)
						if autoApplyBasicAuth == "y" {
							printInfo("Enter a username.")
							fmt.Scanln(&basicAuthUser)
							printInfo("Enter a password.")
							fmt.Scanln(&basicAuthPass)
							basicAuth = []byte(basicAuthUser + ":" + basicAuthPass)
							basicAuthString = base64.StdEncoding.EncodeToString(basicAuth)
							printInfo("Using %s as the Basic Auth value.\n", basicAuthString)
							Headers = append(Headers, "Authorization: Basic "+basicAuthString)
						} else {
							printWarn("A basic authentication header is accepted. Review the spec and craft a header manually using the -H flag.")
						}
					}
				case "bearer":
					if quiet {
						autoApplyBearer = "n"
						printWarn("A bearer token is accepted. Review the spec and craft a token manually using the -H flag.")
					} else {
						printInfo("A bearer token is accepted. Would you like to provide one? (y/N)\n")
						fmt.Scanln(&autoApplyBearer)
						autoApplyBearer = strings.ToLower(autoApplyBearer)
						if autoApplyBearer == "y" {
							printInfo("What value would you like to use for the Bearer Token? ")
							fmt.Scanln(&bearerToken)
							Headers = append(Headers, "Authorization: Bearer "+bearerToken)
						} else {
							printWarn("A bearer token is accepted. Review the spec and craft a token manually using the -H flag.")
						}
					}
				}
			case "apiKey":
				if inVal, ok := scheme["in"].(string); ok {
					switch inVal {
					case "query":
						printInfo("An API key can be provided via a parameter string. Would you like to apply one? (y/N)\n")
						if quiet {
							autoApplyAPIKey = "n"
						} else {
							fmt.Scanln(&autoApplyAPIKey)
							autoApplyAPIKey = strings.ToLower(autoApplyAPIKey)
						}

						if autoApplyAPIKey == "y" {
							if nameVal, ok := scheme["name"].(string); ok {
								apiKeyName = nameVal
							}
							printInfo("What value would you like to use for the API key (%s)?", apiKeyName)
							fmt.Scanln(&apiKey)
							printInfo("Using %s=%s as the API key in all requests.\n", apiKeyName, apiKey)
							// Record the key so BuildRequestsFromPaths adds it to
							// every request's query string, not just the diagnostics.
							authQueryParams = append(authQueryParams, queryAuthParam{name: apiKeyName, value: apiKey})
						}
					case "header":
						if nameVal, ok := scheme["name"].(string); ok {
							printInfo("An API key can be provided via the header %s. Would you like to apply one? (y/N)\n", nameVal)
							if quiet {
								autoApplyAPIKey = "n"
							} else {
								fmt.Scanln(&autoApplyAPIKey)
								autoApplyAPIKey = strings.ToLower(autoApplyAPIKey)
							}
							if autoApplyAPIKey == "y" {
								apiKeyName = nameVal
								printInfo("What value would you like to use for the API key (%s)?", apiKeyName)
								fmt.Scanln(&apiKey)
								Headers = append(Headers, nameVal+": "+apiKey)
							}
						}
					case "cookie":
						if nameVal, ok := scheme["name"].(string); ok {
							printInfo("An API key can be provided via the cookie %s. Would you like to apply one? (y/N)\n", nameVal)
							if quiet {
								autoApplyAPIKey = "n"
							} else {
								fmt.Scanln(&autoApplyAPIKey)
								autoApplyAPIKey = strings.ToLower(autoApplyAPIKey)
							}
							if autoApplyAPIKey == "y" {
								apiKeyName = nameVal
								printInfo("What value would you like to use for the API key (%s)?", apiKeyName)
								fmt.Scanln(&apiKey)
								// Record the key so BuildRequestsFromPaths merges it into
								// the Cookie header it builds for every request.
								authCookieParams = append(authCookieParams, queryAuthParam{name: apiKeyName, value: apiKey})
							}
						}
					}
				}
			}
		}

		if bearerFormat, ok := scheme["bearerFormat"].(string); ok {
			printInfo("  - bearerFormat: %s\n", bearerFormat)
		}
	}
}

// securityState classifies an operation's effective security requirement.
type securityState int

const (
	// secUndeclared: neither the operation nor the spec declares any requirement.
	secUndeclared securityState = iota
	// secRequired: a requirement applies and anonymous access is not offered.
	secRequired
	// secOptional: a requirement applies, but an empty {} entry alongside it
	// means anonymous access is also accepted.
	secOptional
	// secPublic: an operation-level `security: []` explicitly disables auth,
	// overriding any spec-level default.
	secPublic
)

// httpMethods is the set of path-item keys that denote an operation, so the
// security summary skips siblings like "parameters", "summary", or "$ref".
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// opSecurity is the effective security requirement resolved for one operation:
// its classification, the scheme names any requirement references (deduped), any
// referenced names with no matching definition, and the raw alternatives so a
// credential check can honor AND within a requirement object and OR across them.
type opSecurity struct {
	state        securityState
	schemes      []string
	undefined    []string
	alternatives [][]string
}

// resolveOperationSecurity computes the effective security for one operation.
// The operation's own `security` wins when the key is present (an empty list is
// an explicit public override); otherwise the spec-level `security` is inherited.
// Each requirement object (scheme -> scopes) ANDs its schemes; alternative
// objects OR; an empty {} object marks anonymous access as acceptable.
func resolveOperationSecurity(spec, opMap map[string]interface{}) opSecurity {
	raw, fromOp := opMap["security"]
	if !fromOp {
		var present bool
		raw, present = spec["security"]
		if !present {
			return opSecurity{state: secUndeclared}
		}
	}

	list, ok := raw.([]interface{})
	if !ok {
		// A malformed security value is treated as undeclared rather than guessed at.
		return opSecurity{state: secUndeclared}
	}

	if len(list) == 0 {
		// Empty at the operation level opts out of auth; inherited-empty just
		// means no spec-level default applies.
		if fromOp {
			return opSecurity{state: secPublic}
		}
		return opSecurity{state: secUndeclared}
	}

	defined := securitySchemeSource(spec)

	s := opSecurity{}
	seen := map[string]bool{}
	hasAnonymous := false
	for _, item := range list {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if len(obj) == 0 {
			hasAnonymous = true
			continue
		}
		// Map order is not stable, so sort the ANDed scheme names for
		// deterministic labels and summary output.
		var alt []string
		for _, name := range slices.Sorted(maps.Keys(obj)) {
			alt = append(alt, name)
			if !seen[name] {
				seen[name] = true
				s.schemes = append(s.schemes, name)
				if defined[name] == nil {
					s.undefined = append(s.undefined, name)
				}
			}
		}
		if len(alt) > 0 {
			s.alternatives = append(s.alternatives, alt)
		}
	}

	switch {
	case len(s.alternatives) > 0 && hasAnonymous:
		s.state = secOptional
	case len(s.alternatives) > 0:
		s.state = secRequired
	case hasAnonymous:
		// Only empty {} objects: anonymous access with no scheme required.
		s.state = secPublic
	default:
		s.state = secUndeclared
	}
	return s
}

// label renders the effective requirement as a compact string: scheme names are
// joined with "+" within an AND requirement and " | " across OR alternatives.
// Public operations read "public"; undeclared ones yield an empty string so the
// caller can omit the field entirely.
func (s opSecurity) label() string {
	switch s.state {
	case secPublic:
		return "public"
	case secUndeclared:
		return ""
	}

	alts := make([]string, 0, len(s.alternatives))
	for _, alt := range s.alternatives {
		alts = append(alts, strings.Join(alt, "+"))
	}
	joined := strings.Join(alts, " | ")
	if s.state == secOptional {
		return "optional: " + joined
	}
	return joined
}

// SummarizeOperationSecurity prints, to stderr, the effective security
// requirement of every declared operation. It is computed from the spec alone
// (no requests), so it runs for automate, endpoints, and prepare alike, and it
// stays off stdout to keep that stream pipeable.
func SummarizeOperationSecurity(spec map[string]interface{}) {
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		return
	}

	type row struct {
		method string
		path   string
		sec    opSecurity
	}
	var rows []row
	for _, pathName := range slices.Sorted(maps.Keys(paths)) {
		pathItem, ok := paths[pathName].(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range slices.Sorted(maps.Keys(pathItem)) {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}
			opMap, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, row{strings.ToUpper(method), pathName, resolveOperationSecurity(spec, opMap)})
		}
	}
	if len(rows) == 0 {
		return
	}

	printInfo("Operation security requirements:\n")
	for _, r := range rows {
		switch r.sec.state {
		case secRequired:
			printInfo("  %s %-6s %s  requires: %s\n", red("✗"), r.method, r.path, r.sec.label())
		case secOptional:
			printInfo("  %s %-6s %s  %s\n", yellow("⚠"), r.method, r.path, r.sec.label())
		case secPublic:
			printInfo("  %s %-6s %s  PUBLIC (security: [])\n", yellow("⚠"), r.method, r.path)
		default:
			printInfo("  %s %-6s %s  none declared\n", faint("-"), r.method, r.path)
		}
		if len(r.sec.undefined) > 0 {
			printWarn("%s %s references undefined scheme(s): %s", r.method, r.path, strings.Join(r.sec.undefined, ", "))
		}
	}
	printInfo("\n")
}

// requirementSatisfiedByCredentials reports whether a supplied credential
// already satisfies at least one of the operation's requirement alternatives, so
// the missing-auth flag can be suppressed on authenticated runs. It checks the
// base credential state (headers snapshotted before per-operation mutation, plus
// the query/cookie auth globals) against each scheme's definition.
func requirementSatisfiedByCredentials(s opSecurity, spec map[string]interface{}, userHeaders []string) bool {
	if len(s.alternatives) == 0 {
		return false
	}
	defined := securitySchemeSource(spec)
	for _, alt := range s.alternatives {
		satisfied := true
		for _, name := range alt {
			if !credentialSuppliedForScheme(defined, name, userHeaders) {
				satisfied = false
				break
			}
		}
		if satisfied {
			return true
		}
	}
	return false
}

// credentialSuppliedForScheme reports whether a credential matching one named
// scheme was supplied, by consulting the scheme's definition for where it lives
// (header/bearer/basic in Headers, apiKey in query/cookie/header).
func credentialSuppliedForScheme(defined map[string]interface{}, name string, userHeaders []string) bool {
	scheme, _ := defined[name].(map[string]interface{})
	if scheme == nil {
		return false
	}
	switch typ, _ := scheme["type"].(string); typ {
	case "http":
		switch s, _ := scheme["scheme"].(string); strings.ToLower(s) {
		case "bearer":
			return headerValuePrefixPresent(userHeaders, "authorization", "bearer ")
		case "basic":
			return headerValuePrefixPresent(userHeaders, "authorization", "basic ")
		default:
			// An unrecognized http scheme: any Authorization header counts.
			return headerPresent(userHeaders, "authorization")
		}
	case "apiKey":
		keyName, _ := scheme["name"].(string)
		if keyName == "" {
			return false
		}
		switch inVal, _ := scheme["in"].(string); inVal {
		case "header":
			return headerPresent(userHeaders, keyName)
		case "query":
			return slices.ContainsFunc(authQueryParams, func(p queryAuthParam) bool {
				return strings.EqualFold(p.name, keyName)
			})
		case "cookie":
			return slices.ContainsFunc(authCookieParams, func(p queryAuthParam) bool {
				return strings.EqualFold(p.name, keyName)
			})
		}
	}
	return false
}

// splitHeader splits a "Key: Value" header entry, reporting whether a colon was
// found. Key and value are trimmed.
func splitHeader(h string) (key, value string, ok bool) {
	idx := strings.Index(h, ":")
	if idx == -1 {
		return "", "", false
	}
	return strings.TrimSpace(h[:idx]), strings.TrimSpace(h[idx+1:]), true
}

// headerPresent reports whether headers carries a non-empty value for name
// (case-insensitive key match).
func headerPresent(headers []string, name string) bool {
	for _, h := range headers {
		if k, v, ok := splitHeader(h); ok && strings.EqualFold(k, name) && v != "" {
			return true
		}
	}
	return false
}

// headerValuePrefixPresent reports whether headers carries name with a value
// beginning valuePrefix (both compared case-insensitively).
func headerValuePrefixPresent(headers []string, name, valuePrefix string) bool {
	for _, h := range headers {
		if k, v, ok := splitHeader(h); ok && strings.EqualFold(k, name) {
			if strings.HasPrefix(strings.ToLower(v), valuePrefix) {
				return true
			}
		}
	}
	return false
}
