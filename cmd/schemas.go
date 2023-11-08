package cmd

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
)

func BuildObjectsFromSchemaDefinitions(doc openapi3.T) {
	if doc.Components.Schemas != nil {
		log.Infof("Building test objects from schema definitions...\n\n")
	}

	if doc.Components.Schemas != nil {
		for definition := range doc.Components.Schemas {
			log.Infof("%s accepts %d parameters:\n", definition, len(doc.Components.Schemas[definition].Value.Properties))
			for property := range doc.Components.Schemas[definition].Value.Properties {
				schemaDef := doc.Components.Schemas[definition]
				definitionProperty := schemaDef.Value.Properties[property]
				if definitionProperty.Value.Type == "string" {
					log.Infof("%s=%s\n", property, "test")
				} else if definitionProperty.Value.Type == "integer" {
					log.Infof("%s=%d\n", property, 1)
				} else if definitionProperty.Ref != "" {
					fmt.Println("[!] [!] [!] " + definitionProperty.Ref) // Item $ref within a property
				}
			}
		}
	}
}
