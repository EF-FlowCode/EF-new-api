package model

import (
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
)

func TestMigrateChannelTypeRenumberV1(t *testing.T) {
	db := testdb.OpenMySQL8(t, &Channel{}, &Task{}, &Option{}, &TaskPluginControl{}, &TaskPluginProductionBinding{})
	require.NoError(t, ensureTaskPluginControl(db))
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	for _, channel := range []Channel{
		{Id: 74, Type: 58, Name: "happyhorse"},
		{Id: 86, Type: 59, Name: "tts"},
		{Id: 87, Type: 60, Name: "asr"},
		{Id: 88, Type: 61, Name: "asr-realtime"},
		{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "openai"},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	require.NoError(t, db.Create(&Task{TaskID: "hh-1", Platform: "58", ChannelId: 74, Status: TaskStatusSuccess, UpdatedAt: 123}).Error)
	require.NoError(t, db.Create(&Task{TaskID: "ali-1", Platform: "17", ChannelId: 17, Status: TaskStatusSuccess}).Error)
	require.NoError(t, db.Create(&TaskPluginProductionBinding{ChannelID: 74, Key: "happyhorse", Version: "1.0.0", SourceHash: legacyHappyhorseFactorySourceHash, Factory: true, Enabled: true}).Error)
	require.NoError(t, db.Create(&TaskPluginProductionBinding{ChannelID: 75, Key: "happyhorse", Version: "1.0.0", SourceHash: "uploaded-version", Factory: false, Enabled: true}).Error)
	var before TaskPluginControl
	require.NoError(t, db.First(&before, 1).Error)

	report, err := MigrateChannelTypeRenumberV1("new-factory-hash")
	require.NoError(t, err)
	require.False(t, report.AlreadyApplied)
	require.EqualValues(t, 4, report.Channels)
	require.EqualValues(t, 1, report.Tasks)
	require.EqualValues(t, 1, report.PluginBindings)

	types := map[int]int{}
	var channels []Channel
	require.NoError(t, db.Find(&channels).Error)
	for _, channel := range channels {
		types[channel.Id] = channel.Type
	}
	require.Equal(t, map[int]int{74: 1001, 86: 1002, 87: 1003, 88: 1004, 1: constant.ChannelTypeOpenAI}, types)

	var task Task
	require.NoError(t, db.Where("task_id = ?", "hh-1").First(&task).Error)
	require.EqualValues(t, "1001", task.Platform)
	require.EqualValues(t, 123, task.UpdatedAt, "renumbering must not touch task timestamps")
	var aliTask Task
	require.NoError(t, db.Where("task_id = ?", "ali-1").First(&aliTask).Error)
	require.EqualValues(t, "17", aliTask.Platform)

	var bindings []TaskPluginProductionBinding
	require.NoError(t, db.Order("channel_id").Find(&bindings).Error)
	require.Equal(t, "new-factory-hash", bindings[0].SourceHash)
	require.Equal(t, "uploaded-version", bindings[1].SourceHash, "non-factory bindings keep their pinned source")
	var after TaskPluginControl
	require.NoError(t, db.First(&after, 1).Error)
	require.Equal(t, before.Revision+1, after.Revision)

	// 只执行一次：之后新建的 58 号渠道属于上游类型，不能再被挪走。
	require.NoError(t, db.Create(&Channel{Id: 200, Type: 58, Name: "upstream-advanced-custom"}).Error)
	report, err = MigrateChannelTypeRenumberV1("new-factory-hash")
	require.NoError(t, err)
	require.True(t, report.AlreadyApplied)
	var upstream Channel
	require.NoError(t, db.First(&upstream, 200).Error)
	require.Equal(t, 58, upstream.Type)

	require.Error(t, updateOptionsValidated(map[string]string{ChannelTypeRenumberMigrationOption: "{}"}, nil))
}

func TestGetChannelBaseURLCoversPrivateTypes(t *testing.T) {
	require.Equal(t, "https://dashscope.aliyuncs.com", constant.GetChannelBaseURL(constant.ChannelTypeAliTTS))
	require.Equal(t, "https://api.openai.com", constant.GetChannelBaseURL(constant.ChannelTypeOpenAI))
	require.Equal(t, "", constant.GetChannelBaseURL(5000))
	require.Equal(t, "", constant.GetChannelBaseURL(-1))
}
