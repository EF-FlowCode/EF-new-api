package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModerationMaskedKeysRequireUniqueCurrentMatch(t *testing.T) {
	first, second := "key-one-abcd", "key-two-abcd"
	masked := operation_setting.MaskModerationKey(first)
	keys, err := mergePreservedModerationKeys([]string{first}, []string{masked, "new-key"})
	require.NoError(t, err)
	require.Equal(t, []string{first, "new-key"}, keys)
	keys, err = mergePreservedModerationKeys([]string{first}, nil)
	require.NoError(t, err)
	require.Empty(t, keys)
	for _, existing := range [][]string{nil, {second, first}} {
		_, err := mergePreservedModerationKeys(existing, []string{masked})
		require.Error(t, err)
	}
	keys, err = mergePreservedModerationKeys([]string{"abcd"}, []string{"****"})
	require.NoError(t, err)
	require.Equal(t, []string{"abcd"}, keys)
}

func TestConfigurationHandlersRollBackSecondOptionWrite(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		get     func() any
		handler gin.HandlerFunc
	}{
		{"moderation", func() any { return operation_setting.GetModerationSetting() }, UpdateModerationConfig},
		{"enforcement", func() any { return operation_setting.GetEnforcementSetting() }, UpdateEnforcementConfig},
		{"risk_control", func() any { return operation_setting.GetRiskControlSetting() }, UpdateRiskCenterConfig},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := testdb.OpenMySQL8(t, &model.Option{})
			previous := model.DB
			model.DB = db
			t.Cleanup(func() { model.DB = previous })
			snapshot := scenario.get()
			values, err := config.ConfigToMap(snapshot)
			require.NoError(t, err)
			seed := make([]model.Option, 0, len(values))
			keys := make([]string, 0, len(values))
			for key, value := range values {
				seed = append(seed, model.Option{Key: scenario.name + "." + key, Value: value})
				keys = append(keys, key)
			}
			require.NoError(t, db.Create(&seed).Error)
			sort.Strings(keys)
			encoded, err := json.Marshal(snapshot)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(encoded, &payload))
			first := keys[0]
			switch value := payload[first].(type) {
			case bool:
				payload[first] = !value
			case float64:
				payload[first] = value + 1
			case []any:
				payload[first] = append(value, "new-contract-key")
			case nil:
				payload[first] = []string{"new-contract-key"}
			default:
				t.Fatalf("unsupported first field %s", first)
			}
			if scenario.name == "moderation" {
				payload["preserve_existing_keys"] = false
			}
			encoded, err = json.Marshal(payload)
			require.NoError(t, err)
			writes := 0
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:second_option_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "options" {
					writes++
					if writes == 2 {
						tx.AddError(errors.New("second option write failed"))
					}
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Update().Remove("test:second_option_failure") })
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(string(encoded)))
			scenario.handler(context)
			require.Equal(t, 2, writes, "failure must occur after the first write")
			var result struct{ Success bool }
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			require.False(t, result.Success)
			var stored []model.Option
			require.NoError(t, db.Find(&stored).Error)
			for _, option := range stored {
				require.Equal(t, values[strings.TrimPrefix(option.Key, scenario.name+".")], option.Value)
			}
			require.Equal(t, snapshot, scenario.get(), "runtime snapshot must stay unchanged")
		})
	}
}
