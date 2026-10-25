package setting

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoGroupPolicyPublishesWholeVersionAndIsolatesGroups(t *testing.T) {
	original := GetAutoGroupPolicy()
	t.Cleanup(func() { publishAutoGroupPolicy(original) })

	require.NoError(t, UpdateAutoGroupPolicy(map[string]string{
		"AutoGroups":          `["vip","default"]`,
		"DefaultUseAutoGroup": "true",
	}))

	got := GetAutoGroupPolicy()
	require.Equal(t, []string{"vip", "default"}, got.Groups)
	require.True(t, got.DefaultUseAutoGroup)
	got.Groups[0] = "mutated"
	require.Equal(t, []string{"vip", "default"}, GetAutoGroupPolicy().Groups)
	require.Equal(t, []string{"vip", "default"}, GetAutoGroups())
}

func TestAutoGroupPolicyRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	original := GetAutoGroupPolicy()
	t.Cleanup(func() { publishAutoGroupPolicy(original) })

	require.Error(t, UpdateAutoGroupPolicy(map[string]string{
		"AutoGroups":          `["vip"]`,
		"DefaultUseAutoGroup": "not-a-bool",
	}))
	require.Equal(t, original, GetAutoGroupPolicy())
}

func TestAutoGroupPolicyConcurrentReadersObserveWholeVersions(t *testing.T) {
	original := GetAutoGroupPolicy()
	t.Cleanup(func() { publishAutoGroupPolicy(original) })

	versionA := map[string]string{"AutoGroups": `["a"]`, "DefaultUseAutoGroup": "false"}
	versionB := map[string]string{"AutoGroups": `["b","c"]`, "DefaultUseAutoGroup": "true"}
	require.NoError(t, UpdateAutoGroupPolicy(versionA))

	var invalid atomic.Bool
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(5)
	go func() {
		defer wg.Done()
		<-start
		for i := range 1000 {
			values := versionA
			if i%2 == 0 {
				values = versionB
			}
			if UpdateAutoGroupPolicy(values) != nil {
				invalid.Store(true)
				return
			}
		}
	}()
	for range 4 {
		go func() {
			defer wg.Done()
			<-start
			for range 2000 {
				policy := GetAutoGroupPolicy()
				isA := !policy.DefaultUseAutoGroup && reflect.DeepEqual(policy.Groups, []string{"a"})
				isB := policy.DefaultUseAutoGroup && reflect.DeepEqual(policy.Groups, []string{"b", "c"})
				if !isA && !isB {
					invalid.Store(true)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	require.False(t, invalid.Load())
}
