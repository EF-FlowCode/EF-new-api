package service

import (
	"encoding/json"
	"math"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestPluginTokenPriceSnapshotArithmetic(t *testing.T) {
	for _, group := range []float64{0, 0.13, 1, 3.5} {
		price := types.PriceData{ModelRatio: 0.037, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: group}, OtherRatios: map[string]float64{"resolution": 1.778, "seconds": 5}}
		snapshot, err := NewTaskPluginTokenBilling(price, false)
		require.NoError(t, err)
		task := &model.Task{PrivateData: model.TaskPrivateData{PluginContext: &model.TaskPluginContext{TokenBilling: snapshot}}}
		raw, err := json.Marshal(task.PrivateData)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &task.PrivateData))
		price.OtherRatios["seconds"] = 999
		for _, tokens := range []int{1, 17, 1000} {
			quota, ok, err := taskCompletionTokenQuota(task, tokens)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, int(float64(tokens)*0.037*group*(1.778*5)), quota)
		}
		_, ok, err := taskCompletionTokenQuota(task, 0)
		require.NoError(t, err)
		require.False(t, ok)
	}
	for _, mode := range []string{"price", "expression", "patch", "unset", "zero"} {
		price := types.PriceData{ModelRatio: 0.5, TaskModelRatioConfigured: true}
		switch mode {
		case "price":
			price.UsePrice = true
		case "expression":
			price.TaskFixedTotal = true
		case "unset":
			price.TaskModelRatioConfigured = false
		case "zero":
			price.ModelRatio = 0
		}
		snapshot, err := NewTaskPluginTokenBilling(price, mode == "patch")
		require.NoError(t, err)
		if mode == "unset" || mode == "zero" {
			require.Equal(t, "reserved", snapshot.Mode)
		} else {
			require.Nil(t, snapshot)
		}
	}
}

func TestPluginTokenPriceSnapshotRejectsInvalid(t *testing.T) {
	one := 1.0
	for _, snapshot := range []*model.TaskPluginTokenBilling{
		{Version: 2, Mode: "tokens"}, {Version: 1}, {Version: 1, Mode: "tokens", ModelRatio: 1, OtherMultiplier: 1},
		{Version: 1, Mode: "tokens", ModelRatio: -1, GroupRatio: &one, OtherMultiplier: 1},
		{Version: 1, Mode: "tokens", ModelRatio: math.NaN(), GroupRatio: &one, OtherMultiplier: 1},
		{Version: 1, Mode: "tokens", ModelRatio: 1, GroupRatio: &one, OtherMultiplier: math.Inf(1)},
		{Version: 1, Mode: "tokens", ModelRatio: common.MaxQuota, GroupRatio: &one, OtherMultiplier: 1},
	} {
		task := &model.Task{PrivateData: model.TaskPrivateData{PluginContext: &model.TaskPluginContext{TokenBilling: snapshot}}}
		_, ok, err := taskCompletionTokenQuota(task, 1000)
		require.Error(t, err)
		require.False(t, ok)
	}
	for _, ratio := range []float64{-1, 0, math.NaN(), math.Inf(1)} {
		_, err := NewTaskPluginTokenBilling(types.PriceData{ModelRatio: 1, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}, OtherRatios: map[string]float64{"bad": ratio}}, false)
		require.Error(t, err)
	}
}
