package service

import (
	"fmt"
	"math"
	"sort"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/types"
)

// NewTaskPluginTokenBilling captures the same price lookup and selected group
// used for reservation. It must not read live settings again after that lookup.
func NewTaskPluginTokenBilling(price types.PriceData, patchedFixed bool) (*model.TaskPluginTokenBilling, error) {
	snapshot := &model.TaskPluginTokenBilling{Version: 1, Mode: "reserved"}
	if price.TaskFixedTotal || price.UsePrice || patchedFixed {
		return nil, nil
	}
	if !price.TaskModelRatioConfigured || price.ModelRatio == 0 {
		return snapshot, nil
	}
	snapshot.Mode = "tokens"
	snapshot.ModelRatio = price.ModelRatio
	groupRatio := price.GroupRatioInfo.GroupRatio
	snapshot.GroupRatio = &groupRatio
	snapshot.OtherMultiplier = 1
	keys := make([]string, 0, len(price.OtherRatios))
	for key := range price.OtherRatios {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ratio := price.OtherRatios[key]
		if ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return nil, fmt.Errorf("invalid task token price multiplier %s", key)
		}
		snapshot.OtherMultiplier *= ratio
	}
	if err := validateTaskPluginTokenBilling(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func validateTaskPluginTokenBilling(snapshot *model.TaskPluginTokenBilling) error {
	if snapshot.Version != 1 {
		return fmt.Errorf("unsupported task token price snapshot version %d", snapshot.Version)
	}
	if snapshot.Mode == "reserved" {
		return nil
	}
	if snapshot.Mode != "tokens" || snapshot.GroupRatio == nil {
		return fmt.Errorf("incomplete task token price snapshot")
	}
	for _, value := range []float64{snapshot.ModelRatio, *snapshot.GroupRatio, snapshot.OtherMultiplier} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("invalid task token price snapshot")
		}
	}
	if snapshot.ModelRatio == 0 || snapshot.OtherMultiplier == 0 {
		return fmt.Errorf("incomplete task token price snapshot")
	}
	return nil
}

// A missing snapshot identifies an old task and retains its former calculation.
// A present but disabled/invalid snapshot never falls back to current prices.
func taskCompletionTokenQuota(task *model.Task, totalTokens int) (int, bool, error) {
	if pc := task.PrivateData.PluginContext; pc != nil && pc.TokenBilling != nil {
		snapshot := pc.TokenBilling
		if err := validateTaskPluginTokenBilling(snapshot); err != nil {
			return 0, false, err
		}
		if totalTokens < 0 {
			return 0, false, fmt.Errorf("negative completion token count")
		}
		if snapshot.Mode == "reserved" || totalTokens == 0 {
			return 0, false, nil
		}
		// Preserve legacy multiplication order and final integer truncation.
		quota, err := common.QuotaFromFloatStrict(float64(totalTokens) * snapshot.ModelRatio * *snapshot.GroupRatio * snapshot.OtherMultiplier)
		return quota, err == nil, err
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		return 0, false, nil
	}
	quota, ok := calculateTaskQuotaByTokens(task, totalTokens)
	return quota, ok, nil
}
