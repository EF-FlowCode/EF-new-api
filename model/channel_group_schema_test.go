package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestChannelGroupSchemaUsesLongTextForAggregateList(t *testing.T) {
	channelSchema, err := schema.Parse(&Channel{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	channelGroup := channelSchema.LookUpField("Group")
	require.NotNil(t, channelGroup)
	require.Equal(t, "longtext", channelGroup.TagSettings["TYPE"])
	require.False(t, channelGroup.HasDefaultValue)

	abilitySchema, err := schema.Parse(&Ability{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	abilityGroup := abilitySchema.LookUpField("Group")
	require.NotNil(t, abilityGroup)
	require.Equal(t, "varchar(64)", abilityGroup.TagSettings["TYPE"])
}
