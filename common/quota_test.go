package common

import (
	"math"
	"sync"
	"testing"
)

func TestQuotaPerUnitDefaultValue(t *testing.T) {
	if got := GetQuotaPerUnit(); got != 500_000 {
		t.Fatalf("default quota per unit = %v, want 500000", got)
	}
}

func TestSetQuotaPerUnitRejectsInvalidValuesWithoutPublishing(t *testing.T) {
	previous := GetQuotaPerUnit()
	t.Cleanup(func() {
		if err := SetQuotaPerUnit(previous); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	const baseline = 123_456.75
	if err := SetQuotaPerUnit(baseline); err != nil {
		t.Fatalf("set baseline: %v", err)
	}

	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := SetQuotaPerUnit(value); err == nil {
			t.Fatalf("SetQuotaPerUnit(%v) unexpectedly succeeded", value)
		}
		if got := GetQuotaPerUnit(); got != baseline {
			t.Fatalf("invalid value %v changed quota per unit to %v", value, got)
		}
	}
}

func TestQuotaPerUnitConcurrentReadersObserveCompletePublications(t *testing.T) {
	previous := GetQuotaPerUnit()
	t.Cleanup(func() {
		if err := SetQuotaPerUnit(previous); err != nil {
			t.Fatalf("restore quota per unit: %v", err)
		}
	})

	const (
		valueA     = 111_111.25
		valueB     = 999_999.75
		iterations = 10_000
	)
	if err := SetQuotaPerUnit(valueA); err != nil {
		t.Fatalf("set initial value: %v", err)
	}

	start := make(chan struct{})
	unexpected := make(chan float64, 1)
	var workers sync.WaitGroup
	for writer := range 4 {
		workers.Add(1)
		go func(offset int) {
			defer workers.Done()
			<-start
			for i := range iterations {
				value := valueA
				if (i+offset)%2 == 1 {
					value = valueB
				}
				if err := SetQuotaPerUnit(value); err != nil {
					select {
					case unexpected <- value:
					default:
					}
					return
				}
			}
		}(writer)
	}
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				value := GetQuotaPerUnit()
				if value != valueA && value != valueB {
					select {
					case unexpected <- value:
					default:
					}
					return
				}
			}
		}()
	}

	close(start)
	workers.Wait()
	select {
	case value := <-unexpected:
		t.Fatalf("observed unpublished quota per unit value %v", value)
	default:
	}
}
