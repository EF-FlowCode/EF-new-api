package setting

import (
	"strconv"
	"sync"
	"testing"
)

func TestUpdateWaffoPancakeConfigFailureKeepsPublishedSnapshot(t *testing.T) {
	original := GetWaffoPancakeConfig()
	t.Cleanup(func() {
		if err := UpdateWaffoPancakeConfig(waffoPancakeOptionValues(original)); err != nil {
			t.Fatalf("restore Waffo Pancake config: %v", err)
		}
	})

	before := WaffoPancakeConfig{
		MerchantID: "merchant-before",
		PrivateKey: "private-before",
		ReturnURL:  "https://before.example.com",
		UnitPrice:  1.25,
		MinTopUp:   2,
		StoreID:    "store-before",
		ProductID:  "product-before",
	}
	if err := UpdateWaffoPancakeConfig(waffoPancakeOptionValues(before)); err != nil {
		t.Fatalf("seed Waffo Pancake config: %v", err)
	}

	err := UpdateWaffoPancakeConfig(map[string]string{
		"WaffoPancakeMerchantID": "merchant-after",
		"WaffoPancakeUnitPrice":  "not-a-number",
	})
	if err == nil {
		t.Fatal("expected invalid unit price to fail")
	}
	if got := GetWaffoPancakeConfig(); got != before {
		t.Fatalf("failed update published a partial snapshot: got %+v, want %+v", got, before)
	}
}

func TestWaffoPancakeConfigReadersObserveWholeVersions(t *testing.T) {
	original := GetWaffoPancakeConfig()
	t.Cleanup(func() {
		if err := UpdateWaffoPancakeConfig(waffoPancakeOptionValues(original)); err != nil {
			t.Fatalf("restore Waffo Pancake config: %v", err)
		}
	})

	first := WaffoPancakeConfig{
		MerchantID: "merchant-a",
		PrivateKey: "private-a",
		ReturnURL:  "https://a.example.com",
		UnitPrice:  1,
		MinTopUp:   1,
		StoreID:    "store-a",
		ProductID:  "product-a",
	}
	second := WaffoPancakeConfig{
		MerchantID: "merchant-b",
		PrivateKey: "private-b",
		ReturnURL:  "https://b.example.com",
		UnitPrice:  2,
		MinTopUp:   2,
		StoreID:    "store-b",
		ProductID:  "product-b",
	}
	if err := UpdateWaffoPancakeConfig(waffoPancakeOptionValues(first)); err != nil {
		t.Fatalf("seed Waffo Pancake config: %v", err)
	}

	const iterations = 1000
	start := make(chan struct{})
	errCh := make(chan WaffoPancakeConfig, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range iterations {
				got := GetWaffoPancakeConfig()
				if got != first && got != second {
					select {
					case errCh <- got:
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for i := range iterations {
		next := first
		if i%2 == 0 {
			next = second
		}
		if err := UpdateWaffoPancakeConfig(waffoPancakeOptionValues(next)); err != nil {
			t.Fatalf("publish Waffo Pancake config: %v", err)
		}
	}
	readers.Wait()
	select {
	case torn := <-errCh:
		t.Fatalf("reader observed a torn snapshot: %+v", torn)
	default:
	}
}

func waffoPancakeOptionValues(config WaffoPancakeConfig) map[string]string {
	return map[string]string{
		"WaffoPancakeMerchantID": config.MerchantID,
		"WaffoPancakePrivateKey": config.PrivateKey,
		"WaffoPancakeReturnURL":  config.ReturnURL,
		"WaffoPancakeUnitPrice":  strconv.FormatFloat(config.UnitPrice, 'f', -1, 64),
		"WaffoPancakeMinTopUp":   strconv.Itoa(config.MinTopUp),
		"WaffoPancakeStoreID":    config.StoreID,
		"WaffoPancakeProductID":  config.ProductID,
	}
}
