package setting

import (
	"encoding/json"
	"maps"
	"sync"

	"eflow.local/new-api/common"
)

var (
	chatsMu sync.RWMutex
	chats   = []map[string]string{
		//{
		//	"ChatGPT Next Web 官方示例": "https://app.nextchat.dev/#/?settings={\"key\":\"{key}\",\"url\":\"{address}\"}",
		//},
		{
			"Cherry Studio": "cherrystudio://providers/api-keys?v=1&data={cherryConfig}",
		},
		{
			"AionUI": "aionui://provider/add?v=1&data={aionuiConfig}",
		},
		{
			"流畅阅读": "fluentread",
		},
		{
			"CC Switch": "ccswitch",
		},
		{
			"DeepChat": "deepchat://provider/install?v=1&data={deepchatConfig}",
		},
		{
			"AQBot": "aqbot://providers?{aqbotConfig}",
		},
		{
			"Lobe Chat 官方示例": "https://chat-preview.lobehub.com/?settings={\"keyVaults\":{\"openai\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\"}}}",
		},
		{
			"AI as Workspace": "https://aiaw.app/set-provider?provider={\"type\":\"openai\",\"settings\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\",\"compatibility\":\"strict\"}}",
		},
		{
			"AMA 问天": "ama://set-api-key?server={address}&key={key}",
		},
		{
			"OpenCat": "opencat://team/join?domain={address}&token={key}",
		},
	}
)

func parseChatsJSON(jsonString string) ([]map[string]string, error) {
	var parsed []map[string]string
	if err := json.Unmarshal([]byte(jsonString), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func ValidateChatsJsonString(jsonString string) error {
	_, err := parseChatsJSON(jsonString)
	return err
}

func UpdateChatsByJsonString(jsonString string) error {
	parsed, err := parseChatsJSON(jsonString)
	if err != nil {
		return err
	}
	chatsMu.Lock()
	chats = parsed
	chatsMu.Unlock()
	return nil
}

func GetChats() []map[string]string {
	chatsMu.RLock()
	defer chatsMu.RUnlock()
	result := make([]map[string]string, len(chats))
	for i, chat := range chats {
		result[i] = make(map[string]string, len(chat))
		maps.Copy(result[i], chat)
	}
	return result
}

func Chats2JsonString() string {
	jsonBytes, err := json.Marshal(GetChats())
	if err != nil {
		common.SysLog("error marshalling chats: " + err.Error())
		return "[]"
	}
	return string(jsonBytes)
}
