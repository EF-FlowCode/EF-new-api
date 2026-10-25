package model

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
)

func TestEmailTemplateOptionsRejectInvalidBeforePersistence(t *testing.T) {
	key := constant.EmailTemplateKeyPasswordReset
	subjectKey, bodyKey := constant.EmailTemplateSubjectKey(key), constant.EmailTemplateBodyKey(key)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{subjectKey: "原主题", bodyKey: "原正文"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	// 测试不建立数据库；无效模板必须在任何持久化操作之前被拒绝。
	if err := UpdateOption(bodyKey, "{{valid_minutes}} 缺链接"); err == nil {
		t.Fatal("single update accepted invalid body")
	}
	if err := UpdateOptionsBulk(map[string]string{subjectKey: "新主题", bodyKey: "{{reset_url}} 缺有效期"}); err == nil {
		t.Fatal("bulk update accepted invalid body")
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	if common.OptionMap[subjectKey] != "原主题" || common.OptionMap[bodyKey] != "原正文" {
		t.Fatal("rejected update changed published options")
	}
}
