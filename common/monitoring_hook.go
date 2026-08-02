package common

var GroupMonitoringHook func(group string, channelId int, isSuccess bool, promptTokens, cacheTokens, useTimeMs, frtMs int, modelName string, statusCode int, content string)
