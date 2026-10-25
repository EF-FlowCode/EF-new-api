package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
)

const PricingGroupEndpointOverridesOption = "PricingGroupEndpointOverrides"
const ModelContextWindowsOption = "ModelContextWindows"

var DefaultPricingEndpointOverride = PricingGroupEndpointOverride{
	Type:   "openai",
	Path:   "/v1/chat/completions",
	Method: http.MethodPost,
}

type PricingGroupEndpointOverride struct {
	Type   string `json:"type,omitempty"`
	Path   string `json:"path"`
	Method string `json:"method"`
}

func ParsePricingGroupEndpointOverrides(raw string) (map[string]PricingGroupEndpointOverride, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]PricingGroupEndpointOverride{}, nil
	}

	var values map[string]json.RawMessage
	if err := common.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("invalid group endpoint override JSON: %w", err)
	}

	result := make(map[string]PricingGroupEndpointOverride, len(values))
	for group, value := range values {
		group = strings.TrimSpace(group)
		if group == "" {
			return nil, fmt.Errorf("group endpoint override contains an empty group")
		}

		var path string
		if err := common.Unmarshal(value, &path); err == nil {
			result[group] = normalizePricingGroupEndpointOverride(
				PricingGroupEndpointOverride{Path: path},
			)
		} else {
			var override PricingGroupEndpointOverride
			if err := common.Unmarshal(value, &override); err != nil {
				return nil, fmt.Errorf("invalid endpoint override for %s: %w", group, err)
			}
			result[group] = normalizePricingGroupEndpointOverride(override)
		}

		if err := validatePricingGroupEndpointOverride(result[group]); err != nil {
			return nil, fmt.Errorf("invalid endpoint override for %s: %w", group, err)
		}
	}
	return result, nil
}

func normalizePricingGroupEndpointOverride(override PricingGroupEndpointOverride) PricingGroupEndpointOverride {
	override.Type = strings.TrimSpace(override.Type)
	override.Path = strings.TrimSpace(override.Path)
	override.Method = strings.ToUpper(strings.TrimSpace(override.Method))
	if override.Method == "" {
		override.Method = http.MethodPost
	}
	return override
}

func validatePricingGroupEndpointOverride(override PricingGroupEndpointOverride) error {
	if override.Path == "" || !strings.HasPrefix(override.Path, "/") || strings.HasPrefix(override.Path, "//") {
		return fmt.Errorf("path must be an absolute API path starting with a single slash")
	}
	switch override.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return nil
	default:
		return fmt.Errorf("method must be GET, POST, PUT, PATCH, or DELETE")
	}
}

func GetPricingGroupEndpointOverrides() map[string]PricingGroupEndpointOverride {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[PricingGroupEndpointOverridesOption]
	common.OptionMapRWMutex.RUnlock()

	parsed, err := ParsePricingGroupEndpointOverrides(raw)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to parse %s: %v", PricingGroupEndpointOverridesOption, err))
		return map[string]PricingGroupEndpointOverride{}
	}
	return parsed
}

// ResolvePricingGroupEndpointOverride returns the group override, then the
// configured default override, and finally the built-in OpenAI fallback.
func ResolvePricingGroupEndpointOverride(overrides map[string]PricingGroupEndpointOverride, group string) PricingGroupEndpointOverride {
	if group != "" && group != "default" {
		if override, ok := overrides[group]; ok {
			return override
		}
	}
	if override, ok := overrides["default"]; ok {
		return override
	}
	return DefaultPricingEndpointOverride
}

func ValidatePricingGroupEndpointOverrides(raw string) error {
	_, err := ParsePricingGroupEndpointOverrides(raw)
	return err
}

func ParseModelContextWindows(raw string) (map[string]int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]int64{}, nil
	}

	var values map[string]int64
	if err := common.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("invalid model context window JSON: %w", err)
	}

	result := make(map[string]int64, len(values))
	for modelName, contextWindow := range values {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return nil, fmt.Errorf("model context window contains an empty model name")
		}
		if contextWindow <= 0 {
			return nil, fmt.Errorf("context window for %s must be greater than zero", modelName)
		}
		result[modelName] = contextWindow
	}
	return result, nil
}

func GetModelContextWindows() map[string]int64 {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[ModelContextWindowsOption]
	common.OptionMapRWMutex.RUnlock()

	parsed, err := ParseModelContextWindows(raw)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to parse %s: %v", ModelContextWindowsOption, err))
		return map[string]int64{}
	}
	return parsed
}

// GetModelContextWindow resolves a model-specific window, then the optional
// global default. A zero result means no context metadata is available.
func GetModelContextWindow(modelName string) int64 {
	return resolveModelContextWindow(GetModelContextWindows(), modelName)
}

func resolveModelContextWindow(windows map[string]int64, modelName string) int64 {
	if window, ok := windows[modelName]; ok {
		return window
	}
	return windows["default"]
}

func ValidateModelContextWindows(raw string) error {
	_, err := ParseModelContextWindows(raw)
	return err
}
