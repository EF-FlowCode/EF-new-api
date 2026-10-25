package operation_setting

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelFailurePolicyPublishesWholeVersionAndIsolatesSlices(t *testing.T) {
	original := GetChannelFailurePolicy()
	t.Cleanup(func() { publishChannelFailurePolicy(original) })

	require.NoError(t, UpdateChannelFailurePolicy(map[string]string{
		"AutomaticDisableChannelEnabled": "true",
		"AutomaticEnableChannelEnabled":  "true",
		"ChannelDisableThreshold":        "2.5",
		"AutomaticDisableKeywords":       " First \nSECOND\n",
		"AutomaticDisableStatusCodes":    "401-403,500",
		"RetryTimes":                     "3",
		"AutomaticRetryStatusCodes":      "429,500-503",
	}))

	got := GetChannelFailurePolicy()
	require.True(t, got.AutomaticDisableChannelEnabled)
	require.True(t, got.AutomaticEnableChannelEnabled)
	require.Equal(t, 2.5, got.ChannelDisableThreshold)
	require.Equal(t, []string{"first", "second"}, got.AutomaticDisableKeywords)
	require.Equal(t, []StatusCodeRange{{Start: 401, End: 403}, {Start: 500, End: 500}}, got.AutomaticDisableStatusCodes)
	require.Equal(t, 3, got.RetryTimes)
	require.Equal(t, []StatusCodeRange{{Start: 429, End: 429}, {Start: 500, End: 503}}, got.AutomaticRetryStatusCodes)

	got.AutomaticDisableKeywords[0] = "mutated"
	got.AutomaticDisableStatusCodes[0].Start = 200
	got.AutomaticRetryStatusCodes[0].Start = 200
	isolated := GetChannelFailurePolicy()
	require.Equal(t, "first", isolated.AutomaticDisableKeywords[0])
	require.Equal(t, 401, isolated.AutomaticDisableStatusCodes[0].Start)
	require.Equal(t, 429, isolated.AutomaticRetryStatusCodes[0].Start)
}

func TestChannelFailurePolicyRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	original := GetChannelFailurePolicy()
	t.Cleanup(func() { publishChannelFailurePolicy(original) })

	require.Error(t, UpdateChannelFailurePolicy(map[string]string{
		"AutomaticDisableChannelEnabled": "true",
		"RetryTimes":                     "11",
	}))
	require.Equal(t, original, GetChannelFailurePolicy())
}

func TestChannelFailurePolicyConcurrentReadersObserveWholeVersions(t *testing.T) {
	original := GetChannelFailurePolicy()
	t.Cleanup(func() { publishChannelFailurePolicy(original) })

	versionA := map[string]string{
		"AutomaticDisableChannelEnabled": "true",
		"AutomaticEnableChannelEnabled":  "false",
		"ChannelDisableThreshold":        "1",
		"AutomaticDisableKeywords":       "alpha",
		"AutomaticDisableStatusCodes":    "401",
		"RetryTimes":                     "1",
		"AutomaticRetryStatusCodes":      "429",
	}
	versionB := map[string]string{
		"AutomaticDisableChannelEnabled": "false",
		"AutomaticEnableChannelEnabled":  "true",
		"ChannelDisableThreshold":        "9",
		"AutomaticDisableKeywords":       "beta\ngamma",
		"AutomaticDisableStatusCodes":    "500-501",
		"RetryTimes":                     "9",
		"AutomaticRetryStatusCodes":      "502-503",
	}
	require.NoError(t, UpdateChannelFailurePolicy(versionA))

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
			if UpdateChannelFailurePolicy(values) != nil {
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
				policy := GetChannelFailurePolicy()
				if !isChannelFailurePolicyVersionA(policy) && !isChannelFailurePolicyVersionB(policy) {
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

func isChannelFailurePolicyVersionA(policy ChannelFailurePolicy) bool {
	return policy.AutomaticDisableChannelEnabled &&
		!policy.AutomaticEnableChannelEnabled &&
		policy.ChannelDisableThreshold == 1 &&
		reflect.DeepEqual(policy.AutomaticDisableKeywords, []string{"alpha"}) &&
		reflect.DeepEqual(policy.AutomaticDisableStatusCodes, []StatusCodeRange{{Start: 401, End: 401}}) &&
		policy.RetryTimes == 1 &&
		reflect.DeepEqual(policy.AutomaticRetryStatusCodes, []StatusCodeRange{{Start: 429, End: 429}})
}

func isChannelFailurePolicyVersionB(policy ChannelFailurePolicy) bool {
	return !policy.AutomaticDisableChannelEnabled &&
		policy.AutomaticEnableChannelEnabled &&
		policy.ChannelDisableThreshold == 9 &&
		reflect.DeepEqual(policy.AutomaticDisableKeywords, []string{"beta", "gamma"}) &&
		reflect.DeepEqual(policy.AutomaticDisableStatusCodes, []StatusCodeRange{{Start: 500, End: 501}}) &&
		policy.RetryTimes == 9 &&
		reflect.DeepEqual(policy.AutomaticRetryStatusCodes, []StatusCodeRange{{Start: 502, End: 503}})
}
