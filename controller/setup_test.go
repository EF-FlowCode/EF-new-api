package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type setupAPIResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func setupSetupControllerTest(t *testing.T) {
	t.Helper()

	db := testdb.OpenMySQL8(t, &model.User{}, &model.Option{}, &model.Setup{})

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousSetup := constant.IsSetupComplete()
	previousSiteMode := operation_setting.GetSiteModeSettings()

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	model.DB = db
	model.LOG_DB = db
	constant.SetSetupComplete(false)

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		constant.SetSetupComplete(previousSetup)
		require.NoError(t, operation_setting.UpdateSiteModeSettings(map[string]string{
			"SelfUseModeEnabled": strconv.FormatBool(previousSiteMode.SelfUseModeEnabled),
			"DemoSiteEnabled":    strconv.FormatBool(previousSiteMode.DemoSiteEnabled),
		}))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func postSetupForTest(t *testing.T, body string) setupAPIResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostSetup(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response setupAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func getSetupForTest(t *testing.T) setupAPIResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/setup", nil)

	GetSetup(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response setupAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func TestGetSetupOmitsLegacyDatabaseType(t *testing.T) {
	for _, setupComplete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete_%t", setupComplete), func(t *testing.T) {
			setupSetupControllerTest(t)
			constant.SetSetupComplete(setupComplete)

			response := getSetupForTest(t)

			assert.True(t, response.Success)
			assert.Contains(t, response.Data, "status")
			assert.Contains(t, response.Data, "root_init")
			assert.NotContains(t, response.Data, "database_type")
		})
	}
}

func TestPostSetupUsesCanonicalUserLengthRules(t *testing.T) {
	setupSetupControllerTest(t)

	username := "fourteen_chars"
	require.Len(t, username, 14)
	response := postSetupForTest(t, fmt.Sprintf(
		`{"username":%q,"password":"password123","confirmPassword":"password123","SelfUseModeEnabled":true,"DemoSiteEnabled":false}`,
		username,
	))

	assert.True(t, response.Success, response.Message)
	var root model.User
	require.NoError(t, model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error)
	assert.Equal(t, username, root.Username)
}

func TestPostSetupRollsBackAllStateWhenSetupRecordFails(t *testing.T) {
	setupSetupControllerTest(t)
	require.NoError(t, operation_setting.UpdateSiteModeSettings(map[string]string{
		"SelfUseModeEnabled": "false",
		"DemoSiteEnabled":    "false",
	}))

	const callbackName = "test:fail_setup_record"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(
		callbackName,
		func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Setup" {
				tx.AddError(errors.New("injected setup record failure"))
			}
		},
	))
	t.Cleanup(func() {
		model.DB.Callback().Create().Remove(callbackName)
	})

	response := postSetupForTest(t,
		`{"username":"setup_admin","password":"password123","confirmPassword":"password123","SelfUseModeEnabled":true,"DemoSiteEnabled":true}`,
	)

	assert.False(t, response.Success)
	assert.False(t, constant.IsSetupComplete())
	siteMode := operation_setting.GetSiteModeSettings()
	assert.False(t, siteMode.SelfUseModeEnabled)
	assert.False(t, siteMode.DemoSiteEnabled)

	var userCount int64
	require.NoError(t, model.DB.Model(&model.User{}).Count(&userCount).Error)
	assert.Zero(t, userCount)
	var optionCount int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&optionCount).Error)
	assert.Zero(t, optionCount)
}

func TestCheckSetupDoesNotPromoteRootOnlyState(t *testing.T) {
	setupSetupControllerTest(t)
	require.NoError(t, model.DB.Create(&model.User{
		Username: "existing_root",
		Password: "stored_hash",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	model.CheckSetup()

	assert.False(t, constant.IsSetupComplete())
	var setupCount int64
	require.NoError(t, model.DB.Model(&model.Setup{}).Count(&setupCount).Error)
	assert.Zero(t, setupCount)
}

func TestCheckSetupRestoresCompletedStateFromSetupRecord(t *testing.T) {
	setupSetupControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Setup{
		ID:            1,
		Version:       "test",
		InitializedAt: 1,
	}).Error)
	constant.SetSetupComplete(false)

	model.CheckSetup()

	assert.True(t, constant.IsSetupComplete())
}

func TestPostSetupAllowsOnlyOneConcurrentInitialization(t *testing.T) {
	setupSetupControllerTest(t)

	bodies := []string{
		`{"username":"root_alpha","password":"password123","confirmPassword":"password123","SelfUseModeEnabled":true,"DemoSiteEnabled":false}`,
		`{"username":"root_beta","password":"password123","confirmPassword":"password123","SelfUseModeEnabled":false,"DemoSiteEnabled":true}`,
	}
	start := make(chan struct{})
	responses := make(chan setupAPIResponse, len(bodies))
	errs := make(chan error, len(bodies))
	var wg sync.WaitGroup
	for _, body := range bodies {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			<-start
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			PostSetup(ctx)
			var response setupAPIResponse
			if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				errs <- err
				return
			}
			responses <- response
		}(body)
	}
	close(start)
	wg.Wait()
	close(responses)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	successes := 0
	for response := range responses {
		if response.Success {
			successes++
		}
	}
	assert.Equal(t, 1, successes)

	var rootCount int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("role = ?", common.RoleRootUser).Count(&rootCount).Error)
	assert.EqualValues(t, 1, rootCount)
	var setupCount int64
	require.NoError(t, model.DB.Model(&model.Setup{}).Count(&setupCount).Error)
	assert.EqualValues(t, 1, setupCount)
	var optionCount int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&optionCount).Error)
	assert.EqualValues(t, 2, optionCount)
}
