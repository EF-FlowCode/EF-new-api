package setting

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

// Waffo Pancake hosted checkout configuration. Gateway is enabled once
// MerchantID + PrivateKey + ProductID are populated (no separate Enabled
// flag, matching Stripe / Creem). StoreID + ProductID are operator-bound
// via SaveWaffoPancakeConfig.
type WaffoPancakeConfig struct {
	MerchantID string
	PrivateKey string
	ReturnURL  string
	UnitPrice  float64
	MinTopUp   int
	StoreID    string
	ProductID  string
}

var (
	waffoPancakeConfig   atomic.Pointer[WaffoPancakeConfig]
	waffoPancakeConfigMu sync.Mutex
)

func init() {
	publishWaffoPancakeConfig(WaffoPancakeConfig{
		UnitPrice: 1,
		MinTopUp:  1,
	})
}

// GetWaffoPancakeConfig returns one immutable runtime version. Callers that
// need multiple fields should retain this value for the entire operation.
func GetWaffoPancakeConfig() WaffoPancakeConfig {
	current := waffoPancakeConfig.Load()
	if current == nil {
		return WaffoPancakeConfig{UnitPrice: 1, MinTopUp: 1}
	}
	return *current
}

// UpdateWaffoPancakeConfig parses every supplied field before publishing one
// complete replacement. Unknown option keys are ignored.
func UpdateWaffoPancakeConfig(values map[string]string) error {
	waffoPancakeConfigMu.Lock()
	defer waffoPancakeConfigMu.Unlock()

	next := GetWaffoPancakeConfig()
	for key, value := range values {
		switch key {
		case "WaffoPancakeMerchantID":
			next.MerchantID = value
		case "WaffoPancakePrivateKey":
			next.PrivateKey = value
		case "WaffoPancakeReturnURL":
			next.ReturnURL = value
		case "WaffoPancakeUnitPrice":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return fmt.Errorf("WaffoPancakeUnitPrice must be a non-negative number")
			}
			next.UnitPrice = parsed
		case "WaffoPancakeMinTopUp":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return fmt.Errorf("WaffoPancakeMinTopUp must be at least 0")
			}
			next.MinTopUp = parsed
		case "WaffoPancakeStoreID":
			next.StoreID = value
		case "WaffoPancakeProductID":
			next.ProductID = value
		}
	}
	publishWaffoPancakeConfig(next)
	return nil
}

func publishWaffoPancakeConfig(next WaffoPancakeConfig) {
	waffoPancakeConfig.Store(&next)
}
