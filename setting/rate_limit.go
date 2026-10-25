package setting

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"eflow.local/new-api/common"
)

type ModelRequestRateLimitSettings struct {
	Enabled         bool
	DurationMinutes int
	TotalCount      int
	SuccessCount    int
}

type modelRequestRateLimitState struct {
	settings ModelRequestRateLimitSettings
	groups   map[string][2]int
}

var (
	modelRequestRateLimitMutex sync.RWMutex
	modelRequestRateLimit      = modelRequestRateLimitState{
		settings: ModelRequestRateLimitSettings{
			DurationMinutes: 1,
			SuccessCount:    1000,
		},
		groups: map[string][2]int{},
	}
)

type ModelRPMRule struct {
	GlobalRPM int            `json:"global_rpm"`
	TierRPM   map[string]int `json:"group_rpm,omitempty"`
}

type ModelRPMConfig struct {
	Enabled bool                    `json:"enabled"`
	Models  map[string]ModelRPMRule `json:"models"`
}

var modelRPMConfig = ModelRPMConfig{Models: map[string]ModelRPMRule{}}
var modelRPMConfigMutex sync.RWMutex

func ModelRPMConfig2JSONString() string {
	modelRPMConfigMutex.RLock()
	defer modelRPMConfigMutex.RUnlock()

	jsonBytes, err := common.Marshal(modelRPMConfig)
	if err != nil {
		common.SysLog("error marshalling model RPM config: " + err.Error())
		return `{"enabled":false,"models":{}}`
	}
	return string(jsonBytes)
}

func UpdateModelRPMConfigByJSONString(jsonStr string) error {
	config := ModelRPMConfig{}
	if err := common.UnmarshalJsonStr(jsonStr, &config); err != nil {
		return err
	}
	if err := validateModelRPMConfig(config); err != nil {
		return err
	}
	if config.Models == nil {
		config.Models = map[string]ModelRPMRule{}
	}

	modelRPMConfigMutex.Lock()
	modelRPMConfig = config
	modelRPMConfigMutex.Unlock()
	return nil
}

func CheckModelRPMConfig(jsonStr string) error {
	config := ModelRPMConfig{}
	if err := common.UnmarshalJsonStr(jsonStr, &config); err != nil {
		return err
	}
	return validateModelRPMConfig(config)
}

func validateModelRPMConfig(config ModelRPMConfig) error {
	for modelName, rule := range config.Models {
		if strings.TrimSpace(modelName) == "" {
			return fmt.Errorf("model name cannot be empty")
		}
		if rule.GlobalRPM < 1 {
			return fmt.Errorf("model %s global RPM must be at least 1", modelName)
		}
		for tier, rpm := range rule.TierRPM {
			if strings.TrimSpace(tier) == "" {
				return fmt.Errorf("model %s has an empty customer tier", modelName)
			}
			if rpm < 1 {
				return fmt.Errorf("model %s customer tier %s RPM must be at least 1", modelName, tier)
			}
		}
	}
	return nil
}

// GetUserModelRPM resolves the most favorable limit from the user's primary
// and additional customer tiers. A zero result means the model is unlimited.
func GetUserModelRPM(modelName, primaryTier string, additionalTiers []string) int {
	modelRPMConfigMutex.RLock()
	defer modelRPMConfigMutex.RUnlock()

	if !modelRPMConfig.Enabled {
		return 0
	}
	rule, found := modelRPMConfig.Models[modelName]
	if !found {
		return 0
	}

	rpm := rule.GlobalRPM
	for _, tier := range append([]string{primaryTier}, additionalTiers...) {
		if tierRPM, ok := rule.TierRPM[strings.TrimSpace(tier)]; ok && tierRPM > rpm {
			rpm = tierRPM
		}
	}
	return rpm
}

func GetModelRequestRateLimitSettings() ModelRequestRateLimitSettings {
	modelRequestRateLimitMutex.RLock()
	defer modelRequestRateLimitMutex.RUnlock()
	return modelRequestRateLimit.settings
}

// UpdateModelRequestRateLimitSettings parses all supplied fields before
// publishing one complete runtime version.
func UpdateModelRequestRateLimitSettings(values map[string]string) error {
	modelRequestRateLimitMutex.Lock()
	defer modelRequestRateLimitMutex.Unlock()

	next := modelRequestRateLimit
	if value, ok := values["ModelRequestRateLimitEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("ModelRequestRateLimitEnabled must be a boolean: %w", err)
		}
		next.settings.Enabled = parsed
	}
	if value, ok := values["ModelRequestRateLimitDurationMinutes"]; ok {
		parsed, err := parseNonNegativeRateLimitInteger("ModelRequestRateLimitDurationMinutes", value)
		if err != nil {
			return err
		}
		next.settings.DurationMinutes = parsed
	}
	if value, ok := values["ModelRequestRateLimitCount"]; ok {
		parsed, err := parseNonNegativeRateLimitInteger("ModelRequestRateLimitCount", value)
		if err != nil {
			return err
		}
		next.settings.TotalCount = parsed
	}
	if value, ok := values["ModelRequestRateLimitSuccessCount"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return fmt.Errorf("ModelRequestRateLimitSuccessCount must be a positive integer")
		}
		next.settings.SuccessCount = parsed
	}
	if value, ok := values["ModelRequestRateLimitGroup"]; ok {
		groups, err := parseModelRequestRateLimitGroups(value)
		if err != nil {
			return err
		}
		next.groups = groups
	}

	modelRequestRateLimit = next
	return nil
}

func parseNonNegativeRateLimitInteger(name, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return parsed, nil
}

func ModelRequestRateLimitGroup2JSONString() string {
	modelRequestRateLimitMutex.RLock()
	defer modelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := json.Marshal(modelRequestRateLimit.groups)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	return UpdateModelRequestRateLimitSettings(map[string]string{
		"ModelRequestRateLimitGroup": jsonStr,
	})
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	modelRequestRateLimitMutex.RLock()
	defer modelRequestRateLimitMutex.RUnlock()

	if modelRequestRateLimit.groups == nil {
		return 0, 0, false
	}

	limits, found := modelRequestRateLimit.groups[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	_, err := parseModelRequestRateLimitGroups(jsonStr)
	return err
}

func parseModelRequestRateLimitGroups(jsonStr string) (map[string][2]int, error) {
	groups := make(map[string][2]int)
	// 管理页允许清空分组配置，空值等同于没有分组覆盖。
	if strings.TrimSpace(jsonStr) == "" {
		return groups, nil
	}
	err := json.Unmarshal([]byte(jsonStr), &groups)
	if err != nil {
		return nil, err
	}
	for group, limits := range groups {
		if limits[0] < 0 || limits[1] < 1 {
			return nil, fmt.Errorf("group %s has invalid rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return nil, fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}
	return groups, nil
}
