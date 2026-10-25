package common

import (
	"errors"
	"math"
	"sync/atomic"
)

const defaultQuotaPerUnit = 500_000.0

var quotaPerUnitBits atomic.Uint64

func init() {
	quotaPerUnitBits.Store(math.Float64bits(defaultQuotaPerUnit))
}

// GetQuotaPerUnit returns the latest completely published conversion rate.
func GetQuotaPerUnit() float64 {
	return math.Float64frombits(quotaPerUnitBits.Load())
}

// SetQuotaPerUnit publishes a positive finite conversion rate atomically.
// Invalid values leave the previously published rate unchanged.
func SetQuotaPerUnit(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return errors.New("quota per unit must be a positive finite number")
	}
	quotaPerUnitBits.Store(math.Float64bits(value))
	return nil
}

func GetTrustQuota() int {
	return int(10 * GetQuotaPerUnit())
}
