package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func useRedisProofStore(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousRDB, previousEnabled })
	return server
}

func testBinding(t *testing.T, scope string, context string) VerificationBinding {
	t.Helper()
	operation := VerificationOperation{Scope: scope}
	if context != "" {
		operation.Context = []byte(context)
	}
	binding, err := BindVerificationOperation(operation)
	require.NoError(t, err)
	return binding
}

func TestSecurityProofIsSingleUseInRedis(t *testing.T) {
	server := useRedisProofStore(t)
	identity := NewAuthIdentity(7, "session:abc")
	binding := testBinding(t, VerificationScopeChannelKeyRead, `{"channel_id":3}`)

	token, expiresAt, err := IssueSecurityProof(identity, VerificationMethodTwoFA, binding)
	require.NoError(t, err)
	require.Greater(t, expiresAt, time.Now().Unix())
	require.Len(t, server.Keys(), 1, "one single-use record per issued proof")

	claims, err := verifySecurityProof(token, identity, binding)
	require.NoError(t, err)
	require.NoError(t, consumeSecurityProof(claims, identity))
	require.ErrorIs(t, consumeSecurityProof(claims, identity), ErrProofConsumed)
	require.Empty(t, server.Keys())

	_, err = verifySecurityProof(token, NewAuthIdentity(7, "session:other"), binding)
	require.ErrorIs(t, err, ErrAuthTokenInvalid)
	_, err = verifySecurityProof(token, NewAuthIdentity(8, "session:abc"), binding)
	require.ErrorIs(t, err, ErrAuthTokenInvalid)
	_, err = verifySecurityProof(token, identity, testBinding(t, VerificationScopeChannelKeyRead, `{"channel_id":4}`))
	require.ErrorIs(t, err, ErrProofContext)
	_, err = verifySecurityProof(token, identity, testBinding(t, VerificationScopeAccountDelete, ""))
	require.ErrorIs(t, err, ErrProofScope)
}

func TestSecurityProofExpiresAfterOneMinute(t *testing.T) {
	identity := NewAuthIdentity(7, "session:abc")
	binding := testBinding(t, VerificationScopeAccountDelete, "")
	past := time.Now().Add(-2 * time.Minute)
	claims := authClaims{
		TokenUse: securityProofTokenUse, SessionID: identity.SessionID, UserAuthVersion: 1, SessionVersion: 1,
		Method: VerificationMethodPassword, Scopes: []string{binding.Scope}, ContextHash: binding.ContextHash,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: authTokenIssuer, Subject: strconv.Itoa(identity.UserID), Audience: jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(past.Add(SecurityProofTTL)), NotBefore: jwt.NewNumericDate(past), IssuedAt: jwt.NewNumericDate(past), ID: "expired",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(authSigningKey(securityProofTokenUse))
	require.NoError(t, err)
	_, err = verifySecurityProof(token, identity, binding)
	require.ErrorIs(t, err, ErrAuthTokenExpired)

	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("not-the-server-key"))
	require.NoError(t, err)
	_, err = verifySecurityProof(forged, identity, binding)
	require.ErrorIs(t, err, ErrAuthTokenInvalid)
}

func TestMemoryProofStoreExpiresEntries(t *testing.T) {
	store := &memorySingleUseStore{entries: map[string]memoryEntry{}}
	require.NoError(t, store.put("a", "v", time.Millisecond))
	require.ErrorIs(t, store.put("a", "v", time.Minute), ErrAuthTokenInvalid, "ids are never overwritten")
	time.Sleep(5 * time.Millisecond)
	_, ok, err := store.take("a")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestBindVerificationOperationNormalizesContexts(t *testing.T) {
	a := testBinding(t, VerificationScopeAccessTokenGenerate, `{"scopes":["user:read","profile:read","user:read"],"expires_at":0}`)
	b := testBinding(t, VerificationScopeAccessTokenGenerate, `{"expires_at":0,"scopes":["profile:read","user:read"]}`)
	require.Equal(t, a, b, "scope sets compare independently of order and duplicates")

	for _, invalid := range []VerificationOperation{
		{Scope: VerificationScopeChannelKeyRead, Context: []byte(`{"channel_id":0}`)},
		{Scope: VerificationScopeChannelKeyRead, Context: []byte(`{"channel_id":1,"extra":true}`)},
		{Scope: VerificationScopeAccountDelete, Context: []byte(`{"user_id":1}`)},
		{Scope: VerificationScopeAdminUserManage, Context: []byte(`{"user_id":1,"action":"add_quota"}`)},
		{Scope: VerificationScopeAdminUserManage, Context: []byte(`{"user_id":1,"action":"set_role"}`)},
		{Scope: VerificationScopeAdminUserManage, Context: []byte(`{"user_id":1,"action":"set_role","role":42}`)},
		{Scope: VerificationScopeAccessTokenRevoke, Context: []byte(`{"token_id":1,"legacy":true}`)},
	} {
		_, err := BindVerificationOperation(invalid)
		require.ErrorIs(t, err, ErrVerificationContextInvalid, "%s %s", invalid.Scope, invalid.Context)
	}
	_, err := BindVerificationOperation(VerificationOperation{Scope: "unknown.scope"})
	require.ErrorIs(t, err, ErrProofScope)

	// eflow's set_role binds the exact target role, including support/finance.
	support := testBinding(t, VerificationScopeAdminUserManage, `{"user_id":1,"action":"set_role","role":5}`)
	finance := testBinding(t, VerificationScopeAdminUserManage, `{"user_id":1,"action":"set_role","role":7}`)
	require.NotEqual(t, support.ContextHash, finance.ContextHash)
}

func TestAccessTokensCannotObtainTokenManagementProofs(t *testing.T) {
	identity := NewAuthIdentity(7, AccessTokenSessionPrefix+"3")
	previous := lookupAccessTokenScopes
	lookupAccessTokenScopes = func(AuthIdentity) ([]string, bool) {
		return []string{"account_security:write", "user:write"}, true
	}
	t.Cleanup(func() { lookupAccessTokenScopes = previous })

	require.NoError(t, requireAccessTokenVerificationScope(identity, VerificationScopeAccountDelete))
	require.NoError(t, requireAccessTokenVerificationScope(identity, VerificationScopeAdminUserManage))
	require.ErrorIs(t, requireAccessTokenVerificationScope(identity, VerificationScopeChannelKeyRead), ErrVerificationForbidden)
	require.ErrorIs(t, requireAccessTokenVerificationScope(identity, VerificationScopeAccessTokenGenerate), ErrVerificationForbidden)
	require.NoError(t, requireAccessTokenVerificationScope(NewAuthIdentity(7, "session:abc"), VerificationScopeAccessTokenGenerate))
}

func TestSessionMethodRequiresRecentBrowserSignIn(t *testing.T) {
	now := time.Now()
	require.True(t, StepUpContext{LoginAt: now.Add(-time.Minute).Unix()}.recentLogin(now))
	require.False(t, StepUpContext{LoginAt: now.Add(-RecentLoginWindow - time.Second).Unix()}.recentLogin(now))
	require.False(t, StepUpContext{AccessToken: true, LoginAt: now.Unix()}.recentLogin(now))
	require.False(t, StepUpContext{}.recentLogin(now))
}

// Every verification scope must say whether (and with which grant) an access
// token may obtain it; unlisted scopes would silently be denied.
func TestAccessTokenVerificationScopesAreAllListed(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "security_verification.go", nil, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if !strings.HasPrefix(name.Name, "VerificationScope") {
					continue
				}
				value := spec.(*ast.ValueSpec).Values[0].(*ast.BasicLit).Value
				scope := strings.Trim(value, `"`)
				_, listed := accessTokenScopeForVerification[scope]
				require.True(t, listed, "%s (%s) is missing from accessTokenScopeForVerification", name.Name, scope)
			}
		}
	}
}
