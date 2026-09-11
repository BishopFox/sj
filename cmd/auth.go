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
)

func CheckSecuritySchemes(spec map[string]interface{}) {
	components, ok := spec["components"].(map[string]interface{})
	if ok && components != nil {
		securitySchemes, ok := components["securitySchemes"].(map[string]interface{})
		if !ok || len(securitySchemes) == 0 {
			printInfo("No security schemes defined.\n")
		} else {
			if outputFormat != "json" {
				printInfo("Found security schemes:\n")
			}
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
						if schemeType := scheme["scheme"]; schemeType != nil {
							switch schemeType {
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
								printWarn("A bearer token is accepted. Review the spec and craft a token manually using the -H flag.")
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
								}
							case "header":
								if mechanism == "bearer" {
									printInfo("A bearer token is accepted. Would you like to provide one? (y/N)\n")
									if quiet {
										autoApplyBearer = "n"
									} else {
										fmt.Scanln(&autoApplyBearer)
										autoApplyBearer = strings.ToLower(autoApplyBearer)
									}
									if autoApplyBearer == "y" {
										printInfo("What value would you like to use for the Bearer Token? ")
										fmt.Scanln(&bearerToken)
										Headers = append(Headers, "Authorization: Bearer "+bearerToken)
									} else {
										printWarn("A bearer token is accepted. Review the spec and craft a header manually using the -H flag.")
									}
								} else {
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
	}
}
