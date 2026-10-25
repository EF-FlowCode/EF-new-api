package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// eflow: 一次性、动作绑定的二次验证凭证（Security Proof）。
// 对外契约（X-Security-Proof、claim 形状、签名键派生、错误）与上游 QuantumNous/new-api
// service/auth_token.go 一致；上游把一次性记录存在 auth_flows 表，eflow 没有该表，
// 改为 Redis（部署角色强制共享 Redis），开发/单测无 Redis 时回退进程内存。

const (
	SecurityProofTTL      = time.Minute
	securityProofTokenUse = "security_proof"
	authTokenIssuer       = "new-api"
	authTokenAudience     = "new-api-dashboard"
	// securityProofKeyPrefix holds the single-use record of an issued proof.
	securityProofKeyPrefix = "security_proof:"
	// securityProofStoreSlack keeps the record slightly longer than the JWT so
	// expiry is reported by the signature check rather than as "consumed".
	securityProofStoreSlack = 5 * time.Second
)

var (
	ErrAuthTokenInvalid = errors.New("authentication token is invalid")
	ErrAuthTokenExpired = errors.New("authentication token has expired")
	ErrProofScope       = errors.New("security proof scope mismatch")
	ErrProofMethod      = errors.New("security proof method mismatch")
	ErrProofContext     = errors.New("security proof context mismatch")
	ErrProofConsumed    = errors.New("security proof has already been consumed")
)

// AuthIdentity is the server-validated identity a proof is bound to. SessionID
// is the Redis session id for browser sessions, or "pat:<id>" for scoped access
// tokens. eflow has no auth/session versions; both stay 1 so the claim shape
// matches upstream, and revocation is enforced by the auth middleware itself.
type AuthIdentity struct {
	UserID          int
	SessionID       string
	UserAuthVersion int64
	SessionVersion  int64
}

func NewAuthIdentity(userID int, sessionID string) AuthIdentity {
	return AuthIdentity{UserID: userID, SessionID: sessionID, UserAuthVersion: 1, SessionVersion: 1}
}

func (identity AuthIdentity) valid() bool {
	return identity.UserID > 0 && identity.SessionID != "" && identity.UserAuthVersion > 0 && identity.SessionVersion > 0
}

type authClaims struct {
	TokenUse        string   `json:"token_use"`
	SessionID       string   `json:"sid"`
	UserAuthVersion int64    `json:"uv"`
	SessionVersion  int64    `json:"sv"`
	Method          string   `json:"method,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	ContextHash     string   `json:"context_hash,omitempty"`
	jwt.RegisteredClaims
}

// authSigningKey derives a purpose-specific key from SESSION_SECRET exactly as upstream does.
func authSigningKey(purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, _ = mac.Write([]byte("new-api/auth/" + purpose + "/v1"))
	return mac.Sum(nil)
}

func IssueSecurityProof(identity AuthIdentity, method string, binding VerificationBinding) (string, int64, error) {
	method = strings.TrimSpace(method)
	if !identity.valid() || method == "" || binding.Scope == "" || binding.ContextHash == "" {
		return "", 0, ErrAuthTokenInvalid
	}
	now := time.Now()
	expiresAt := now.Add(SecurityProofTTL).Truncate(time.Second)
	proofID := uuid.NewString()
	if err := proofStore().put(proofID, proofRecord(identity, binding), time.Until(expiresAt)+securityProofStoreSlack); err != nil {
		return "", 0, err
	}
	claims := authClaims{
		TokenUse:        securityProofTokenUse,
		SessionID:       identity.SessionID,
		UserAuthVersion: identity.UserAuthVersion,
		SessionVersion:  identity.SessionVersion,
		Method:          method,
		Scopes:          []string{binding.Scope},
		ContextHash:     binding.ContextHash,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authTokenIssuer,
			Subject:   strconv.Itoa(identity.UserID),
			Audience:  jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        proofID,
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(authSigningKey(securityProofTokenUse))
	return signed, expiresAt.Unix(), err
}

func proofRecord(identity AuthIdentity, binding VerificationBinding) string {
	return strconv.Itoa(identity.UserID) + "|" + identity.SessionID + "|" + binding.Scope
}

func verifySecurityProof(raw string, identity AuthIdentity, binding VerificationBinding) (*authClaims, error) {
	claims, err := parseAuthClaims(raw, securityProofTokenUse, authSigningKey(securityProofTokenUse))
	if err != nil {
		return nil, err
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID != identity.UserID || claims.SessionID != identity.SessionID || claims.UserAuthVersion != identity.UserAuthVersion || claims.SessionVersion != identity.SessionVersion {
		return nil, ErrAuthTokenInvalid
	}
	if len(claims.Scopes) != 1 || claims.Scopes[0] != binding.Scope {
		return nil, ErrProofScope
	}
	if claims.ContextHash == "" {
		return nil, ErrAuthTokenInvalid
	}
	if !hmac.Equal([]byte(claims.ContextHash), []byte(binding.ContextHash)) {
		return nil, ErrProofContext
	}
	return claims, nil
}

// consumeSecurityProof atomically removes the single-use record. It reports
// ErrProofConsumed when the proof was already used (or never issued here).
func consumeSecurityProof(claims *authClaims, identity AuthIdentity) error {
	expected := proofRecord(identity, VerificationBinding{Scope: claims.Scopes[0]})
	stored, ok, err := proofStore().take(claims.ID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrProofConsumed
	}
	if stored != expected {
		return ErrAuthTokenInvalid
	}
	return nil
}

func parseAuthClaims(raw, expectedUse string, key []byte) (*authClaims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrAuthTokenInvalid
	}
	claims := &authClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected signing method", ErrAuthTokenInvalid)
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(authTokenIssuer), jwt.WithAudience(authTokenAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrAuthTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrAuthTokenInvalid, err)
	}
	if !parsed.Valid || claims.TokenUse != expectedUse || claims.ID == "" || claims.IssuedAt == nil || claims.NotBefore == nil {
		return nil, ErrAuthTokenInvalid
	}
	return claims, nil
}

// singleUseStore records issued proofs so each can be consumed at most once.
type singleUseStore interface {
	put(id, value string, ttl time.Duration) error
	take(id string) (string, bool, error)
}

func proofStore() singleUseStore {
	if common.RedisEnabled && common.RDB != nil {
		return redisSingleUseStore{prefix: securityProofKeyPrefix}
	}
	return memorySecurityProofs
}

type redisSingleUseStore struct{ prefix string }

func (s redisSingleUseStore) put(id, value string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ok, err := common.RDB.SetNX(ctx, s.prefix+id, value, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrAuthTokenInvalid
	}
	return nil
}

func (s redisSingleUseStore) take(id string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	value, err := takeScript.Run(ctx, common.RDB, []string{s.prefix + id}).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

// takeScript is GET+DEL in one atomic step; GETDEL needs Redis 6.2+.
var takeScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if value then
	redis.call('DEL', KEYS[1])
end
return value
`)

// memorySingleUseStore backs proofs only for single-process development and tests.
type memorySingleUseStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
}

type memoryEntry struct {
	value     string
	expiresAt time.Time
}

var memorySecurityProofs = &memorySingleUseStore{entries: map[string]memoryEntry{}}

func (s *memorySingleUseStore) put(id, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
	if _, exists := s.entries[id]; exists {
		return ErrAuthTokenInvalid
	}
	s.entries[id] = memoryEntry{value: value, expiresAt: now.Add(ttl)}
	return nil
}

func (s *memorySingleUseStore) take(id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return "", false, nil
	}
	delete(s.entries, id)
	if time.Now().After(entry.expiresAt) {
		return "", false, nil
	}
	return entry.value, true, nil
}
