package service

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"eflow.local/new-api/constant"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/golang-jwt/jwt/v5"
)

type TaskPluginAuthFunc func(context.Context, int, string, string, string, host.Executor) (management.TaskQueryEnvironment, error)

func PrepareTaskPluginAuth(ctx context.Context, channelType int, base, key, version string, executor host.Executor) (management.TaskQueryEnvironment, error) {
	env := management.TaskQueryEnvironment{BaseURL: base, UserSetting: map[string]any{"apiVersion": version, "geminiVersion": version}}
	if channelType != constant.ChannelTypeVertexAi {
		return env, nil
	}
	var creds struct {
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if json.Unmarshal([]byte(key), &creds) != nil || creds.ProjectID == "" || creds.ClientEmail == "" {
		return env, errors.New("invalid Vertex service account")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(creds.PrivateKey, `\n`, "\n")))
	if block == nil {
		return env, errors.New("invalid Vertex private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return env, errors.New("invalid Vertex private key")
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return env, errors.New("Vertex requires an RSA private key")
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": creds.ClientEmail, "scope": "https://www.googleapis.com/auth/cloud-platform", "aud": "https://www.googleapis.com/oauth2/v4/token", "iat": now.Unix(), "exp": now.Add(30 * time.Minute).Unix()}).SignedString(privateKey)
	if err != nil {
		return env, errors.New("Vertex JWT signing failed")
	}
	token, err := exchangeTaskPluginToken(ctx, "https://www.googleapis.com/oauth2/v4/token", assertion, executor)
	if err != nil {
		return env, err
	}
	env.AuthHeader = "Bearer " + token
	env.Auth = map[string]any{"projectId": creds.ProjectID}
	if env.BaseURL == "" {
		env.BaseURL = "https://aiplatform.googleapis.com"
	}
	if version != "" {
		var regions map[string]any
		if json.Unmarshal([]byte(version), &regions) == nil {
			env.UserSetting["vertexRegion"] = regions
		} else {
			env.UserSetting["vertexRegion"] = version
		}
	}
	return env, nil
}

func exchangeTaskPluginToken(ctx context.Context, endpoint, assertion string, executor host.Executor) (string, error) {
	if executor.Client == nil || executor.ValidateURL == nil {
		return "", errors.New("OAuth HTTP policy is required")
	}
	if err := executor.ValidateURL(endpoint); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := *executor.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return "", errors.New("Vertex token exchange failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("Vertex token exchange rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	var value struct {
		AccessToken string `json:"access_token"`
	}
	if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value.AccessToken) == "" || strings.ContainsAny(value.AccessToken, "\r\n") {
		return "", errors.New("invalid Vertex token response")
	}
	return value.AccessToken, nil
}
