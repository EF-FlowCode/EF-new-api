package model

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting"
	settingconfig "eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/system_setting"
)

func TestUpdateOptionsBulkValidatedSerializesDependencyChecksAndPublication(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	seed := []Option{
		{Key: "GitHubOAuthEnabled", Value: "false"},
		{Key: "GitHubClientSecret", Value: "configured"},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed options: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{
		"GitHubOAuthEnabled": "false",
		"GitHubClientSecret": "configured",
	}
	common.OptionMapRWMutex.Unlock()
	previousOAuthSettings := system_setting.GetLegacyOAuthSettings()
	if err := system_setting.UpdateLegacyOAuthSettings(map[string]string{
		"GitHubOAuthEnabled": "false",
		"GitHubClientSecret": "configured",
	}); err != nil {
		t.Fatalf("seed legacy OAuth settings: %v", err)
	}
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(previousOAuthSettings)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	var activeValidators atomic.Int32
	var maxActiveValidators atomic.Int32
	validatorFor := func(updates map[string]string) func(map[string]string) error {
		return func(current map[string]string) error {
			active := activeValidators.Add(1)
			defer activeValidators.Add(-1)
			for observed := maxActiveValidators.Load(); active > observed; observed = maxActiveValidators.Load() {
				if maxActiveValidators.CompareAndSwap(observed, active) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)

			enabled := current["GitHubOAuthEnabled"]
			secret := current["GitHubClientSecret"]
			if value, ok := updates["GitHubOAuthEnabled"]; ok {
				enabled = value
			}
			if value, ok := updates["GitHubClientSecret"]; ok {
				secret = value
			}
			if enabled == "true" && secret == "" {
				return errors.New("enabled provider requires a secret")
			}
			return nil
		}
	}

	updates := []map[string]string{
		{"GitHubClientSecret": ""},
		{"GitHubOAuthEnabled": "true"},
	}
	start := make(chan struct{})
	results := make(chan error, len(updates))
	var workers sync.WaitGroup
	for _, values := range updates {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- UpdateOptionsBulkValidated(values, validatorFor(values))
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful updates = %d, want exactly one", successes)
	}
	if maxActiveValidators.Load() != 1 {
		t.Fatalf("concurrent validators = %d, want 1", maxActiveValidators.Load())
	}

	persisted := make(map[string]string)
	var options []Option
	if err := db.Find(&options).Error; err != nil {
		t.Fatalf("load options: %v", err)
	}
	for _, option := range options {
		persisted[option.Key] = option.Value
	}
	common.OptionMapRWMutex.RLock()
	memoryEnabled := common.OptionMap["GitHubOAuthEnabled"]
	memorySecret := common.OptionMap["GitHubClientSecret"]
	common.OptionMapRWMutex.RUnlock()
	if persisted["GitHubOAuthEnabled"] != memoryEnabled || persisted["GitHubClientSecret"] != memorySecret {
		t.Fatalf("database and memory diverged: db=%v memory=(%q,%q)", persisted, memoryEnabled, memorySecret)
	}
	if memoryEnabled == "true" && memorySecret == "" {
		t.Fatal("dependency invariant was violated")
	}
}

func TestUpdateOptionsBulkInvalidValueDoesNotChangeDatabaseOrMemory(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	seed := []Option{
		{Key: "Price", Value: "1"},
		{Key: "AutomaticDisableStatusCodes", Value: "400-499"},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed options: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	oldPrice, hadPrice := common.OptionMap["Price"]
	common.OptionMap["Price"] = "1"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if optionMapWasNil {
			common.OptionMap = nil
			return
		}
		if hadPrice {
			common.OptionMap["Price"] = oldPrice
		} else {
			delete(common.OptionMap, "Price")
		}
	})

	err := UpdateOptionsBulk(map[string]string{
		"Price":                       "2",
		"AutomaticDisableStatusCodes": "not-a-status-code",
	})
	if err == nil {
		t.Fatal("expected invalid option batch to fail")
	}

	var price Option
	if err = db.Where("`key` = ?", "Price").First(&price).Error; err != nil {
		t.Fatalf("load Price: %v", err)
	}
	if price.Value != "1" {
		t.Fatalf("database Price = %q, want unchanged value %q", price.Value, "1")
	}
	common.OptionMapRWMutex.RLock()
	memoryPrice := common.OptionMap["Price"]
	common.OptionMapRWMutex.RUnlock()
	if memoryPrice != "1" {
		t.Fatalf("in-memory Price = %q, want unchanged value %q", memoryPrice, "1")
	}
}

func TestUpdateOptionsBulkRejectsLegacyPublisherErrorsBeforeTransaction(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	if err := db.Create(&Option{Key: "Price", Value: "1"}).Error; err != nil {
		t.Fatalf("seed Price: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	oldPrice, hadPrice := common.OptionMap["Price"]
	common.OptionMap["Price"] = "1"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if optionMapWasNil {
			common.OptionMap = nil
			return
		}
		if hadPrice {
			common.OptionMap["Price"] = oldPrice
		} else {
			delete(common.OptionMap, "Price")
		}
	})

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "invitation policy", key: "InvitationCodePolicy", value: `{invalid`},
		{name: "group rate limit", key: "ModelRequestRateLimitGroup", value: `{"vip":[0,0]}`},
		{name: "model RPM", key: "ModelRPMConfig", value: `{"enabled":true,"models":{"gpt":{"global_rpm":0}}}`},
		{name: "payment methods", key: "PayMethods", value: `[{"name":1}]`},
		{name: "chat links", key: "Chats", value: `{invalid`},
		{name: "ticket assignment", key: "TicketAssignConfig", value: `{invalid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateOptionsBulk(map[string]string{
				"Price": "2",
				tt.key:  tt.value,
			})
			if err == nil {
				t.Fatal("expected invalid option batch to fail")
			}

			var price Option
			if err = db.Where("`key` = ?", "Price").First(&price).Error; err != nil {
				t.Fatalf("load Price: %v", err)
			}
			if price.Value != "1" {
				t.Fatalf("database Price = %q, want unchanged value %q", price.Value, "1")
			}
			var invalidCount int64
			if err = db.Model(&Option{}).Where("`key` = ?", tt.key).Count(&invalidCount).Error; err != nil {
				t.Fatalf("count invalid option: %v", err)
			}
			if invalidCount != 0 {
				t.Fatalf("invalid option %s was persisted", tt.key)
			}
			common.OptionMapRWMutex.RLock()
			memoryPrice := common.OptionMap["Price"]
			common.OptionMapRWMutex.RUnlock()
			if memoryPrice != "1" {
				t.Fatalf("in-memory Price = %q, want unchanged value %q", memoryPrice, "1")
			}
		})
	}
}

func TestUpdateOptionCanonicalizesBooleanBeforePersistenceAndPublication(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	originalGeneral := operation_setting.GetGeneralSetting()

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{"GitHubOAuthEnabled": "false"}
	common.OptionMapRWMutex.Unlock()
	previousOAuthSettings := system_setting.GetLegacyOAuthSettings()
	if err := system_setting.UpdateLegacyOAuthSettings(map[string]string{"GitHubOAuthEnabled": "false"}); err != nil {
		t.Fatalf("seed legacy OAuth settings: %v", err)
	}
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(previousOAuthSettings)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
		if _, err := settingconfig.GlobalConfig.Update("general_setting", map[string]string{
			"ping_interval_enabled": strconv.FormatBool(originalGeneral.PingIntervalEnabled),
		}); err != nil {
			t.Fatalf("restore general config: %v", err)
		}
	})

	if err := UpdateOption("GitHubOAuthEnabled", "TRUE"); err != nil {
		t.Fatalf("UpdateOption() failed: %v", err)
	}
	var persisted Option
	if err := db.Where("`key` = ?", "GitHubOAuthEnabled").First(&persisted).Error; err != nil {
		t.Fatalf("load persisted option: %v", err)
	}
	common.OptionMapRWMutex.RLock()
	published := common.OptionMap["GitHubOAuthEnabled"]
	common.OptionMapRWMutex.RUnlock()
	legacyOAuthSettings := system_setting.GetLegacyOAuthSettings()
	if persisted.Value != "true" || published != "true" || !legacyOAuthSettings.GitHubOAuthEnabled {
		t.Fatalf("boolean versions diverged: db=%q OptionMap=%q snapshot=%t", persisted.Value, published, legacyOAuthSettings.GitHubOAuthEnabled)
	}

	if err := UpdateOption("general_setting.ping_interval_enabled", "1"); err != nil {
		t.Fatalf("UpdateOption() registered bool failed: %v", err)
	}
	persisted = Option{}
	if err := db.Where("`key` = ?", "general_setting.ping_interval_enabled").First(&persisted).Error; err != nil {
		t.Fatalf("load registered bool option: %v", err)
	}
	common.OptionMapRWMutex.RLock()
	published = common.OptionMap["general_setting.ping_interval_enabled"]
	common.OptionMapRWMutex.RUnlock()
	if persisted.Value != "true" || published != "true" || !operation_setting.GetGeneralSetting().PingIntervalEnabled {
		t.Fatalf("registered boolean versions diverged: db=%q OptionMap=%q config=%t", persisted.Value, published, operation_setting.GetGeneralSetting().PingIntervalEnabled)
	}
}

func TestUpdateOptionsBulkPublishesLegacyOAuthSnapshot(t *testing.T) {
	useModelIntegrationTestDB(t, &Option{})
	originalSettings := system_setting.GetLegacyOAuthSettings()
	expected := modelLegacyOAuthSettings("published", true, 3)

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(originalSettings)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	values := legacyOAuthOptionValues(expected)
	if err := UpdateOptionsBulk(values); err != nil {
		t.Fatalf("update legacy OAuth options: %v", err)
	}
	if got := system_setting.GetLegacyOAuthSettings(); got != expected {
		t.Fatalf("published snapshot = %+v, want %+v", got, expected)
	}

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for key, expectedValue := range values {
		if got := common.OptionMap[key]; got != expectedValue {
			t.Fatalf("OptionMap[%q] = %q, want %q", key, got, expectedValue)
		}
	}
}

func TestLoadOptionsFromDatabasePublishesLegacyOAuthSnapshot(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	if err := migrateModelPricingFromDatabase(); err != nil {
		t.Fatal(err)
	}
	originalSettings := system_setting.GetLegacyOAuthSettings()
	baseline := modelLegacyOAuthSettings("before-sync", false, 0)
	expected := modelLegacyOAuthSettings("after-sync", true, 4)
	if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(baseline)); err != nil {
		t.Fatalf("publish baseline: %v", err)
	}

	expectedValues := legacyOAuthOptionValues(expected)
	options := make([]Option, 0, len(expectedValues))
	for key, value := range expectedValues {
		options = append(options, Option{Key: key, Value: value})
	}
	if err := db.Create(&options).Error; err != nil {
		t.Fatalf("seed persisted OAuth options: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = legacyOAuthOptionValues(baseline)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(originalSettings)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	if err := loadOptionsFromDatabase(); err != nil {
		t.Fatalf("load persisted OAuth options: %v", err)
	}
	if got := system_setting.GetLegacyOAuthSettings(); got != expected {
		t.Fatalf("synced snapshot = %+v, want %+v", got, expected)
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for key, expectedValue := range expectedValues {
		if got := common.OptionMap[key]; got != expectedValue {
			t.Fatalf("synced OptionMap[%q] = %q, want %q", key, got, expectedValue)
		}
	}
}

func TestUpdateOptionsBulkInvalidLegacyOAuthValueKeepsSnapshot(t *testing.T) {
	useModelIntegrationTestDB(t, &Option{})
	originalSettings := system_setting.GetLegacyOAuthSettings()
	baseline := modelLegacyOAuthSettings("baseline", true, 2)
	if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(baseline)); err != nil {
		t.Fatalf("publish baseline: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = legacyOAuthOptionValues(baseline)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := system_setting.UpdateLegacyOAuthSettings(legacyOAuthOptionValues(originalSettings)); err != nil {
			t.Fatalf("restore legacy OAuth settings: %v", err)
		}
	})

	err := UpdateOptionsBulk(map[string]string{
		"LinuxDOClientId":          "must-not-publish",
		"LinuxDOMinimumTrustLevel": "5",
	})
	if err == nil {
		t.Fatal("expected invalid trust level to fail")
	}
	if got := system_setting.GetLegacyOAuthSettings(); got != baseline {
		t.Fatalf("failed update changed snapshot: got %+v, want %+v", got, baseline)
	}
	common.OptionMapRWMutex.RLock()
	clientID := common.OptionMap["LinuxDOClientId"]
	trustLevel := common.OptionMap["LinuxDOMinimumTrustLevel"]
	common.OptionMapRWMutex.RUnlock()
	if clientID != baseline.LinuxDOClientId || trustLevel != strconv.Itoa(baseline.LinuxDOMinimumTrustLevel) {
		t.Fatalf("failed update changed OptionMap: client_id=%q trust_level=%q", clientID, trustLevel)
	}
}

func TestUpdateOptionsBulkPublishesSMTPAndWorkerSnapshots(t *testing.T) {
	useModelIntegrationTestDB(t, &Option{})
	originalSMTP := common.GetSMTPSettings()
	originalWorker := system_setting.GetWorkerSettings()
	expectedSMTP := common.SMTPSettings{
		Server:             "smtp.published.example",
		Port:               2465,
		SSLEnabled:         true,
		StartTLSEnabled:    false,
		InsecureSkipVerify: false,
		ForceAuthLogin:     true,
		Account:            "mailer",
		From:               "sender@example.com",
		Token:              "smtp-token",
		ArchiveBcc:         "archive@example.com",
	}
	expectedWorker := system_setting.WorkerSettings{
		URL:                   "https://worker.published.example",
		ValidKey:              "worker-key",
		AllowHTTPImageRequest: true,
	}

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if err := common.UpdateSMTPSettings(smtpOptionValues(originalSMTP)); err != nil {
			t.Fatalf("restore SMTP settings: %v", err)
		}
		if err := system_setting.UpdateWorkerSettings(workerOptionValues(originalWorker)); err != nil {
			t.Fatalf("restore Worker settings: %v", err)
		}
	})

	values := smtpOptionValues(expectedSMTP)
	maps.Copy(values, workerOptionValues(expectedWorker))
	if err := UpdateOptionsBulk(values); err != nil {
		t.Fatalf("update SMTP and Worker options: %v", err)
	}
	if got := common.GetSMTPSettings(); got != expectedSMTP {
		t.Fatalf("SMTP snapshot = %+v, want %+v", got, expectedSMTP)
	}
	if got := system_setting.GetWorkerSettings(); got != expectedWorker {
		t.Fatalf("Worker snapshot = %+v, want %+v", got, expectedWorker)
	}

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for key, expectedValue := range values {
		if got := common.OptionMap[key]; got != expectedValue {
			t.Fatalf("OptionMap[%q] = %q, want %q", key, got, expectedValue)
		}
	}
}

func TestLoadOptionsFromDatabaseInvalidValueKeepsLastPublishedState(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{})
	originalConfig := setting.GetWaffoPancakeConfig()
	originalGeneral := operation_setting.GetGeneralSetting()
	baseline := setting.WaffoPancakeConfig{
		MerchantID: "merchant-before",
		PrivateKey: "private-before",
		ReturnURL:  "https://before.example.com",
		UnitPrice:  1.25,
		MinTopUp:   2,
		StoreID:    "store-before",
		ProductID:  "product-before",
	}
	if err := setting.UpdateWaffoPancakeConfig(waffoPancakeValues(baseline)); err != nil {
		t.Fatalf("seed Waffo Pancake config: %v", err)
	}
	if _, err := settingconfig.GlobalConfig.Update("general_setting", map[string]string{"docs_link": "https://before.example.com/docs"}); err != nil {
		t.Fatalf("seed general config: %v", err)
	}

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{
		"WaffoPancakeMerchantID": baseline.MerchantID,
		"WaffoPancakeUnitPrice":  "1.25",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		if err := setting.UpdateWaffoPancakeConfig(waffoPancakeValues(originalConfig)); err != nil {
			t.Fatalf("restore Waffo Pancake config: %v", err)
		}
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		if _, err := settingconfig.GlobalConfig.Update("general_setting", map[string]string{"docs_link": originalGeneral.DocsLink}); err != nil {
			t.Fatalf("restore general config: %v", err)
		}
	})

	options := []Option{
		{Key: "general_setting.docs_link", Value: "https://after.example.com/docs"},
		{Key: "WaffoPancakeMerchantID", Value: "merchant-after"},
		{Key: "WaffoPancakeUnitPrice", Value: "not-a-number"},
	}
	if err := db.Create(&options).Error; err != nil {
		t.Fatalf("seed persisted options: %v", err)
	}
	if err := loadOptionsFromDatabase(); err == nil {
		t.Fatal("expected invalid persisted option to fail")
	}
	if got := setting.GetWaffoPancakeConfig(); got != baseline {
		t.Fatalf("failed reload changed Waffo Pancake snapshot: got %+v, want %+v", got, baseline)
	}
	if got := operation_setting.GetGeneralSetting().DocsLink; got != "https://before.example.com/docs" {
		t.Fatalf("failed reload changed registered config: got %q", got)
	}
	common.OptionMapRWMutex.RLock()
	merchantID := common.OptionMap["WaffoPancakeMerchantID"]
	unitPrice := common.OptionMap["WaffoPancakeUnitPrice"]
	common.OptionMapRWMutex.RUnlock()
	if merchantID != baseline.MerchantID || unitPrice != "1.25" {
		t.Fatalf("failed reload changed OptionMap: merchant=%q unit_price=%q", merchantID, unitPrice)
	}
}

func waffoPancakeValues(config setting.WaffoPancakeConfig) map[string]string {
	return map[string]string{
		"WaffoPancakeMerchantID": config.MerchantID,
		"WaffoPancakePrivateKey": config.PrivateKey,
		"WaffoPancakeReturnURL":  config.ReturnURL,
		"WaffoPancakeUnitPrice":  fmt.Sprintf("%g", config.UnitPrice),
		"WaffoPancakeMinTopUp":   fmt.Sprintf("%d", config.MinTopUp),
		"WaffoPancakeStoreID":    config.StoreID,
		"WaffoPancakeProductID":  config.ProductID,
	}
}

func modelLegacyOAuthSettings(version string, enabled bool, trustLevel int) system_setting.LegacyOAuthSettings {
	return system_setting.LegacyOAuthSettings{
		GitHubOAuthEnabled:          enabled,
		GitHubClientId:              "github-id-" + version,
		GitHubClientSecret:          "github-secret-" + version,
		LinuxDOOAuthEnabled:         enabled,
		LinuxDOClientId:             "linuxdo-id-" + version,
		LinuxDOClientSecret:         "linuxdo-secret-" + version,
		LinuxDOMinimumTrustLevel:    trustLevel,
		TelegramOAuthEnabled:        enabled,
		TelegramBotToken:            "telegram-token-" + version,
		TelegramBotName:             "telegram-name-" + version,
		WeChatAuthEnabled:           enabled,
		WeChatServerAddress:         "https://wechat-" + version + ".example.com",
		WeChatServerToken:           "wechat-token-" + version,
		WeChatAccountQRCodeImageURL: "https://wechat-" + version + ".example.com/qr.png",
	}
}

func legacyOAuthOptionValues(settings system_setting.LegacyOAuthSettings) map[string]string {
	return map[string]string{
		"GitHubOAuthEnabled":          strconv.FormatBool(settings.GitHubOAuthEnabled),
		"GitHubClientId":              settings.GitHubClientId,
		"GitHubClientSecret":          settings.GitHubClientSecret,
		"LinuxDOOAuthEnabled":         strconv.FormatBool(settings.LinuxDOOAuthEnabled),
		"LinuxDOClientId":             settings.LinuxDOClientId,
		"LinuxDOClientSecret":         settings.LinuxDOClientSecret,
		"LinuxDOMinimumTrustLevel":    strconv.Itoa(settings.LinuxDOMinimumTrustLevel),
		"TelegramOAuthEnabled":        strconv.FormatBool(settings.TelegramOAuthEnabled),
		"TelegramBotToken":            settings.TelegramBotToken,
		"TelegramBotName":             settings.TelegramBotName,
		"WeChatAuthEnabled":           strconv.FormatBool(settings.WeChatAuthEnabled),
		"WeChatServerAddress":         settings.WeChatServerAddress,
		"WeChatServerToken":           settings.WeChatServerToken,
		"WeChatAccountQRCodeImageURL": settings.WeChatAccountQRCodeImageURL,
	}
}

func smtpOptionValues(settings common.SMTPSettings) map[string]string {
	return map[string]string{
		"SMTPServer":             settings.Server,
		"SMTPPort":               strconv.Itoa(settings.Port),
		"SMTPSSLEnabled":         strconv.FormatBool(settings.SSLEnabled),
		"SMTPStartTLSEnabled":    strconv.FormatBool(settings.StartTLSEnabled),
		"SMTPInsecureSkipVerify": strconv.FormatBool(settings.InsecureSkipVerify),
		"SMTPForceAuthLogin":     strconv.FormatBool(settings.ForceAuthLogin),
		"SMTPAccount":            settings.Account,
		"SMTPFrom":               settings.From,
		"SMTPToken":              settings.Token,
		"SMTPArchiveBcc":         settings.ArchiveBcc,
	}
}

func workerOptionValues(settings system_setting.WorkerSettings) map[string]string {
	return map[string]string{
		"WorkerUrl":                          settings.URL,
		"WorkerValidKey":                     settings.ValidKey,
		"WorkerAllowHttpImageRequestEnabled": strconv.FormatBool(settings.AllowHTTPImageRequest),
	}
}
