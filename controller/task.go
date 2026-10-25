package controller

import (
	"context"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/relay"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// UpdateTaskBulk 薄入口，实际轮询逻辑在 service 层
func UpdateTaskBulk() {
	service.TaskPollingLoop(context.Background(), 15*time.Second)
}

func GetAllTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	// 解析其他查询参数
	queryParams := model.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ChannelID:      c.Query("channel_id"),
	}

	items := model.TaskGetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllTasks(queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, true))
	common.ApiSuccess(c, pageInfo)
}

func GetUserTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	queryParams := model.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	items := model.TaskGetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllUserTask(userId, queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, false))
	common.ApiSuccess(c, pageInfo)
}

func tasksToDto(tasks []*model.Task, fillUser bool) []*dto.TaskDto {
	var userIdMap map[int]*model.UserBase
	if fillUser {
		userIdMap = make(map[int]*model.UserBase)
		userIds := types.NewSet[int]()
		for _, task := range tasks {
			userIds.Add(task.UserId)
		}
		for _, userId := range userIds.Items() {
			cacheUser, err := model.GetUserCache(userId)
			if err == nil {
				userIdMap[userId] = cacheUser
			}
		}
	}
	result := make([]*dto.TaskDto, len(tasks))
	for i, task := range tasks {
		if fillUser {
			if user, ok := userIdMap[task.UserId]; ok {
				task.Username = user.Username
			}
		}
		result[i] = relay.TaskModel2Dto(task)
	}
	markDisabledTaskChannels(tasks, result)
	return result
}

// markDisabledTaskChannels flags terminal tasks whose channel (or, for plugin
// tasks, production binding) is no longer active: the stored result is shown
// as a historical record. One channel query per page; channel ids stay hidden.
func markDisabledTaskChannels(tasks []*model.Task, result []*dto.TaskDto) {
	ids := types.NewSet[int]()
	for _, task := range tasks {
		if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
			ids.Add(task.ChannelId)
		}
	}
	if ids.Len() == 0 {
		return
	}
	var channels []model.Channel
	if err := model.DB.Select("id", "status").Where("id IN ?", ids.Items()).Find(&channels).Error; err != nil {
		return
	}
	enabled := map[int]bool{}
	for _, channel := range channels {
		enabled[channel.Id] = channel.Status == common.ChannelStatusEnabled
	}
	for i, task := range tasks {
		if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
			continue
		}
		active := enabled[task.ChannelId]
		if active && task.PrivateData.Plugin != nil {
			_, binding, bound := management.Default.ProductionHandle(task.ChannelId)
			active = bound && binding.Key == task.PrivateData.Plugin.Key
		}
		if !active {
			result[i].ChannelStatus = management.ChannelStatusDisabled
		}
	}
}
