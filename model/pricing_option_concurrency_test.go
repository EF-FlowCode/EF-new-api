package model

import (
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/ratio_setting"
)

// A pricing refresh holds updatePricingLock while reading model context windows.
// A concurrent option publication must not keep the option map locked while it
// waits for that same refresh to finish before invalidating the pricing cache.
func TestPricingRefreshCanReadOptionsDuringConcurrentPublication(t *testing.T) {
	const modelName = "pricing-lock-regression-model"
	const updatedRatio = `{"pricing-lock-regression-model":7}`
	previousRatios := ratio_setting.ModelRatio2JSONString()
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{ModelContextWindowsOption: `{"default":128000}`}
	common.OptionMapRWMutex.Unlock()
	previousPricing, previousVendors, previousTime := pricingMap, vendorsList, lastGetPricingTime
	t.Cleanup(func() {
		if err := ratio_setting.UpdateModelRatioByJSONString(previousRatios); err != nil {
			t.Errorf("restore model ratios: %v", err)
		}
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		updatePricingLock.Lock()
		pricingMap, vendorsList, lastGetPricingTime = previousPricing, previousVendors, previousTime
		updatePricingLock.Unlock()
	})

	updatePricingLock.Lock()
	pricingMap = []Pricing{{ModelName: modelName, ModelRatio: 1}}
	vendorsList = []PricingVendor{{ID: 1, Name: "cached"}}
	lastGetPricingTime = time.Now()
	publication := make(chan error, 1)
	go func() { publication <- updateOptionMap("ModelRatio", updatedRatio) }()

	// Observe the real setter's publication, rather than assuming the writer has
	// reached the cache invalidation after an arbitrary scheduling delay.
	deadline := time.Now().Add(3 * time.Second)
	for {
		ratio, found, _ := ratio_setting.GetModelRatio(modelName)
		if found && ratio == 7 {
			break
		}
		if time.Now().After(deadline) {
			updatePricingLock.Unlock()
			<-publication
			t.Fatal("option setter did not publish the new ratio")
		}
		time.Sleep(time.Millisecond)
	}

	window := make(chan int64, 1)
	go func() { window <- GetModelContextWindow(modelName) }()
	blocked := false
	select {
	case got := <-window:
		if got != 128000 {
			t.Errorf("context window = %d, want 128000", got)
		}
	case <-time.After(500 * time.Millisecond):
		blocked = true
	}
	// Always release the simulated refresh, including on the unfixed code, so a
	// regression fails cleanly instead of leaving blocked goroutines behind.
	updatePricingLock.Unlock()
	if err := <-publication; err != nil {
		t.Fatalf("publish model ratio: %v", err)
	}
	if blocked {
		<-window
		t.Error("pricing refresh cannot read options while publication waits for the pricing lock: lock-order deadlock")
	}
	if len(pricingMap) != 0 || len(vendorsList) != 0 || !lastGetPricingTime.IsZero() {
		t.Error("successful publication must invalidate the old pricing and vendor cache")
	}
	common.OptionMapRWMutex.RLock()
	got := common.OptionMap["ModelRatio"]
	common.OptionMapRWMutex.RUnlock()
	if got != updatedRatio {
		t.Errorf("published option = %q, want %q", got, updatedRatio)
	}
}
