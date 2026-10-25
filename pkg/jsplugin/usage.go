package jsplugin

import "fmt"

// ValidateUsageFacts shares the canonical schema contract with metadata examples.
// Missing facts are allowed here; a consumer must require the fields it uses.
func ValidateUsageFacts(schema map[string]UsageFieldSchema, facts map[string]any) error {
	for key, value := range facts {
		field, exists := schema[key]
		if !exists {
			return fmt.Errorf("unknown usage field %q", key)
		}
		if err := validateUsageExampleValue(value, field); err != nil {
			return fmt.Errorf("invalid usage field %q: %w", key, err)
		}
	}
	return nil
}
