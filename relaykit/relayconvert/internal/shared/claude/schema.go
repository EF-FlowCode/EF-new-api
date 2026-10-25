package claude

import "maps"

// FunctionParametersToInputSchema normalizes an OpenAI function parameter
// definition for Claude. Claude requires an object schema even when the
// upstream tool omits parameters entirely.
func FunctionParametersToInputSchema(parameters any) map[string]any {
	params, _ := parameters.(map[string]any)
	schema := make(map[string]any, len(params)+2)
	maps.Copy(schema, params)
	if schema["type"] == nil {
		schema["type"] = "object"
	}
	if schema["properties"] == nil {
		schema["properties"] = map[string]any{}
	}
	return schema
}
