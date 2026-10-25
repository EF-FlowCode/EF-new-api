package gemini

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"eflow.local/new-api/relaykit/dto"
	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
)

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

func CleanFunctionParameters(params any) any {
	return cleanGeminiFunctionParametersWithDepth(params, 0)
}

func cleanGeminiFunctionParametersWithDepth(params any, depth int) any {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanGeminiFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]any:
		cleanedMap := make(map[string]any, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		sanitizeGeminiEnum(cleanedMap)

		if props, ok := cleanedMap["properties"].(map[string]any); ok && props != nil {
			cleanedProps := make(map[string]any)
			for propName, propValue := range props {
				cleanedProps[propName] = cleanGeminiFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		if items, ok := cleanedMap["items"].(map[string]any); ok && items != nil {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(items, depth+1)
		}
		if itemsArray, ok := cleanedMap["items"].([]any); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		if nested, ok := cleanedMap["anyOf"].([]any); ok && nested != nil {
			cleanedNested := make([]any, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap
	case []any:
		cleanedArray := make([]any, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray
	default:
		return params
	}
}

func cleanGeminiFunctionParametersShallow(params any) any {
	switch v := params.(type) {
	case map[string]any:
		cleanedMap := make(map[string]any, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		sanitizeGeminiEnum(cleanedMap)
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []any:
		return []any{}
	default:
		return params
	}
}

// sanitizeGeminiEnum keeps the enum constraint only when every element is a
// string, the only form Gemini's legacy Schema accepts. Any other shape is
// dropped (e.g. boolean or number elements, which valid JSON Schema allows but
// Gemini rejects with a 400). This widens the value domain for that schema, a
// deliberate lossy relaxation: the declared type is preserved, string enums
// pass through untouched.
func sanitizeGeminiEnum(schema map[string]any) {
	raw, exists := schema["enum"]
	if !exists {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		delete(schema, "enum")
		return
	}
	for _, value := range values {
		if _, isString := value.(string); !isString {
			delete(schema, "enum")
			return
		}
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]any) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch typed := rawType.(type) {
	case string:
		normalized, isNull := normalize(typed)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []any:
		nullable := false
		var chosen string
		for _, item := range typed {
			if value, ok := item.(string); ok {
				normalized, isNull := normalize(value)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func RemoveAdditionalProperties(schema any, depth int) any {
	if depth >= 5 {
		return schema
	}

	value, ok := schema.(map[string]any)
	if !ok || len(value) == 0 {
		return schema
	}
	delete(value, "title")
	delete(value, "$schema")
	if typeVal, exists := value["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch value["type"] {
	case "object":
		delete(value, "additionalProperties")
		if properties, ok := value["properties"].(map[string]any); ok {
			for key, nested := range properties {
				properties[key] = RemoveAdditionalProperties(nested, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := value[field].([]any); ok {
				for i, item := range nested {
					nested[i] = RemoveAdditionalProperties(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := value["items"].(map[string]any); ok {
			value["items"] = RemoveAdditionalProperties(items, depth+1)
		}
	}

	return value
}

func OpenAIToolChoiceToConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	if toolChoiceMap, ok := toolChoice.(map[string]any); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			if function, ok := toolChoiceMap["function"].(map[string]any); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		return nil
	}

	return nil
}

// FunctionDeclaration is a Gemini function declaration built from an OpenAI
// function. Field order and tags follow dto.FunctionRequest so a declaration
// that keeps the typed Schema encodes exactly as before; a schema the typed
// Schema cannot hold goes in ParametersJsonSchema instead.
type FunctionDeclaration struct {
	Description          string `json:"description,omitempty"`
	Name                 string `json:"name"`
	Parameters           any    `json:"parameters,omitempty"`
	ParametersJsonSchema any    `json:"parametersJsonSchema,omitempty"`
}

// NewFunctionDeclaration converts an OpenAI function for Gemini. Empty
// object parameters are still left out; see FunctionParameters for where the
// schema goes.
func NewFunctionDeclaration(function dto.FunctionRequest, jsonSchema bool) FunctionDeclaration {
	declaration := FunctionDeclaration{Description: function.Description, Name: function.Name}
	if function.Parameters == nil {
		return declaration
	}
	parameters := function.Parameters
	switch parameters.(type) {
	case map[string]any, bool:
	default:
		// Typed values (structs, raw JSON) are inspected in their JSON form.
		if generic, err := kitutil.Any2Type[any](parameters); err == nil {
			parameters = generic
		}
	}
	field, value, _ := FunctionParameters(parameters, jsonSchema)
	if field == "parametersJsonSchema" {
		declaration.ParametersJsonSchema = value
	} else {
		declaration.Parameters = value
	}
	return declaration
}

// FunctionParameters returns the Gemini function declaration field that
// carries schema, and its value. A schema that fits Gemini's typed Schema is
// cleaned as before, and still left out when its properties are empty. Any
// other schema goes unchanged to parametersJsonSchema when jsonSchema is
// true; otherwise it is cleaned anyway and misfits names what the typed
// Schema cannot carry.
func FunctionParameters(schema any, jsonSchema bool) (field string, value any, misfits []string) {
	misfits = schemaMisfits(schema)
	object, _ := schema.(map[string]any)
	properties, hasProperties := object["properties"].(map[string]any)
	emptyObject := hasProperties && len(properties) == 0
	if emptyObject {
		for key, item := range object {
			switch key {
			case "type", "properties":
				continue
			case "required":
				if required, _ := item.([]any); len(required) == 0 {
					continue
				}
			}
			if _, allowed := geminiOpenAPISchemaAllowedFields[key]; allowed {
				misfits = append(misfits, "constraints on an empty object")
				break
			}
		}
	}
	if len(misfits) > 0 && jsonSchema {
		return "parametersJsonSchema", schema, nil
	}
	if emptyObject {
		return "parameters", nil, misfits
	}
	return "parameters", CleanFunctionParameters(schema), misfits
}

// ApplyResponseSchema sets the structured output schema from the json_schema
// object of an OpenAI response format. A schema that fits Gemini's typed
// Schema keeps the responseSchema cleanup. Any other schema is sent unchanged
// as responseJsonSchema when jsonSchema is true; otherwise it is cleaned
// anyway and the keywords responseSchema cannot carry are dropped.
func ApplyResponseSchema(config *dto.GeminiChatGenerationConfig, rawFormat json.RawMessage, jsonSchema bool) {
	var format dto.FormatJsonSchema
	if err := kitutil.Unmarshal(rawFormat, &format); err != nil {
		return
	}
	if len(schemaMisfits(format.Schema)) > 0 && jsonSchema {
		// The raw member keeps the client's key order.
		var raw struct {
			Schema json.RawMessage `json:"schema"`
		}
		if err := kitutil.Unmarshal(rawFormat, &raw); err == nil && len(raw.Schema) > 0 {
			config.ResponseJsonSchema = raw.Schema
			return
		}
	}
	config.ResponseSchema = RemoveAdditionalProperties(format.Schema, 0)
}

// schemaMisfits lists what Gemini's typed Schema cannot hold of a JSON Schema
// as written: keywords outside its fields, type arrays, non-string enum
// values, tuple items, boolean schemas, and nesting deeper than
// CleanFunctionParameters keeps. additionalProperties and $schema are not
// misfits: agent tools and OpenAI strict schemas carry them on nearly every
// object, and dropping them as before keeps those requests unchanged for
// upstreams that do not pass the JSON Schema fields through.
func schemaMisfits(schema any) []string {
	found := make(map[string]struct{})
	collectSchemaMisfits(schema, 0, found)
	misfits := make([]string, 0, len(found))
	for misfit := range found {
		misfits = append(misfits, misfit)
	}
	slices.Sort(misfits)
	return misfits
}

func collectSchemaMisfits(schema any, depth int, found map[string]struct{}) {
	if depth > geminiFunctionSchemaMaxDepth {
		found[fmt.Sprintf("nesting deeper than %d levels", geminiFunctionSchemaMaxDepth)] = struct{}{}
		return
	}
	value, ok := schema.(map[string]any)
	if !ok {
		if _, isBool := schema.(bool); isBool {
			found["boolean schemas"] = struct{}{}
		}
		return
	}
	for key, item := range value {
		if _, allowed := geminiOpenAPISchemaAllowedFields[key]; !allowed {
			if key != "additionalProperties" && key != "$schema" {
				found[key] = struct{}{}
			}
			continue
		}
		switch key {
		case "type":
			if _, isArray := item.([]any); isArray {
				found["type arrays"] = struct{}{}
			}
		case "enum":
			values, _ := item.([]any)
			if slices.ContainsFunc(values, func(value any) bool {
				_, isString := value.(string)
				return !isString
			}) {
				found["non-string enum values"] = struct{}{}
			}
		case "properties":
			properties, _ := item.(map[string]any)
			for _, property := range properties {
				collectSchemaMisfits(property, depth+1, found)
			}
		case "items":
			entries, isTuple := item.([]any)
			if !isTuple {
				collectSchemaMisfits(item, depth+1, found)
				continue
			}
			found["tuple items"] = struct{}{}
			for _, entry := range entries {
				collectSchemaMisfits(entry, depth+1, found)
			}
		case "anyOf":
			variants, _ := item.([]any)
			for _, variant := range variants {
				collectSchemaMisfits(variant, depth+1, found)
			}
		}
	}
}
