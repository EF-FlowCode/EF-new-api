package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"

	"github.com/gin-gonic/gin"
)

// AccessTokenExpiresAtHeader is an eflow extension: responses to requests made
// with a scoped access token carry its expiry (unix seconds, 0 = never), so
// automation such as admin-api can warn before the token stops working.
const AccessTokenExpiresAtHeader = "X-Access-Token-Expires-At"

// accessTokenLookup is the outcome of authenticating a dashboard request with
// an access token: a scoped token (token != nil) or a legacy users.access_token.
type accessTokenLookup struct {
	user   *model.User
	token  *model.UserAccessToken
	legacy bool
	ref    string
}

// accessTokenAuthError carries the HTTP status and stable code of a rejected token.
type accessTokenAuthError struct {
	status  int
	code    string
	message string
}

func (e *accessTokenAuthError) Error() string { return e.code }

var (
	errAccessTokenInvalid = &accessTokenAuthError{http.StatusUnauthorized, "access_token_invalid", "Unauthorized, the access token is invalid"}
	errAccessTokenExpired = &accessTokenAuthError{http.StatusUnauthorized, "ACCESS_TOKEN_EXPIRED", "Unauthorized, the access token has expired"}
	errLegacyTokenRetired = &accessTokenAuthError{http.StatusUnauthorized, "ACCESS_TOKEN_LEGACY_RETIRED", "Legacy access tokens have been retired. Create a new access token in security settings"}
)

// lookupAccessToken authenticates the Authorization header value. Scoped
// tokens start with nap_; anything else is treated as a legacy token.
func lookupAccessToken(raw string, now int64) (*accessTokenLookup, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "Bearer "))
	if raw == "" {
		return nil, errAccessTokenInvalid
	}
	if strings.HasPrefix(raw, model.AccessTokenPrefix) {
		ref := model.AccessTokenFingerprint(raw)
		token, err := model.FindUserAccessTokenByHash(ref)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", model.ErrDatabase, err)
		}
		if token == nil {
			return nil, errAccessTokenInvalid
		}
		if token.Expired(now) {
			return nil, errAccessTokenExpired
		}
		user, err := model.GetUserById(token.UserId, false)
		if err != nil {
			return nil, errAccessTokenInvalid
		}
		return &accessTokenLookup{user: user, token: token, ref: ref}, nil
	}
	user, err := model.ValidateAccessToken(raw)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Username == "" {
		return nil, errAccessTokenInvalid
	}
	if model.LegacyAccessTokensRetired(now) {
		return nil, errLegacyTokenRetired
	}
	return &accessTokenLookup{user: user, legacy: true, ref: model.AccessTokenFingerprint(raw)}, nil
}

func rejectAccessToken(c *gin.Context, err *accessTokenAuthError) {
	c.Header("Cache-Control", "no-store")
	logger.LogWarn(c, fmt.Sprintf("authentication rejected code=%s instance=%s path=%s", err.code, common.NodeName, c.Request.URL.Path))
	c.JSON(err.status, gin.H{"success": false, "code": err.code, "message": err.message})
	c.Abort()
}

// enforceAccessTokenRoute applies the route's access token rule (fail closed:
// scoped tokens cannot call undeclared routes). Legacy tokens keep their full
// access during the transition, except on session-only routes.
func enforceAccessTokenRoute(c *gin.Context, lookup *accessTokenLookup, role int) bool {
	key := c.Request.Method + " " + c.FullPath()
	rule, declared := AccessTokenRouteRule(key)
	if accessTokenRouteProbe != nil && accessTokenRouteProbe(key, declared) {
		c.AbortWithStatus(http.StatusNoContent)
		return false
	}
	var rejection *accessTokenAuthError
	switch {
	case declared && rule.kind == accessTokenRuleSession:
		rejection = &accessTokenAuthError{http.StatusForbidden, "AUTH_SESSION_REQUIRED", "This action requires a browser sign-in."}
	case lookup.legacy:
		return true
	case !declared:
		common.SysError("access token route is not declared: " + key)
		rejection = &accessTokenAuthError{http.StatusForbidden, "ACCESS_TOKEN_ROUTE_UNDECLARED", "Unauthorized, insufficient privileges"}
	case rule.kind == accessTokenRuleAny, service.AccessTokenScopeGranted(lookup.token.GetScopes(), rule.scope, role):
		return true
	default:
		rejection = &accessTokenAuthError{http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED", "The access token lacks the required permission: " + rule.scope}
	}
	recordAccessTokenUse(c, lookup, rejection.code)
	rejectAccessToken(c, rejection)
	return false
}

// accessTokenRouteProbe lets the route coverage test observe every dashboard
// route an access token reaches; returning true stops the request there.
var accessTokenRouteProbe func(key string, declared bool) bool

// setAccessTokenContext exposes the token grant to handlers and step-up.
func setAccessTokenContext(c *gin.Context, lookup *accessTokenLookup) {
	c.Set("access_token_legacy", lookup.legacy)
	c.Set("access_token_ref", lookup.ref)
	if lookup.legacy {
		c.Set("access_token_id", 0)
		c.Set("access_token_scopes", []string{})
		return
	}
	c.Set("access_token_id", lookup.token.Id)
	c.Set("access_token_scopes", lookup.token.GetScopes())
	c.Header(AccessTokenExpiresAtHeader, strconv.FormatInt(lookup.token.ExpiresAt, 10))
	touchAccessToken(lookup.token.Id, c.ClientIP())
}

var accessTokenTouches sync.Map // token id -> unix seconds of the last touch

// touchAccessToken records the last use at most once a minute per token per process.
func touchAccessToken(id int, ip string) {
	now := time.Now().Unix()
	if last, ok := accessTokenTouches.Load(id); ok && now-last.(int64) < 60 {
		return
	}
	accessTokenTouches.Store(id, now)
	go func() {
		if err := model.TouchUserAccessToken(id, ip, now); err != nil {
			common.SysError("touch access token: " + err.Error())
		}
	}()
}

// accessTokenReadAudits throttles read audits: one row per token and route
// per accessTokenReadAuditInterval, so polling automation does not flood the
// permanent audit table. Writes are always recorded.
var (
	accessTokenReadAudits        sync.Map // "ref route" -> unix seconds
	accessTokenReadAuditInterval = int64(10 * 60)
)

// finishAccessTokenAudit records a completed access-token request unless the
// staff write fallback (or the handler) already recorded it.
func finishAccessTokenAudit(c *gin.Context, lookup *accessTokenLookup) {
	if common.GetContextKeyBool(c, constant.ContextKeyAuditLogged) {
		return
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		key := lookup.ref + " " + c.FullPath()
		now := time.Now().Unix()
		if last, ok := accessTokenReadAudits.Load(key); ok && now-last.(int64) < accessTokenReadAuditInterval {
			return
		}
		accessTokenReadAudits.Store(key, now)
	}
	recordAccessTokenUse(c, lookup, "")
}

func recordAccessTokenUse(c *gin.Context, lookup *accessTokenLookup, failureCode string) {
	params := map[string]any{"legacy": lookup.legacy}
	if lookup.token != nil {
		params["token_id"] = lookup.token.Id
		params["token_name"] = lookup.token.Name
	}
	status := c.Writer.Status()
	success := failureCode == "" && status < http.StatusBadRequest
	if failureCode != "" {
		params["failure_reason"] = failureCode
		status = http.StatusForbidden
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId:     lookup.user.Id,
		Username:   lookup.user.Username,
		ActorRole:  lookup.user.Role,
		Category:   model.AuditCategoryAccessToken,
		Action:     "access_token.use",
		TokenRef:   lookup.ref,
		AuthMethod: "access_token",
		Status:     status,
		Success:    success,
		Content:    c.Request.Method + " " + c.FullPath(),
		Other:      model.AuditOther{Op: &model.AuditOperation{Action: "access_token.use", Params: params}},
	})
}

func asAccessTokenAuthError(err error) (*accessTokenAuthError, bool) {
	var authErr *accessTokenAuthError
	ok := errors.As(err, &authErr)
	return authErr, ok
}

// SetAccessTokenRouteProbeForTest installs probe and returns a restore func.
func SetAccessTokenRouteProbeForTest(probe func(key string, declared bool) bool) func() {
	previous := accessTokenRouteProbe
	accessTokenRouteProbe = probe
	return func() { accessTokenRouteProbe = previous }
}
