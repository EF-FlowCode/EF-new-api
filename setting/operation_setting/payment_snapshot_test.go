package operation_setting

import (
	"reflect"
	"strconv"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestEpayInvalidBatchKeepsPublishedSnapshot(t *testing.T) {
	preserveEpayConfig(t)
	require.NoError(t, UpdateEpayConfig(map[string]string{
		"PayAddress": "https://before.example.com", "Price": "7.3",
		"PayMethods": `[{"type":"alipay"}]`,
	}))
	before := GetEpayConfig()
	invalid := map[string]string{"PayAddress": "https://after.example.com", "PayMethods": `[null]`}

	require.Error(t, ValidateEpayOptionValues(invalid))
	require.Equal(t, before, GetEpayConfig())
	require.Error(t, UpdateEpayConfig(invalid))
	require.Equal(t, before, GetEpayConfig())
}

func TestEpayConfigGetterDeepCopiesPayMethods(t *testing.T) {
	preserveEpayConfig(t)
	require.NoError(t, UpdateEpayConfig(map[string]string{
		"PayMethods": `[{"type":"alipay","name":"original"}]`,
	}))

	config := GetEpayConfig()
	config.PayMethods[0]["name"] = "mutated"
	config.PayMethods = append(config.PayMethods, map[string]string{"type": "extra"})

	published := GetEpayConfig()
	require.Len(t, published.PayMethods, 1)
	require.Equal(t, "original", published.PayMethods[0]["name"])
}

func TestEpayConfigReadersObserveCompleteVersions(t *testing.T) {
	preserveEpayConfig(t)
	versionA := map[string]string{
		"PayAddress": "https://a.example.com", "CustomCallbackAddress": "https://callback-a.example.com",
		"EpayId": "id-a", "EpayKey": "key-a", "Price": "1", "MinTopUp": "1",
		"USDExchangeRate": "7", "PayMethods": `[{"type":"a","name":"A"}]`,
	}
	versionB := map[string]string{
		"PayAddress": "https://b.example.com", "CustomCallbackAddress": "https://callback-b.example.com",
		"EpayId": "id-b", "EpayKey": "key-b", "Price": "2", "MinTopUp": "2",
		"USDExchangeRate": "8", "PayMethods": `[{"type":"b","name":"B"}]`,
	}
	require.NoError(t, UpdateEpayConfig(versionA))
	wantA := GetEpayConfig()
	require.NoError(t, UpdateEpayConfig(versionB))
	wantB := GetEpayConfig()
	require.NoError(t, UpdateEpayConfig(versionA))

	const iterations = 500
	start := make(chan struct{})
	invalid := make(chan EpayConfig, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range iterations * 4 {
				got := GetEpayConfig()
				if !reflect.DeepEqual(got, wantA) && !reflect.DeepEqual(got, wantB) {
					select {
					case invalid <- got:
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for i := range iterations {
		values := versionA
		if i%2 == 0 {
			values = versionB
		}
		require.NoError(t, UpdateEpayConfig(values))
	}
	readers.Wait()
	select {
	case got := <-invalid:
		t.Fatalf("reader observed mixed Epay config: %#v", got)
	default:
	}
}

func preserveEpayConfig(t *testing.T) {
	t.Helper()
	original := GetEpayConfig()
	t.Cleanup(func() { require.NoError(t, UpdateEpayConfig(epaySnapshotOptionValues(t, original))) })
}

func epaySnapshotOptionValues(t *testing.T, config EpayConfig) map[string]string {
	t.Helper()
	methods, err := common.Marshal(config.PayMethods)
	require.NoError(t, err)
	return map[string]string{
		"PayAddress": config.PayAddress, "CustomCallbackAddress": config.CustomCallbackAddress,
		"EpayId": config.EpayID, "EpayKey": config.EpayKey,
		"Price": strconv.FormatFloat(config.Price, 'f', -1, 64), "MinTopUp": strconv.Itoa(config.MinTopUp),
		"USDExchangeRate": strconv.FormatFloat(config.USDExchangeRate, 'f', -1, 64), "PayMethods": string(methods),
	}
}
