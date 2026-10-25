package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	rootdto "eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestAdvancedCustomRouteURLAndAuth(t *testing.T) {
	requestURL, err := advancedCustomRouteURL("https://gateway.example/base/", "/v1/balance")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example/base/v1/balance", requestURL)

	requestURL, headers, err := applyAdvancedCustomRouteAuth(
		"https://gateway.example/balance?existing=1",
		&dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeQuery, Name: "token", Value: "prefix-{api_key}"},
		"sk-secret",
	)
	require.NoError(t, err)
	require.Contains(t, requestURL, "existing=1")
	require.Contains(t, requestURL, "token=prefix-sk-secret")
	require.Empty(t, headers.Get("Authorization"))
}

func TestFetchAdvancedCustomManagementBodyUsesConfiguredAuth(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	defer server.Close()

	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: dto.AdvancedCustomModelListPath, UpstreamPath: "/models", Auth: &dto.AdvancedCustomRouteAuth{
			Type: dto.AdvancedCustomAuthTypeHeader, Name: "X-API-Key", Value: "{api_key}",
		}},
	}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "sk-secret", OtherSettings: string(settings)}

	body, err := fetchAdvancedCustomManagementBody(channel, dto.AdvancedCustomModelListPath)
	require.NoError(t, err)
	require.JSONEq(t, `{"data":[{"id":"model-a"}]}`, string(body))
	require.Equal(t, "sk-secret", receivedAuth)
}

func TestFetchAdvancedCustomManagementBodyUsesEnabledKeyAndHeaderOverrides(t *testing.T) {
	type receivedRequest struct {
		auth   string
		static string
		client string
		host   string
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- receivedRequest{
			auth:   r.Header.Get("X-Route-Key"),
			static: r.Header.Get("X-Static"),
			client: r.Header.Get("X-Client"),
			host:   r.Host,
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	defer server.Close()

	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: dto.AdvancedCustomModelListPath, UpstreamPath: "/models", Auth: &dto.AdvancedCustomRouteAuth{
			Type: dto.AdvancedCustomAuthTypeHeader, Name: "X-Route-Key", Value: "route-{api_key}",
		}},
	}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := server.URL
	headerOverride := `{"X-Route-Key":"global-{api_key}","X-Static":"static-value","X-Client":"{client_header:X-Client}","Host":"models.example.test","*":""}`
	channel := &model.Channel{
		BaseURL:        &baseURL,
		Key:            "disabled-key\nenabled-key",
		OtherSettings:  string(settings),
		HeaderOverride: &headerOverride,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusEnabled},
		},
	}

	_, err = fetchAdvancedCustomManagementBody(channel, dto.AdvancedCustomModelListPath)
	require.NoError(t, err)
	request := <-received
	require.Equal(t, "global-enabled-key", request.auth)
	require.Equal(t, "static-value", request.static)
	require.Empty(t, request.client)
	require.Equal(t, "models.example.test", request.host)
}

func TestParseAdvancedCustomModelIDsRequiresNonEmptyData(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed", body: `{"data":`, want: "invalid OpenAI Models response"},
		{name: "missing data", body: `{"object":"list"}`, want: "data is required"},
		{name: "null data", body: `{"data":null}`, want: "data is required"},
		{name: "empty data", body: `{"data":[]}`, want: "no valid model IDs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			models, err := parseAdvancedCustomModelIDs([]byte(test.body))
			require.ErrorContains(t, err, test.want)
			require.Nil(t, models)
		})
	}

	models, err := parseAdvancedCustomModelIDs([]byte(`{"data":[{"id":" model-a "},{"id":""},{"id":"model-a"},{"id":"model-b"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b"}, models)
}

func TestFetchAdvancedCustomManagementBodyRejectsDisabledMultiKeyChannel(t *testing.T) {
	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: dto.AdvancedCustomModelListPath,
		UpstreamPath: "/models",
	}}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := "https://gateway.example"
	channel := &model.Channel{
		Type:          constant.ChannelTypeCustom,
		BaseURL:       &baseURL,
		Key:           "disabled-key",
		OtherSettings: string(settings),
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}
	_, err = fetchAdvancedCustomManagementBody(channel, dto.AdvancedCustomModelListPath)
	require.Error(t, err)
}

func TestFetchAdvancedCustomBalanceReturnsRawResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"credits":"unstructured","provider":"custom"}`))
	}))
	defer server.Close()

	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: dto.AdvancedCustomBalancePath, UpstreamPath: "/balance"},
	}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "sk-secret", OtherSettings: string(settings)}

	result, err := fetchAdvancedCustomBalance(channel)
	require.NoError(t, err)
	require.Nil(t, result.Balance)
	require.True(t, strings.Contains(result.RawResponse, "unstructured"))
}

func TestFetchAdvancedCustomBalanceAcceptsNegativeNumericResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"credit_summary","total_available":-1.25}`))
	}))
	defer server.Close()

	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: dto.AdvancedCustomBalancePath,
		UpstreamPath: "/balance",
	}}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "sk-secret", OtherSettings: string(settings)}

	result, err := fetchAdvancedCustomBalance(channel)
	require.NoError(t, err)
	require.NotNil(t, result.Balance)
	require.Equal(t, -1.25, *result.Balance)
}

func TestFetchAdvancedCustomBalanceKeepsNonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("provider balance unavailable"))
	}))
	defer server.Close()

	config := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: dto.AdvancedCustomBalancePath,
		UpstreamPath: "/balance",
	}}}
	settings, err := common.Marshal(rootdto.ChannelOtherSettings{AdvancedCustom: config})
	require.NoError(t, err)
	baseURL := server.URL
	channel := &model.Channel{BaseURL: &baseURL, Key: "sk-secret", OtherSettings: string(settings)}

	result, err := fetchAdvancedCustomBalance(channel)
	require.NoError(t, err)
	require.Nil(t, result.Balance)
	require.Equal(t, "provider balance unavailable", result.RawResponse)
}

func TestSanitizeAdvancedCustomSecretError(t *testing.T) {
	err := sanitizeAdvancedCustomSecretError(errors.New("dial sk-secret?token=sk-secret"), "sk-secret", "https://example.test/balance?token=sk-secret")
	require.NotContains(t, err.Error(), "sk-secret")
}
