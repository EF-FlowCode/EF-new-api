package model

import (
	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicChannelFilterBeforePriority(t *testing.T) {
	db := useChannelCacheTestDB(t)
	for id, priority := range map[int]int64{1: 100, 2: 10} {
		ch := Channel{Id: id, Type: 1, Status: common.ChannelStatusEnabled, Models: "movie", Group: "default", Priority: &priority}
		require.NoError(t, db.Create(&ch).Error)
		require.NoError(t, ch.AddAbilities(db))
	}
	InitChannelCache()
	for _, cache := range []bool{true, false} {
		common.MemoryCacheEnabled = cache
		ch, err := GetRandomSatisfiedChannelForRequest("default", "movie", 0, "/v1/responses", func(ch *Channel) bool { return ch.Id == 2 })
		require.NoError(t, err)
		require.NotNil(t, ch)
		require.Equal(t, 2, ch.Id)
		ch, err = GetRandomSatisfiedChannelForRequest("default", "movie", 0, "/v1/responses", func(*Channel) bool { return false })
		require.NoError(t, err)
		require.Nil(t, ch)
	}
	require.NoError(t, db.Delete(&Channel{}, 2).Error)
	filtered := filterAbilitiesByRequestPath([]Ability{{ChannelId: 2}}, "/v1/responses", "movie", func(*Channel) bool { return true })
	require.Empty(t, filtered)
}
