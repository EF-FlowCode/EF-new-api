package service

import (
	"errors"
	"math"
	"sort"
	"strings"

	"eflow.local/new-api/setting/billing_setting"
)

// TaskPluginProviderRatios preserves the site's existing unit conventions.
// Provider facts named units/credits/clips are not a price configuration.
func TaskPluginProviderRatios(key, name string, facts map[string]any) (map[string]float64, error) {
	switch key {
	case "alibaba", "happyhorse":
		return TaskPluginBillingRatios(name, facts)
	case "google", "vertex-ai":
		seconds, ok := facts["seconds"].(float64)
		if !ok || seconds <= 0 || seconds > 3600 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return nil, errors.New("invalid Veo duration")
		}
		ratio := 1.0
		if resolution, _ := facts["resolution"].(string); strings.EqualFold(resolution, "4k") {
			if strings.Contains(name, "3.1-fast-generate") {
				ratio = 2.333333
			} else if strings.Contains(name, "3.1") {
				ratio = 1.5
			}
		}
		return map[string]float64{"seconds": seconds, "resolution": ratio}, nil
	case "sora":
		seconds, ok := facts["seconds"].(float64)
		if !ok || seconds <= 0 || seconds > 3600 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return nil, errors.New("invalid Sora duration or missing origin usage")
		}
		size, _ := facts["size"].(string)
		if size != "720x1280" && size != "1280x720" && size != "1792x1024" && size != "1024x1792" {
			return nil, errors.New("invalid Sora size or missing origin usage")
		}
		ratio := 1.0
		if facts["size"] == "1792x1024" || facts["size"] == "1024x1792" {
			ratio = 1.666667
		}
		return map[string]float64{"seconds": seconds, "size": ratio}, nil
	case "doubao":
		resolution, _ := facts["resolution"].(string)
		return map[string]float64{"video_input": billing_setting.TaskVideoInputRatio(name, resolution, facts["video_input"] == "video")}, nil
	case "hailuo", "jimeng", "kling", "vidu", "sunoapi":
		return map[string]float64{}, nil // existing per-call reservation
	default:
		return nil, errors.New("unsupported task billing contract")
	}
}

// TaskPluginBillingRatios turns declared facts into local price dimensions.
// Scripts never supply monetary values. This helper implements the local
// WAN/Happyhorse duration contract; other providers use their own mapping.
func TaskPluginBillingRatios(modelName string, facts map[string]any) (map[string]float64, error) {
	seconds, ok := facts["seconds"].(float64)
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 3600 {
		return nil, errors.New("valid video seconds are required for local pricing")
	}
	resolution, ok := facts["resolution"].(string)
	if !ok || resolution == "" {
		return nil, errors.New("video resolution is required for local pricing")
	}
	ratios := map[string]float64{"seconds": seconds}
	if ratio, exists := billing_setting.GetTaskVideoResolutionRatio(modelName, resolution); exists {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 {
			return nil, errors.New("invalid local dimensional price")
		}
		if ratio != 1 {
			ratios["resolution-"+resolution] = ratio
		}
	}
	return ratios, nil
}

func TaskPluginQuota(base int, ratios map[string]float64) (int, error) {
	value := float64(base)
	keys := make([]string, 0, len(ratios))
	for key := range ratios {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ratio := ratios[key]
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 {
			return 0, errors.New("invalid plugin billing ratio")
		}
		value *= ratio
	}
	if base < 0 || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= float64(int(^uint(0)>>1)) {
		return 0, errors.New("plugin quota exceeds local bounds")
	}
	return int(value), nil
}
