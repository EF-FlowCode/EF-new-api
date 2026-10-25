package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
)

// eflow: 访问令牌管理接口，路径与返回结构照抄上游 QuantumNous/new-api controller/access_token.go。
// 全部只允许浏览器会话（路由规则表为 session-only），创建、改权限、撤销需要绑定具体
// 授权内容的一次性验证凭证，改名不需要。

// accessTokenMinLifetimeSecs is the shortest expiry a new access token may have.
const accessTokenMinLifetimeSecs = 60 * 60

type accessTokenItem struct {
	model.UserAccessToken
	Scopes []string `json:"scopes"`
}

type accessTokenCreateRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"expires_at"`
}

// accessTokenUpdateRequest renames a token; a present scopes field also replaces its grant.
type accessTokenUpdateRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func newAccessTokenItem(token *model.UserAccessToken) accessTokenItem {
	scopes := token.GetScopes()
	if scopes == nil {
		scopes = []string{}
	}
	return accessTokenItem{UserAccessToken: *token, Scopes: scopes}
}

// normalizeAccessTokenName returns the trimmed name, or false when it is empty
// or longer than the 64-character column.
func normalizeAccessTokenName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	count := utf8.RuneCountInString(name)
	return name, count > 0 && count <= 64
}

// requireBrowserSession rejects access-token callers; token management always
// needs a browser sign-in.
func requireBrowserSession(c *gin.Context) (service.AuthIdentity, bool) {
	if c.GetBool("use_access_token") {
		writeSecurityStatusError(c, http.StatusForbidden, "AUTH_SESSION_REQUIRED", "This action requires a browser sign-in.")
		return service.AuthIdentity{}, false
	}
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		writeSecurityStatusError(c, http.StatusUnauthorized, "SECURITY_PROOF_INVALID", "The current sign-in method does not support security verification")
		return service.AuthIdentity{}, false
	}
	return identity, true
}

func writeAccessTokenNotFound(c *gin.Context) {
	writeSecurityStatusError(c, http.StatusNotFound, "ACCESS_TOKEN_NOT_FOUND", model.ErrAccessTokenNotFound.Error())
}

func writeAccessTokenScopeError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrAccessTokenScopeForbidden) {
		writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_SCOPE_FORBIDDEN", "You cannot grant one or more of these permissions.")
		return
	}
	writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_SCOPE_INVALID", "Select at least one valid permission.")
}

func ListAccessTokens(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	tokens, err := model.ListUserAccessTokens(identity.UserID)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	items := make([]accessTokenItem, 0, len(tokens))
	for i := range tokens {
		items = append(items, newAccessTokenItem(&tokens[i]))
	}
	var legacy gin.H
	if !model.LegacyAccessTokensRetired(common.GetTimestamp()) {
		status, err := model.GetLegacyAccessTokenStatus(identity.UserID)
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		if status.Exists {
			legacy = gin.H{
				"token_hint":   status.TokenHint,
				"token_ref":    status.TokenRef,
				"last_used_at": status.LastUsedAt,
				"last_used_ip": status.LastUsedIp,
				"retire_at":    model.LegacyAccessTokenRetireAt(),
			}
		}
	}
	common.ApiSuccess(c, gin.H{"items": items, "legacy": legacy})
}

func GetAccessTokenCatalog(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	common.ApiSuccess(c, gin.H{
		"groups":              service.AccessTokenCatalog(c.GetInt("role")),
		"max_tokens":          service.AccessTokenMaxPerUser,
		"default_expiry_days": service.AccessTokenDefaultExpiryDays,
	})
}

// GetAccessTokenScopes returns the unfiltered scope labels used to describe stored grants.
func GetAccessTokenScopes(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	common.ApiSuccess(c, gin.H{"resources": service.AccessTokenScopeDictionary()})
}

// CreateAccessToken validates the whole request before consuming the proof, so
// a rejected grant never burns the user's verification.
func CreateAccessToken(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	var req accessTokenCreateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		writeSecurityStatusError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body.")
		return
	}
	name, ok := normalizeAccessTokenName(req.Name)
	if !ok {
		writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_NAME_INVALID", "Access token name must be 1 to 64 characters.")
		return
	}
	now := common.GetTimestamp()
	if req.ExpiresAt != 0 && req.ExpiresAt < now+accessTokenMinLifetimeSecs {
		writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_EXPIRY_INVALID", "Access token expiry must be at least one hour from now.")
		return
	}
	scopes, err := service.NormalizeAccessTokenScopes(c.GetInt("role"), req.Scopes)
	if err != nil {
		writeAccessTokenScopeError(c, err)
		return
	}
	proofContext, err := common.Marshal(service.AccessTokenGenerateContext{Scopes: scopes, ExpiresAt: req.ExpiresAt})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenGenerate, Context: proofContext}) == nil {
		return
	}
	suffix, err := common.GenerateRandomCharsKey(43)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	raw := model.AccessTokenPrefix + suffix
	token := &model.UserAccessToken{
		Name:      name,
		TokenHash: model.AccessTokenFingerprint(raw),
		TokenHint: model.AccessTokenHint(raw),
		ExpiresAt: req.ExpiresAt,
		CreatedAt: now,
	}
	if err := token.SetScopes(scopes); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if err := model.CreateUserAccessToken(identity.UserID, token, service.AccessTokenMaxPerUser); err != nil {
		if errors.Is(err, model.ErrAccessTokenLimit) {
			writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_LIMIT", "You have reached the access token limit.")
			return
		}
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "access_token.generate", map[string]any{
		"token_id":   token.Id,
		"name":       token.Name,
		"scopes":     scopes,
		"expires_at": token.ExpiresAt,
		"token_ref":  token.TokenHash,
	})
	common.ApiSuccess(c, gin.H{"token": raw, "item": newAccessTokenItem(token)})
}

// UpdateAccessToken renames a token without verification. Changing the grant
// is checked in full before the proof is consumed, like creation.
func UpdateAccessToken(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeAccessTokenNotFound(c)
		return
	}
	var req accessTokenUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		writeSecurityStatusError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body.")
		return
	}
	name, ok := normalizeAccessTokenName(req.Name)
	if !ok {
		writeSecurityStatusError(c, http.StatusBadRequest, "ACCESS_TOKEN_NAME_INVALID", "Access token name must be 1 to 64 characters.")
		return
	}
	var scopes []string
	var previous *model.UserAccessToken
	if req.Scopes != nil {
		previous, err = model.GetUserAccessToken(identity.UserID, id)
		if errors.Is(err, model.ErrAccessTokenNotFound) {
			writeAccessTokenNotFound(c)
			return
		}
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		scopes, err = service.NormalizeAccessTokenScopes(c.GetInt("role"), req.Scopes)
		if err != nil {
			writeAccessTokenScopeError(c, err)
			return
		}
		proofContext, err := common.Marshal(service.AccessTokenUpdateContext{TokenID: id, Scopes: scopes})
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenUpdate, Context: proofContext}) == nil {
			return
		}
	}
	token, err := model.UpdateUserAccessToken(identity.UserID, id, name, scopes)
	if errors.Is(err, model.ErrAccessTokenNotFound) {
		writeAccessTokenNotFound(c)
		return
	}
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if previous == nil {
		recordUserSecurityAudit(c, identity.UserID, "access_token.rename", map[string]any{"token_id": token.Id, "name": token.Name})
		common.ApiSuccess(c, newAccessTokenItem(token))
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "access_token.update", map[string]any{
		"token_id":        token.Id,
		"name":            token.Name,
		"scopes":          scopes,
		"previous_scopes": previous.GetScopes(),
		"token_ref":       token.TokenHash,
	})
	common.ApiSuccess(c, newAccessTokenItem(token))
}

// DeleteAccessToken checks ownership before consuming the proof, so probing
// other users' token IDs never burns a verification.
func DeleteAccessToken(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeAccessTokenNotFound(c)
		return
	}
	if _, err := model.GetUserAccessToken(identity.UserID, id); err != nil {
		if errors.Is(err, model.ErrAccessTokenNotFound) {
			writeAccessTokenNotFound(c)
			return
		}
		writeSecurityOperationError(c, err)
		return
	}
	proofContext, err := common.Marshal(service.AccessTokenRevokeContext{TokenID: id})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenRevoke, Context: proofContext}) == nil {
		return
	}
	token, err := model.DeleteUserAccessToken(identity.UserID, id)
	if errors.Is(err, model.ErrAccessTokenNotFound) {
		writeAccessTokenNotFound(c)
		return
	}
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "access_token.revoke", map[string]any{"token_id": token.Id, "name": token.Name, "token_ref": token.TokenHash})
	common.ApiSuccess(c, nil)
}

// RevokeLegacyAccessToken removes the pre-scope users.access_token credential.
func RevokeLegacyAccessToken(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	status, err := model.GetLegacyAccessTokenStatus(identity.UserID)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if !status.Exists {
		writeAccessTokenNotFound(c)
		return
	}
	proofContext, err := common.Marshal(service.AccessTokenRevokeContext{Legacy: true})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenRevoke, Context: proofContext}) == nil {
		return
	}
	ref, err := model.RevokeLegacyAccessToken(identity.UserID)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if ref != "" {
		recordUserSecurityAudit(c, identity.UserID, "access_token.revoke", map[string]any{"legacy": true, "token_ref": ref})
	}
	common.ApiSuccess(c, nil)
}

// accessTokenGrants reports whether the request may use scope: browser
// sessions and legacy tokens are not narrowed; scoped tokens need the scope
// within the owner's current role.
func accessTokenGrants(c *gin.Context, scope string) bool {
	if !c.GetBool("use_access_token") || c.GetBool("access_token_legacy") {
		return true
	}
	scopes, _ := c.Get("access_token_scopes")
	granted, _ := scopes.([]string)
	return service.AccessTokenScopeGranted(granted, scope, c.GetInt("role"))
}
