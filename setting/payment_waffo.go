package setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
)

// WaffoConfig is one immutable runtime version of the Waffo settings.
type WaffoConfig struct {
	Enabled           bool
	APIKey            string
	PrivateKey        string
	PublicCert        string
	SandboxPublicCert string
	SandboxAPIKey     string
	SandboxPrivateKey string
	Sandbox           bool
	MerchantID        string
	NotifyURL         string
	ReturnURL         string
	Currency          string
	UnitPrice         float64
	MinTopUp          int
	PayMethods        []constant.WaffoPayMethod
}

var (
	waffoConfig   atomic.Pointer[WaffoConfig]
	waffoConfigMu sync.Mutex
)

func init() {
	publishWaffoConfig(WaffoConfig{
		UnitPrice:  1,
		MinTopUp:   1,
		PayMethods: constant.DefaultWaffoPayMethods(),
	})
}

func GetWaffoConfig() WaffoConfig {
	current := waffoConfig.Load()
	if current == nil {
		return WaffoConfig{UnitPrice: 1, MinTopUp: 1, PayMethods: constant.DefaultWaffoPayMethods()}
	}
	return cloneWaffoConfig(*current)
}

// ValidateWaffoOptionValues validates a partial option batch without
// publishing it. The database option path uses this before committing.
func ValidateWaffoOptionValues(values map[string]string) error {
	_, _, err := applyWaffoOptionValues(GetWaffoConfig(), values)
	return err
}

func UpdateWaffoConfig(values map[string]string) error {
	waffoConfigMu.Lock()
	defer waffoConfigMu.Unlock()

	next, touched, err := applyWaffoOptionValues(GetWaffoConfig(), values)
	if err != nil {
		return err
	}
	if touched {
		publishWaffoConfig(next)
	}
	return nil
}

func applyWaffoOptionValues(current WaffoConfig, values map[string]string) (WaffoConfig, bool, error) {
	next := cloneWaffoConfig(current)
	touched := false
	for key, value := range values {
		switch key {
		case "WaffoEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return current, false, fmt.Errorf("WaffoEnabled must be a boolean: %w", err)
			}
			next.Enabled = parsed
			touched = true
		case "WaffoApiKey":
			next.APIKey = value
			touched = true
		case "WaffoPrivateKey":
			next.PrivateKey = value
			touched = true
		case "WaffoPublicCert":
			next.PublicCert = value
			touched = true
		case "WaffoSandboxPublicCert":
			next.SandboxPublicCert = value
			touched = true
		case "WaffoSandboxApiKey":
			next.SandboxAPIKey = value
			touched = true
		case "WaffoSandboxPrivateKey":
			next.SandboxPrivateKey = value
			touched = true
		case "WaffoSandbox":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return current, false, fmt.Errorf("WaffoSandbox must be a boolean: %w", err)
			}
			next.Sandbox = parsed
			touched = true
		case "WaffoMerchantId":
			next.MerchantID = value
			touched = true
		case "WaffoNotifyUrl":
			next.NotifyURL = value
			touched = true
		case "WaffoReturnUrl":
			next.ReturnURL = value
			touched = true
		case "WaffoCurrency":
			next.Currency = value
			touched = true
		case "WaffoUnitPrice":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return current, false, fmt.Errorf("WaffoUnitPrice must be a non-negative finite number")
			}
			next.UnitPrice = parsed
			touched = true
		case "WaffoMinTopUp":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return current, false, fmt.Errorf("WaffoMinTopUp must be at least 0")
			}
			next.MinTopUp = parsed
			touched = true
		case "WaffoPayMethods":
			methods, err := parseWaffoPayMethods(value)
			if err != nil {
				return current, false, err
			}
			next.PayMethods = methods
			touched = true
		}
	}
	return next, touched, nil
}

func parseWaffoPayMethods(value string) ([]constant.WaffoPayMethod, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return constant.DefaultWaffoPayMethods(), nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil, fmt.Errorf("WaffoPayMethods must be a JSON array")
	}
	objects := make([]map[string]any, 0)
	if err := common.Unmarshal([]byte(value), &objects); err != nil {
		return nil, fmt.Errorf("WaffoPayMethods must contain JSON objects: %w", err)
	}
	for i, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("WaffoPayMethods[%d] must be an object", i)
		}
	}

	methods := make([]constant.WaffoPayMethod, 0)
	if err := common.Unmarshal([]byte(value), &methods); err != nil {
		return nil, fmt.Errorf("WaffoPayMethods must be a valid JSON array: %w", err)
	}
	return methods, nil
}

func cloneWaffoConfig(config WaffoConfig) WaffoConfig {
	config.PayMethods = append([]constant.WaffoPayMethod(nil), config.PayMethods...)
	return config
}

func publishWaffoConfig(next WaffoConfig) {
	next = cloneWaffoConfig(next)
	waffoConfig.Store(&next)
}

// GetWaffoPayMethods returns a detached copy of the published payment methods.
func GetWaffoPayMethods() []constant.WaffoPayMethod {
	return GetWaffoConfig().PayMethods
}

// WaffoPayMethods2JsonString serializes the current immutable snapshot.
func WaffoPayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(GetWaffoConfig().PayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}
