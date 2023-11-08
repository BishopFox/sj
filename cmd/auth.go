package cmd

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	log "github.com/sirupsen/logrus"
)

var (
	autoApplyAPIKey    string = "n"
	autoApplyBasicAuth string = "n"
	basicAuthUser      string
	basicAuthPass      string
	basicAuth          []byte
	basicAuthString    string
)

func CheckSecDefs(doc3 openapi3.T) (apiInQuery bool, apiKey string, apiKeyName string) {

	if len(doc3.Components.SecuritySchemes) > 0 {
		log.Info("Available authentication mechanisms: ")
	}

	for mechanism := range doc3.Components.SecuritySchemes {
		fmt.Printf("    - %s (%s)\n", mechanism, doc3.Components.SecuritySchemes[mechanism].Value.Scheme)
		if doc3.Components.SecuritySchemes[mechanism].Value.Type == "http" && !quiet {
			if doc3.Components.SecuritySchemes[mechanism].Value.Scheme == "basic" {
				log.Infof("Basic Authentication is accepted. Supply a username and password? (y/N)")
				fmt.Scanln(&autoApplyBasicAuth)
				autoApplyBasicAuth = strings.ToLower(autoApplyBasicAuth)
				if autoApplyBasicAuth == "y" {
					fmt.Printf("Enter a username.")
					fmt.Scanln(&basicAuthUser)
					fmt.Printf("Enter a password.")
					fmt.Scanln(&basicAuthPass)
					basicAuth = []byte(basicAuthUser + ":" + basicAuthPass)
					basicAuthString = base64.StdEncoding.EncodeToString(basicAuth)
					log.Infof("Using %s as the Basic Auth value.", basicAuthString)
					Headers = append(Headers, "Authorization: Basic "+basicAuthString)
				} else {
					log.Warn("A basic authentication header is accepted. Review the spec and craft a header manually using the -H flag.")
				}
			} else if strings.ToLower(doc3.Components.SecuritySchemes[mechanism].Value.Scheme) == "bearer" {
				log.Warn("A bearer token is accepted. Review the spec and craft a token manually using the -H flag.")
			}
		} else if doc3.Components.SecuritySchemes[mechanism].Value.Type == "apiKey" && doc3.Components.SecuritySchemes[mechanism].Value.In == "query" && !quiet {
			apiInQuery = true
			log.Infof("An API key can be provided via a parameter string. Would you like to apply one? (y/N)")
			fmt.Scanln(&autoApplyAPIKey)
			autoApplyAPIKey = strings.ToLower(autoApplyAPIKey)
			if autoApplyAPIKey == "y" {
				apiKeyName = doc3.Components.SecuritySchemes[mechanism].Value.Name
				fmt.Printf("What value would you like to use for the API key (%s)?", apiKeyName)
				fmt.Scanln(&apiKey)
				log.Infof("Using %s=%s as the API key in all requests.", apiKeyName, apiKey)
			}
		} else if doc3.Components.SecuritySchemes[mechanism].Value.Type == "apiKey" && doc3.Components.SecuritySchemes[mechanism].Value.In == "header" && !quiet {
			log.Infof("An API key can be provided via the header %s. Would you like to apply one? (y/N)", doc3.Components.SecuritySchemes[mechanism].Value.Name)
			fmt.Scanln(&autoApplyAPIKey)
			autoApplyAPIKey = strings.ToLower(autoApplyAPIKey)
			if autoApplyAPIKey == "y" {
				apiKeyName = doc3.Components.SecuritySchemes[mechanism].Value.Name
				fmt.Printf("What value would you like to use for the API key (%s)?", apiKeyName)
				fmt.Scanln(&apiKey)
				Headers = append(Headers, doc3.Components.SecuritySchemes[mechanism].Value.Name+": "+apiKey)
			}
		}
	}
	fmt.Printf("\n")
	return apiInQuery, apiKey, apiKeyName
}
