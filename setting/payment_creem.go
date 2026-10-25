package setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"eflow.local/new-api/common"
)

type CreemProduct struct {
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

// CreemConfig is one immutable runtime version of the Creem settings.
type CreemConfig struct {
	APIKey        string
	ProductsJSON  string
	Products      []CreemProduct
	TestMode      bool
	WebhookSecret string
}

var (
	creemConfig   atomic.Pointer[CreemConfig]
	creemConfigMu sync.Mutex
)

func init() {
	publishCreemConfig(CreemConfig{
		ProductsJSON: "[]",
		Products:     []CreemProduct{},
	})
}

func GetCreemConfig() CreemConfig {
	current := creemConfig.Load()
	if current == nil {
		return CreemConfig{ProductsJSON: "[]", Products: []CreemProduct{}}
	}
	return cloneCreemConfig(*current)
}

// ValidateCreemOptionValues validates a partial option batch without
// publishing it. The database option path uses this before committing.
func ValidateCreemOptionValues(values map[string]string) error {
	_, _, err := applyCreemOptionValues(GetCreemConfig(), values)
	return err
}

func UpdateCreemConfig(values map[string]string) error {
	creemConfigMu.Lock()
	defer creemConfigMu.Unlock()

	next, touched, err := applyCreemOptionValues(GetCreemConfig(), values)
	if err != nil {
		return err
	}
	if touched {
		publishCreemConfig(next)
	}
	return nil
}

func applyCreemOptionValues(current CreemConfig, values map[string]string) (CreemConfig, bool, error) {
	next := cloneCreemConfig(current)
	touched := false
	for key, value := range values {
		switch key {
		case "CreemApiKey":
			next.APIKey = value
			touched = true
		case "CreemProducts":
			products, err := parseCreemProducts(value)
			if err != nil {
				return current, false, err
			}
			next.ProductsJSON = value
			next.Products = products
			touched = true
		case "CreemTestMode":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return current, false, fmt.Errorf("CreemTestMode must be a boolean: %w", err)
			}
			next.TestMode = parsed
			touched = true
		case "CreemWebhookSecret":
			next.WebhookSecret = value
			touched = true
		}
	}
	return next, touched, nil
}

func parseCreemProducts(value string) ([]CreemProduct, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []CreemProduct{}, nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil, fmt.Errorf("CreemProducts must be a JSON array")
	}
	products := make([]CreemProduct, 0)
	if err := common.Unmarshal([]byte(value), &products); err != nil {
		return nil, fmt.Errorf("CreemProducts must be a valid JSON array: %w", err)
	}
	for i, product := range products {
		if strings.TrimSpace(product.ProductID) == "" {
			return nil, fmt.Errorf("CreemProducts[%d].productId must not be empty", i)
		}
		if math.IsNaN(product.Price) || math.IsInf(product.Price, 0) || product.Price < 0 {
			return nil, fmt.Errorf("CreemProducts[%d].price must be a non-negative finite number", i)
		}
		if product.Quota < 0 {
			return nil, fmt.Errorf("CreemProducts[%d].quota must be at least 0", i)
		}
	}
	return products, nil
}

func cloneCreemConfig(config CreemConfig) CreemConfig {
	config.Products = append([]CreemProduct(nil), config.Products...)
	return config
}

func publishCreemConfig(next CreemConfig) {
	next = cloneCreemConfig(next)
	creemConfig.Store(&next)
}
