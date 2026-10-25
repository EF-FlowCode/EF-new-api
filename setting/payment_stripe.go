package setting

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

// StripeConfig is one immutable runtime version of the Stripe settings.
type StripeConfig struct {
	APISecret             string
	WebhookSecret         string
	PriceID               string
	UnitPrice             float64
	MinTopUp              int
	PromotionCodesEnabled bool
}

var (
	stripeConfig   atomic.Pointer[StripeConfig]
	stripeConfigMu sync.Mutex
)

func init() {
	publishStripeConfig(StripeConfig{
		UnitPrice: 8,
		MinTopUp:  1,
	})
}

// GetStripeConfig returns one immutable runtime version. Callers using more
// than one field should retain this value for the entire operation.
func GetStripeConfig() StripeConfig {
	current := stripeConfig.Load()
	if current == nil {
		return StripeConfig{UnitPrice: 8, MinTopUp: 1}
	}
	return *current
}

// ValidateStripeOptionValues validates a partial option batch without
// publishing it. The database option path uses this before committing.
func ValidateStripeOptionValues(values map[string]string) error {
	_, _, err := applyStripeOptionValues(GetStripeConfig(), values)
	return err
}

// UpdateStripeConfig validates every supplied field before publishing one
// complete replacement. Unknown option keys are ignored.
func UpdateStripeConfig(values map[string]string) error {
	stripeConfigMu.Lock()
	defer stripeConfigMu.Unlock()

	next, touched, err := applyStripeOptionValues(GetStripeConfig(), values)
	if err != nil {
		return err
	}
	if touched {
		publishStripeConfig(next)
	}
	return nil
}

func applyStripeOptionValues(current StripeConfig, values map[string]string) (StripeConfig, bool, error) {
	next := current
	touched := false
	for key, value := range values {
		switch key {
		case "StripeApiSecret":
			next.APISecret = value
			touched = true
		case "StripeWebhookSecret":
			next.WebhookSecret = value
			touched = true
		case "StripePriceId":
			next.PriceID = value
			touched = true
		case "StripeUnitPrice":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
				return current, false, fmt.Errorf("StripeUnitPrice must be a non-negative finite number")
			}
			next.UnitPrice = parsed
			touched = true
		case "StripeMinTopUp":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return current, false, fmt.Errorf("StripeMinTopUp must be at least 0")
			}
			next.MinTopUp = parsed
			touched = true
		case "StripePromotionCodesEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return current, false, fmt.Errorf("StripePromotionCodesEnabled must be a boolean: %w", err)
			}
			next.PromotionCodesEnabled = parsed
			touched = true
		}
	}
	return next, touched, nil
}

func publishStripeConfig(next StripeConfig) {
	stripeConfig.Store(&next)
}
