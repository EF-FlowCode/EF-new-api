// eflow patch: 共享身份 cookie 签发 / 验证 helper。
//
// 给同域子站（image-web / video-web / diagram-web 等）一个独立的"我是谁"凭证，
// 让子站不再需要每次都调 /api/user/self 反查 user_id（避开 CriticalRateLimit 20/20min）。
//
// 协议：HS256 JWT，HMAC-SHA256 签名，base64url 编码。
// payload: { "uid": <int>, "iat": <unix>, "exp": <unix>, "sid": <会话公开 ID>, "ver": 2 }
// 子站只验签 + 看 uid/exp（不认识的字段忽略），不需要再访问 new-api。
// eflow: v2 绑定签发它的主会话：主站请求发现 cookie 属于其他会话就重签，主会话失效时
// 清掉 cookie，子站随之视为未登录（会话吊销后旧 JWT 不再被续用）。
//
// 密钥来自环境变量 EFLOW_AUTH_SECRET，所有相关容器共享同一个值。
// 未配置时签发返回空串、验证恒为 false（即"功能关闭"），不影响主流程。
package common

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"time"
)

const (
	EflowAuthCookieName = "e-flowcode-auth"
	EflowAuthTTLSeconds = int64(30 * 24 * 3600)
)

// 进程启动时读取一次。轮换 secret 需要重启 new-api。
var eflowAuthSecret = strings.TrimSpace(os.Getenv("EFLOW_AUTH_SECRET"))

// 固定 header（HS256 + JWT），预先编码避免每次签发重复构造
var eflowAuthHeaderEncoded = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

type eflowClaims struct {
	Uid int    `json:"uid"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Sid string `json:"sid,omitempty"`
	Ver int    `json:"ver,omitempty"`
}

func EflowAuthEnabled() bool {
	return eflowAuthSecret != ""
}

// IssueEflowAuth 给指定 user_id 签发一个与主 session 同期的 30 天 JWT。
// 未配置 EFLOW_AUTH_SECRET 时返回空串（调用方应据此跳过 SetCookie）。
func IssueEflowAuth(userId int) (string, error) {
	return IssueEflowAuthForSession(userId, "")
}

// IssueEflowAuthForSession binds the token to the public id of the main session
// that issued it (empty when the session store has no server-side id).
func IssueEflowAuthForSession(userId int, sessionPublicID string) (string, error) {
	if eflowAuthSecret == "" {
		return "", nil
	}
	now := time.Now().Unix()
	body, err := Marshal(eflowClaims{
		Uid: userId,
		Iat: now,
		Exp: now + EflowAuthTTLSeconds,
		Sid: sessionPublicID,
		Ver: 2,
	})
	if err != nil {
		return "", err
	}
	bodyEncoded := base64.RawURLEncoding.EncodeToString(body)
	signingInput := eflowAuthHeaderEncoded + "." + bodyEncoded
	sig := eflowSign(signingInput)
	return signingInput + "." + sig, nil
}

func eflowSign(msg string) string {
	h := hmac.New(sha256.New, []byte(eflowAuthSecret))
	h.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// VerifyEflowAuth 校验 cookie 字符串：HMAC 通过 + 未过期 → 返回 (uid, true)。
// 主要给 lazy issue 路径用：判断现有 cookie 是否还需要刷新。
// 子站不用走 Go，子站有自己的 JS 实现。
func VerifyEflowAuth(token string) (int, bool) {
	claims, ok := parseEflowAuth(token)
	if !ok {
		return 0, false
	}
	return claims.Uid, true
}

// EflowAuthMatchesSession reports whether token is valid and was issued for
// userId's session sessionPublicID.
func EflowAuthMatchesSession(token string, userId int, sessionPublicID string) bool {
	claims, ok := parseEflowAuth(token)
	return ok && claims.Uid == userId && claims.Sid == sessionPublicID
}

func parseEflowAuth(token string) (eflowClaims, bool) {
	if eflowAuthSecret == "" || token == "" {
		return eflowClaims{}, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return eflowClaims{}, false
	}
	expected := eflowSign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return eflowClaims{}, false
	}
	bodyBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return eflowClaims{}, false
	}
	var claims eflowClaims
	if err := Unmarshal(bodyBytes, &claims); err != nil {
		return eflowClaims{}, false
	}
	if time.Now().Unix() >= claims.Exp {
		return eflowClaims{}, false
	}
	if claims.Uid <= 0 {
		return eflowClaims{}, false
	}
	return claims, true
}
