package cmd

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	log "github.com/sirupsen/logrus"
)

type SchemaReference struct {
	Required   []string
	Type       string
	Properties struct {
		Property struct {
			Type      string
			Format    string
			Example   string
			Reference string
			XML       struct {
				Name      string
				Namespace string
				Prefix    string
				Attribute bool
				Wrapped   bool
			}
		}
	}
}

func HandleSchemaReference(schema *openapi3.SchemaRef) {

}

func (s SwaggerRequest) SetParametersFromSchema(param *openapi3.ParameterRef, location, schemaRef string, req *openapi3.RequestBodyRef, counter int) SwaggerRequest {
	if param != nil {
		name := strings.TrimPrefix(schemaRef, "#/components/schemas/")
		if s.Def.Components.Schemas[name] != nil {
			schema := s.Def.Components.Schemas[name]
			if schema.Value.Properties != nil {
				for property := range schema.Value.Properties {
					if schema.Value.Properties[property].Ref != "" {
						if counter < 3 {
							s = s.SetParametersFromSchema(param, location, schema.Value.Properties[property].Ref, req, counter+1)
						} else {
							log.Warnf("Nested reference encountered for %s (Property: %s). Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path, property)
							break
						}
					} else if location == "path" {
						if schema.Value.Properties[property].Value.Example != "" && schema.Value.Properties[property].Value.Example != nil {
							s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", schema.Value.Properties[property].Value.Example.(string))
						} else if schema.Value.Properties[property].Value.Type == "string" {
							if strings.Contains(s.URL.Path, param.Value.Name) {
								s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", "test")
							} else {
								s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+strings.ToLower(param.Value.Name)+"}", "test")
							}
						} else {
							if strings.Contains(s.URL.Path, param.Value.Name) {
								s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", "1")
							} else {
								s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+strings.ToLower(param.Value.Name)+"}", "1")
							}
						}
					} else if location == "query" {
						if schema.Value.Properties[property].Value.Example != "" && schema.Value.Properties[property].Value.Example != nil {
							s.Query.Add(param.Value.Name, (schema.Value.Properties[property].Value.Example).(string))
						} else if schema.Value.Properties[property].Value.Type == "string" {
							s.Query.Add(param.Value.Name, "test")
						} else {
							s.Query.Add(param.Value.Name, "1")
						}
					} else if location == "body" {
						if schema.Value.Properties[property].Value.Example != "" && schema.Value.Properties[property].Value.Example != nil {
							s.Body[param.Value.Name] = schema.Value.Properties[property].Value.Example
						} else if schema.Value.Properties[property].Value.Type == "string" {
							s.Body[param.Value.Name] = "test"
						} else {
							s.Body[param.Value.Name] = 1
						}
					}
				}
			} else if schema.Value.Enum != nil {
				if location == "path" {
					value := schema.Value.Enum[rand.Intn(len(schema.Value.Enum))]
					s.URL.Path = strings.ReplaceAll(s.URL.Path, "{"+param.Value.Name+"}", fmt.Sprint(value))
				} else if location == "query" {
					value := schema.Value.Enum[rand.Intn(len(schema.Value.Enum))]
					s.Query.Add(param.Value.Name, fmt.Sprint(value))
				} else if location == "body" {
					value := schema.Value.Enum[rand.Intn(len(schema.Value.Enum))]
					s.Body[param.Value.Name] = value
				}
			} else if schema.Value.AllOf != nil {
				for i := range schema.Value.AllOf {
					if schema.Value.AllOf[i].Ref != "" {
						if counter < 3 {
							s = s.SetParametersFromSchema(param, location, schema.Value.AllOf[i].Ref, req, counter+1)
						} else {
							log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
							break
						}
					}
				}
			} else if schema.Value.OneOf != nil && schema.Value.OneOf[0].Ref != "" {
				if counter < 3 {
					s = s.SetParametersFromSchema(param, location, schema.Value.OneOf[0].Ref, req, counter+1)
				} else {
					log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
				}
			} else if schema.Value.AnyOf != nil && schema.Value.AnyOf[0].Ref != "" {
				if counter < 3 {
					s = s.SetParametersFromSchema(param, location, schema.Value.AnyOf[0].Ref, req, counter+1)
				} else {
					log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
				}
			}
		}
	} else {
		name := strings.TrimPrefix(schemaRef, "#/components/schemas/")
		if s.Def.Components.Schemas[name] != nil {
			schema := s.Def.Components.Schemas[name]
			if schema.Value.Properties != nil {
				for property := range schema.Value.Properties {
					if schema.Value.Properties[property].Ref != "" {
						if counter < 3 {
							s = s.SetParametersFromSchema(param, location, schema.Value.Properties[property].Ref, req, counter+1)
						} else {
							log.Warnf("Nested reference encountered for %s (Property: %s). Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path, property)
							break
						}
					} else {
						if schema.Value.Properties[property].Value.Example != "" {
							s.Body[property] = schema.Value.Properties[property].Value.Example
						} else if schema.Value.Properties[property].Value.Type == "string" {
							s.Body[property] = "test"
						} else {
							s.Body[property] = 1
						}
					}
				}
			} else if schema.Value.Enum != nil {
				value := schema.Value.Enum[rand.Intn(len(schema.Value.Enum))]
				s.Body[name] = value
			} else {
				if schema.Value.AllOf != nil {
					for i := range schema.Value.AllOf {
						if schema.Value.AllOf[i].Ref != "" {
							if counter < 3 {
								s = s.SetParametersFromSchema(param, location, schema.Value.AllOf[i].Ref, req, counter+1)
							} else {
								log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
								break
							}
						}
					}
				} else if schema.Value.OneOf != nil && schema.Value.OneOf[0].Ref != "" {
					if counter < 3 {
						s = s.SetParametersFromSchema(param, location, schema.Value.OneOf[0].Ref, req, counter+1)
					} else {
						log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
					}
				} else if schema.Value.AnyOf != nil && schema.Value.AnyOf[0].Ref != "" {
					if counter < 3 {
						s = s.SetParametersFromSchema(param, location, schema.Value.AnyOf[0].Ref, req, counter+1)
					} else {
						log.Warnf("Nested reference encountered for %s. Test this endpoint manually.\n", s.URL.Scheme+"://"+s.URL.Host+s.URL.Path)
					}
				}
			}
		}
	}
	return s
}
