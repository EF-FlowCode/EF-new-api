package billing_setting

import "strings"

// These are the existing local WAN resolution multipliers, moved out of the
// retired protocol adaptor. Scripts supply facts, never prices.
var wanVideoResolutionRatios = map[string]map[string]float64{
	"wan3.0-video":       {"480P": 1, "720P": 2, "1080P": 4},
	"wan3.0-video-prime": {"480P": 1, "720P": 2, "1080P": 4},
	"wan2.6-i2v":         {"720P": 1, "1080P": 1 / 0.6},
	"wan2.5-t2v-preview": {"480P": 1, "720P": 2, "1080P": 1 / 0.3},
	"wan2.2-t2v-plus":    {"480P": 1, "1080P": 0.7 / 0.14},
	"wan2.5-i2v-preview": {"480P": 1, "720P": 2, "1080P": 1 / 0.3},
	"wan2.2-i2v-plus":    {"480P": 1, "1080P": 0.7 / 0.14},
	"wan2.2-kf2v-flash":  {"480P": 1, "720P": 2, "1080P": 4.8},
	"wan2.2-i2v-flash":   {"480P": 1, "720P": 2},
	"wan2.2-s2v":         {"480P": 1, "720P": 0.9 / 0.5},
}

func GetTaskVideoResolutionRatio(model, resolution string) (float64, bool) {
	if ratio, ok := GetDimensionalTierRatio(model, resolution); ok {
		return ratio, true
	}
	ratio, ok := wanVideoResolutionRatios[model][strings.ToUpper(strings.TrimSpace(resolution))]
	return ratio, ok
}

// Relative prices retained from the local Doubao task adaptor.
func TaskVideoInputRatio(model, resolution string, video bool) float64 {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	if model == "doubao-seedance-2-0-fast-260128" {
		if video && resolution != "1080p" && resolution != "4k" {
			return 22.0 / 37.0
		}
		return 1
	}
	if model != "doubao-seedance-2-0-260128" {
		return 1
	}
	if resolution == "4k" {
		if video {
			return 16.0 / 46.0
		}
		return 26.0 / 46.0
	}
	if resolution == "1080p" {
		if video {
			return 31.0 / 46.0
		}
		return 51.0 / 46.0
	}
	if video {
		return 28.0 / 46.0
	}
	return 1
}
