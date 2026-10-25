/**
此文件为旧版支付设置文件，如需增加新的参数、变量等，请在 payment_setting.go 中添加
This file is the old version of the payment settings file. If you need to add new parameters, variables, etc., please add them in payment_setting.go
*/

package operation_setting

import (
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"eflow.local/new-api/common"
)

type EpayConfig struct {
	PayAddress            string
	CustomCallbackAddress string
	EpayID                string
	EpayKey               string
	Price                 float64
	MinTopUp              int
	USDExchangeRate       float64
	PayMethods            []map[string]string
}

var (
	epayConfig   atomic.Pointer[EpayConfig]
	epayConfigMu sync.Mutex
)

func defaultEpayPayMethods() []map[string]string {
	return []map[string]string{
		{
			"name":  "支付宝",
			"color": "rgba(var(--semi-blue-5), 1)",
			"type":  "alipay",
		},
		{
			"name":  "微信",
			"color": "rgba(var(--semi-green-5), 1)",
			"type":  "wxpay",
		},
		{
			"name":      "自定义1",
			"color":     "black",
			"type":      "custom1",
			"min_topup": "50",
		},
	}
}

func init() {
	publishEpayConfig(EpayConfig{
		Price:           7.3,
		MinTopUp:        1,
		USDExchangeRate: 7.3,
		PayMethods:      defaultEpayPayMethods(),
	})
}

func GetEpayConfig() EpayConfig {
	current := epayConfig.Load()
	if current == nil {
		return EpayConfig{
			Price:           7.3,
			MinTopUp:        1,
			USDExchangeRate: 7.3,
			PayMethods:      clonePayMethods(defaultEpayPayMethods()),
		}
	}
	return cloneEpayConfig(*current)
}

// ValidateEpayOptionValues validates a partial option batch without
// publishing it. The database option path uses this before committing.
func ValidateEpayOptionValues(values map[string]string) error {
	_, _, err := applyEpayOptionValues(GetEpayConfig(), values)
	return err
}

func UpdateEpayConfig(values map[string]string) error {
	epayConfigMu.Lock()
	defer epayConfigMu.Unlock()

	next, touched, err := applyEpayOptionValues(GetEpayConfig(), values)
	if err != nil {
		return err
	}
	if touched {
		publishEpayConfig(next)
	}
	return nil
}

func applyEpayOptionValues(current EpayConfig, values map[string]string) (EpayConfig, bool, error) {
	next := cloneEpayConfig(current)
	touched := false
	for key, value := range values {
		switch key {
		case "PayAddress":
			next.PayAddress = value
			touched = true
		case "CustomCallbackAddress":
			next.CustomCallbackAddress = value
			touched = true
		case "EpayId":
			next.EpayID = value
			touched = true
		case "EpayKey":
			next.EpayKey = value
			touched = true
		case "Price":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return current, false, fmt.Errorf("Price must be a non-negative finite number")
			}
			next.Price = parsed
			touched = true
		case "MinTopUp":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return current, false, fmt.Errorf("MinTopUp must be at least 0")
			}
			next.MinTopUp = parsed
			touched = true
		case "USDExchangeRate":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return current, false, fmt.Errorf("USDExchangeRate must be a non-negative finite number")
			}
			next.USDExchangeRate = parsed
			touched = true
		case "PayMethods":
			methods, err := parsePayMethods(value)
			if err != nil {
				return current, false, err
			}
			next.PayMethods = methods
			touched = true
		}
	}
	return next, touched, nil
}

func parsePayMethods(value string) ([]map[string]string, error) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, fmt.Errorf("PayMethods must be a JSON array")
	}
	methods := make([]map[string]string, 0)
	if err := common.Unmarshal([]byte(value), &methods); err != nil {
		return nil, fmt.Errorf("PayMethods must be a valid JSON array: %w", err)
	}
	for i, method := range methods {
		if method == nil {
			return nil, fmt.Errorf("PayMethods[%d] must be an object", i)
		}
	}
	return methods, nil
}

func clonePayMethods(methods []map[string]string) []map[string]string {
	cloned := make([]map[string]string, len(methods))
	for i, method := range methods {
		if method == nil {
			continue
		}
		cloned[i] = make(map[string]string, len(method))
		maps.Copy(cloned[i], method)
	}
	return cloned
}

func cloneEpayConfig(config EpayConfig) EpayConfig {
	config.PayMethods = clonePayMethods(config.PayMethods)
	return config
}

func publishEpayConfig(next EpayConfig) {
	next = cloneEpayConfig(next)
	epayConfig.Store(&next)
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	return UpdateEpayConfig(map[string]string{"PayMethods": jsonString})
}

func PayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(GetEpayConfig().PayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	return ContainsPayMethodWithConfig(GetEpayConfig(), method)
}

func ContainsPayMethodWithConfig(config EpayConfig, method string) bool {
	for _, payMethod := range config.PayMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}
