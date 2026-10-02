package cmd

import (
	"encoding/base64"
	"fmt"
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
