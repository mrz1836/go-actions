package actions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// buildArtifacts assembles the OpenAPI document and the _actions index from the
// frozen, sorted action set. It is called once by Freeze.
func (r *Registry) buildArtifacts() {
	r.buildOpenAPI()
	r.buildIndex()
}

// buildOpenAPI assembles the OpenAPI 3.1 document, marshals it once to JSON, and
// derives the YAML serialization from the same structure. The info block is
// taken from the registry's configured (or default) OpenAPI info.
func (r *Registry) buildOpenAPI() {
	sb := newSchemaBuilder()
	paths := map[string]any{}
	for _, a := range r.actions {
		pathItem, ok := paths[a.path].(map[string]any)
		if !ok {
			pathItem = map[string]any{}
		}
		pathItem[strings.ToLower(a.method)] = buildOperation(sb, a)
		paths[a.path] = pathItem
	}
	sb.components["Error"] = errorSchema()

	version := r.openapiVersion
	if version == "" {
		version = defaultOpenAPIVersion
	}
	components := map[string]any{"schemas": sb.components}
	if len(r.securitySchemes) > 0 {
		schemes := make(map[string]any, len(r.securitySchemes))
		for name, scheme := range r.securitySchemes {
			schemes[name] = scheme.toMap()
		}
		components["securitySchemes"] = schemes
	}

	doc := map[string]any{
		"openapi": version,
		"info": map[string]any{
			"title":       r.info.title,
			"version":     r.info.version,
			"description": r.info.description,
		},
		"paths":      paths,
		"components": components,
	}
	if len(r.servers) > 0 {
		servers := make([]any, len(r.servers))
		for i, s := range r.servers {
			m := map[string]any{"url": s.URL}
			if s.Description != "" {
				m["description"] = s.Description
			}
			servers[i] = m
		}
		doc["servers"] = servers
	}
	if len(r.security) > 0 {
		doc["security"] = securityToList(r.security)
	}

	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(fmt.Errorf("actions: marshal OpenAPI JSON: %w", err))
	}
	r.openapiJSON = append(jsonBytes, '\n')

	var roundTrip any
	if decodeErr := json.Unmarshal(jsonBytes, &roundTrip); decodeErr != nil {
		panic(fmt.Errorf("actions: re-decode OpenAPI: %w", decodeErr))
	}
	yamlBytes, err := yaml.Marshal(roundTrip)
	if err != nil {
		panic(fmt.Errorf("actions: marshal OpenAPI YAML: %w", err))
	}
	r.openapiYAML = yamlBytes
}

// buildOperation builds the OpenAPI operation object for one action.
func buildOperation(sb *schemaBuilder, a anyAction) map[string]any {
	op := map[string]any{
		"operationId": a.id,
		"summary":     a.summary,
		"responses":   buildResponses(sb, a),
	}
	if a.description != "" {
		op["description"] = a.description
	}
	if a.deprecated {
		op["deprecated"] = true
	}
	if len(a.tags) > 0 {
		op["tags"] = a.tags
	}
	// A nil Security inherits the registry-wide default; a non-nil Security
	// overrides it — including an explicitly empty slice, which emits "security:
	// []" to mark the operation public (opting out of any global requirement).
	if a.security != nil {
		op["security"] = securityToList(a.security)
	}
	if params := buildParameters(sb, a.reqType); len(params) > 0 {
		op["parameters"] = params
	}
	if body := buildRequestBody(sb, a.reqType); body != nil {
		op["requestBody"] = body
	}
	return op
}

// buildParameters builds the path/query/header parameter objects of a request.
func buildParameters(sb *schemaBuilder, reqType reflect.Type) []any {
	for reqType.Kind() == reflect.Pointer {
		reqType = reqType.Elem()
	}
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	var params []any
	for i := range reqType.NumField() {
		f := reqType.Field(i)
		in, name := parameterLocation(f)
		if in == "" {
			continue
		}
		schema := sb.schemaFor(f.Type)
		applyConstraints(schema, f.Tag.Get("validate"))
		params = append(params, map[string]any{
			"name":     name,
			"in":       in,
			"required": in == "path" || hasRule(f.Tag.Get("validate"), "required"),
			"schema":   schema,
		})
	}
	return params
}

// parameterLocation reports a field's OpenAPI parameter location and name.
func parameterLocation(f reflect.StructField) (in, name string) {
	for _, key := range []string{"path", "query", "header"} {
		if v, ok := f.Tag.Lookup(key); ok && v != "" {
			return key, v
		}
	}
	return "", ""
}

// buildRequestBody builds the requestBody object, or nil when the request has
// no JSON body fields.
func buildRequestBody(sb *schemaBuilder, reqType reflect.Type) map[string]any {
	for reqType.Kind() == reflect.Pointer {
		reqType = reqType.Elem()
	}
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	schema := sb.structSchema(reqType)
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	return map[string]any{
		"required": true,
		"content":  jsonContent(schema),
	}
}

// buildResponses builds the responses object from an action's StatusDocs.
func buildResponses(sb *schemaBuilder, a anyAction) map[string]any {
	bodySchema, hasBody := sb.responseSchema(a.respType)
	responses := map[string]any{}
	for _, sd := range a.statuses {
		description := sd.Description
		if description == "" {
			description = http.StatusText(sd.Code)
		}
		resp := map[string]any{"description": description}
		switch {
		case sd.Error:
			resp["content"] = jsonContent(map[string]any{"$ref": "#/components/schemas/Error"})
		case hasBody && bodySchema != nil:
			resp["content"] = jsonContent(bodySchema)
		}
		responses[strconv.Itoa(sd.Code)] = resp
	}
	return responses
}

// jsonContent wraps a schema in an application/json content object.
func jsonContent(schema map[string]any) map[string]any {
	return map[string]any{
		"application/json": map[string]any{"schema": schema},
	}
}

// errorSchema is the JSON Schema of the framework's error response envelope.
func errorSchema() map[string]any {
	return map[string]any{
		schemaTypeKey: "object",
		"properties": map[string]any{
			"error":      map[string]any{schemaTypeKey: "string"},
			"code":       map[string]any{schemaTypeKey: "string"},
			"request_id": map[string]any{schemaTypeKey: "string"},
		},
	}
}
