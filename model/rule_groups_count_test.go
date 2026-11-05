package model

import (
	"testing"

	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
)

// groups 是 MySQL 8 保留字；未配置分组的计数必须在真实 MySQL 上能执行。
func TestCountEnabledRulesWithoutGroupsOnMySQL8(t *testing.T) {
	oldDB := DB
	DB = testdb.OpenMySQL8(t, &ModerationRule{}, &RiskRule{})
	t.Cleanup(func() { DB = oldDB })

	for i, groups := range []string{"", "[]", `["vip"]`} {
		require.NoError(t, DB.Create(&ModerationRule{Name: "m" + string(rune('a'+i)), Enabled: true, Groups: groups}).Error)
		require.NoError(t, DB.Create(&RiskRule{Name: "r" + string(rune('a'+i)), Enabled: true, Groups: groups}).Error)
	}
	require.NoError(t, DB.Create(&ModerationRule{Name: "m-off", Groups: ""}).Error)

	moderation, err := CountEnabledModerationRulesWithoutGroups()
	require.NoError(t, err)
	require.EqualValues(t, 2, moderation)

	risk, err := CountEnabledRiskRulesWithoutGroups()
	require.NoError(t, err)
	require.EqualValues(t, 2, risk)
}
