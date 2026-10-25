package controller

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const telegramBindTestToken = "telegram-bind-test-token"

type telegramBindHarness struct {
	engine *gin.Engine
	cookie []*http.Cookie
}

func newTelegramBindHarness(t *testing.T, userID int) *telegramBindHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previous := system_setting.GetLegacyOAuthSettings()
	require.NoError(t, system_setting.UpdateLegacyOAuthSettings(map[string]string{
		"TelegramOAuthEnabled": "true",
		"TelegramBotToken":     telegramBindTestToken,
	}))
	t.Cleanup(func() {
		enabled := "false"
		if previous.TelegramOAuthEnabled {
			enabled = "true"
		}
		_ = system_setting.UpdateLegacyOAuthSettings(map[string]string{
			"TelegramOAuthEnabled": enabled,
			"TelegramBotToken":     previous.TelegramBotToken,
		})
	})
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("telegram-bind-flow-test-key"))))
	// Stand-in for SessionAuth/UserAuth: the flow-token contract is independent of how the user was authenticated.
	engine.Use(func(c *gin.Context) { c.Set("id", userID) })
	engine.POST("/start", TelegramBindStart)
	engine.GET("/bind", TelegramBind)
	return &telegramBindHarness{engine: engine}
}

func (h *telegramBindHarness) do(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for _, c := range h.cookie {
		req.AddCookie(c)
	}
	res := httptest.NewRecorder()
	h.engine.ServeHTTP(res, req)
	if cookies := res.Result().Cookies(); len(cookies) > 0 {
		h.cookie = cookies
	}
	return res
}

func (h *telegramBindHarness) start(t *testing.T) string {
	t.Helper()
	res := h.do(t, http.MethodPost, "/start")
	var result struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.NotEmpty(t, result.Data)
	return result.Data
}

func telegramBindURL(flowToken string, params url.Values) string {
	query := url.Values{}
	maps.Copy(query, params)
	if flowToken != "" {
		query.Set(telegramBindFlowParam, flowToken)
	}
	return "/bind?" + query.Encode()
}

func requireTelegramBindRejected(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "绑定流程已失效")
}

func TestTelegramBindRejectsMissingOrForeignFlowToken(t *testing.T) {
	signed := signedTelegramAuthorization(telegramBindTestToken, time.Now())

	h := newTelegramBindHarness(t, 1)
	requireTelegramBindRejected(t, h.do(t, http.MethodGet, telegramBindURL("", signed)))

	// A link carrying someone else's token must not bind, and it burns the
	// victim's pending flow so the legitimate token cannot be replayed later.
	own := h.start(t)
	requireTelegramBindRejected(t, h.do(t, http.MethodGet, telegramBindURL("attacker-token", signed)))
	requireTelegramBindRejected(t, h.do(t, http.MethodGet, telegramBindURL(own, signed)))

	// Without any session the callback is rejected before touching the database.
	fresh := newTelegramBindHarness(t, 1)
	requireTelegramBindRejected(t, fresh.do(t, http.MethodGet, telegramBindURL(own, signed)))
}

func TestTelegramBindFlowTokenIsSingleUseAndExcludedFromSignature(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.User{})
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	user := model.User{Username: "tg-bind-user", Password: "password-hash", AffCode: "tgbind01"}
	require.NoError(t, db.Create(&user).Error)

	h := newTelegramBindHarness(t, user.Id)
	token := h.start(t)
	signed := signedTelegramAuthorization(telegramBindTestToken, time.Now())
	res := h.do(t, http.MethodGet, telegramBindURL(token, signed))
	require.Equal(t, http.StatusFound, res.Code, res.Body.String())

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "123456", stored.TelegramId)

	requireTelegramBindRejected(t, h.do(t, http.MethodGet, telegramBindURL(token, signed)))
}
