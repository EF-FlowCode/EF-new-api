package model

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddNewRecordSaturatesAccumulatorOverflow(t *testing.T) {
	storeType := BatchUpdateTypeRequestCount
	setStore := func(store map[int]int) {
		batchUpdateLocks[storeType].Lock()
		defer batchUpdateLocks[storeType].Unlock()
		batchUpdateStores[storeType] = store
	}
	getValue := func(id int) int {
		batchUpdateLocks[storeType].Lock()
		defer batchUpdateLocks[storeType].Unlock()
		return batchUpdateStores[storeType][id]
	}

	batchUpdateLocks[storeType].Lock()
	previous := batchUpdateStores[storeType]
	batchUpdateStores[storeType] = make(map[int]int)
	batchUpdateLocks[storeType].Unlock()
	t.Cleanup(func() {
		batchUpdateLocks[storeType].Lock()
		batchUpdateStores[storeType] = previous
		batchUpdateLocks[storeType].Unlock()
	})

	addNewRecord(storeType, 1, math.MaxInt)
	addNewRecord(storeType, 1, 1)
	require.Equal(t, math.MaxInt, getValue(1))

	setStore(make(map[int]int))
	addNewRecord(storeType, 1, math.MinInt)
	addNewRecord(storeType, 1, -1)
	require.Equal(t, math.MinInt, getValue(1))
}
