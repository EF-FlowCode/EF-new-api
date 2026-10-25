package common

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/securecookie"
	gsessions "github.com/gorilla/sessions"
)

// eflow: 会话存储从 cookie 挪到 Redis。浏览器里的 session cookie 只带签名的会话 ID，
// 会话内容放服务端，因此退出、改密、封号可以真正作废会话，一次性状态也无法拿旧 cookie 重放。
//
// 上线前签发的旧 cookie（整份会话值签名后放在 cookie 里）在首次出现时迁进 Redis，
// 会话 ID 由旧 cookie 值哈希得到，同一份旧 cookie 永远对应同一条 Redis 记录；
// 记录被作废后留下空值墓碑，旧 cookie 的副本不能再次迁移。旧 cookie 最长 30 天自然过期，
// 之后可以删掉迁移分支。

const (
	sessionKeyPrefix              = "session:"
	sessionUserKeyPrefix          = "session:user:"
	sessionRevokedBeforeKeyPrefix = "session:revoked_before:"
	sessionStoreTimeout           = 2 * time.Second
)

var revokeUserSessionsScript = redis.NewScript(`
local members = redis.call('SMEMBERS', KEYS[1])
local revoked = 0
for _, sid in ipairs(members) do
  if sid ~= ARGV[1] then
    redis.call('SET', ARGV[3] .. sid, '', 'EX', ARGV[2])
    redis.call('DEL', ARGV[3] .. 'active:' .. sid)
    redis.call('SREM', KEYS[1], sid)
    revoked = revoked + 1
  end
end
redis.call('SET', KEYS[2], ARGV[4], 'EX', ARGV[2])
return revoked
`)

type redisSessionStore struct {
	codecs  []securecookie.Codec
	options *gsessions.Options
}

// NewSessionStore 返回 gin 会话存储：启用 Redis 时用服务端会话，否则回退原 cookie 存储。
func NewSessionStore(options sessions.Options) sessions.Store {
	if !RedisEnabled || RDB == nil {
		store := cookie.NewStore([]byte(SessionSecret))
		store.Options(options)
		return store
	}
	store := &redisSessionStore{
		codecs: securecookie.CodecsFromPairs([]byte(SessionSecret)),
	}
	store.Options(options)
	return store
}

func (s *redisSessionStore) Options(options sessions.Options) {
	s.options = options.ToGorillaOptions()
	for _, codec := range s.codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(s.options.MaxAge)
		}
	}
}

func (s *redisSessionStore) Get(r *http.Request, name string) (*gsessions.Session, error) {
	return gsessions.GetRegistry(r).Get(s, name)
}

func (s *redisSessionStore) New(r *http.Request, name string) (*gsessions.Session, error) {
	session := gsessions.NewSession(s, name)
	opts := *s.options
	session.Options = &opts
	session.IsNew = true

	c, err := r.Cookie(name)
	if err != nil {
		return session, nil
	}
	var sid string
	if securecookie.DecodeMulti(name, c.Value, &sid, s.codecs...) == nil {
		values, err := loadSessionValues(sid)
		if err != nil {
			return session, err
		}
		if len(values) > 0 {
			session.ID = sid
			session.Values = values
			session.IsNew = false
		}
		return session, nil
	}

	legacy := make(map[any]any)
	if securecookie.DecodeMulti(name, c.Value, &legacy, s.codecs...) != nil {
		return session, nil
	}
	sid = legacySessionID(c.Value)
	values, err := s.migrateLegacySession(sid, legacy)
	if err != nil {
		return session, err
	}
	if len(values) > 0 {
		session.ID = sid
		session.Values = values
		session.IsNew = false
	}
	return session, nil
}

func (s *redisSessionStore) Save(r *http.Request, w http.ResponseWriter, session *gsessions.Session) error {
	if session.Options.MaxAge <= 0 || len(session.Values) == 0 {
		if session.ID != "" {
			if err := tombstoneSession(session.ID, s.ttl()); err != nil {
				return err
			}
			session.ID = ""
		}
		opts := *session.Options
		opts.MaxAge = -1
		http.SetCookie(w, gsessions.NewCookie(session.Name(), "", &opts))
		return nil
	}

	if session.ID == "" {
		session.ID = newSessionID()
	}
	payload, err := encodeSessionValues(session.Values)
	if err != nil {
		return err
	}
	ttl := time.Duration(session.Options.MaxAge) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	pipe := RDB.TxPipeline()
	pipe.Set(ctx, sessionKeyPrefix+session.ID, payload, ttl)
	if userID, ok := session.Values["id"].(int); ok && userID > 0 {
		userKey := sessionUserKeyPrefix + strconv.Itoa(userID)
		pipe.SAdd(ctx, userKey, session.ID)
		pipe.Expire(ctx, userKey, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, gsessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

func (s *redisSessionStore) ttl() time.Duration {
	return time.Duration(s.options.MaxAge) * time.Second
}

func (s *redisSessionStore) migrateLegacySession(sid string, legacy map[any]any) (map[any]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	if userID, ok := legacy["id"].(int); ok && userID > 0 {
		// 用户改密、被封或主动踢下线之后，尚未迁移的旧 cookie 一律作废。
		revoked, err := RDB.Exists(ctx, sessionRevokedBeforeKeyPrefix+strconv.Itoa(userID)).Result()
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		if revoked > 0 {
			return nil, nil
		}
	}
	payload, err := encodeSessionValues(legacy)
	if err != nil {
		return nil, err
	}
	created, err := RDB.SetNX(ctx, sessionKeyPrefix+sid, payload, s.ttl()).Result()
	if err != nil {
		return nil, fmt.Errorf("migrate session: %w", err)
	}
	if !created {
		return loadSessionValues(sid)
	}
	if userID, ok := legacy["id"].(int); ok && userID > 0 {
		userKey := sessionUserKeyPrefix + strconv.Itoa(userID)
		pipe := RDB.TxPipeline()
		pipe.SAdd(ctx, userKey, sid)
		pipe.Expire(ctx, userKey, s.ttl())
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, fmt.Errorf("migrate session: %w", err)
		}
	}
	return legacy, nil
}

func loadSessionValues(sid string) (map[any]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	payload, err := RDB.Get(ctx, sessionKeyPrefix+sid).Result()
	if errors.Is(err, redis.Nil) || (err == nil && payload == "") {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	values := make(map[any]any)
	if err := gob.NewDecoder(strings.NewReader(payload)).Decode(&values); err != nil {
		return nil, nil
	}
	return values, nil
}

func encodeSessionValues(values map[any]any) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(values); err != nil {
		return "", fmt.Errorf("encode session: %w", err)
	}
	return buf.String(), nil
}

// tombstoneSession 把会话改成空值而不是删除，防止同一份旧 cookie 再次迁移。
func tombstoneSession(sid string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	if err := RDB.Set(ctx, sessionKeyPrefix+sid, "", ttl).Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func newSessionID() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
}

func legacySessionID(cookieValue string) string {
	sum := sha256.Sum256([]byte("legacy-session:" + cookieValue))
	return strings.TrimRight(base32.StdEncoding.EncodeToString(sum[:]), "=")
}

// RegenerateSession 作废当前会话 ID，下次 Save 时换发新 ID，用于登录时防会话固定。
// 会话内容保留。cookie 存储没有服务端 ID，调用无副作用。
func RegenerateSession(session sessions.Session) error {
	holder, ok := session.(interface{ Session() *gsessions.Session })
	if !ok {
		return nil
	}
	inner := holder.Session()
	if inner == nil || inner.ID == "" {
		return nil
	}
	if RedisEnabled && RDB != nil {
		if err := tombstoneSession(inner.ID, time.Duration(inner.Options.MaxAge)*time.Second); err != nil {
			return err
		}
	}
	inner.ID = ""
	return nil
}

// CurrentSessionID 返回当前请求的服务端会话 ID；未登录或 cookie 存储时为空。
func CurrentSessionID(session sessions.Session) string {
	holder, ok := session.(interface{ Session() *gsessions.Session })
	if !ok {
		return ""
	}
	if inner := holder.Session(); inner != nil {
		return inner.ID
	}
	return ""
}

// RevokeUserSessions 作废该用户除 keepSessionID 以外的全部服务端会话，并拒绝其尚未迁移的旧 cookie。
// keepSessionID 为空表示全部作废。未启用 Redis 时 cookie 会话无法吊销，直接返回。
func RevokeUserSessions(userID int, keepSessionID string) error {
	if !RedisEnabled || RDB == nil || userID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	uid := strconv.Itoa(userID)
	ttlSeconds := int64(30 * 24 * time.Hour / time.Second)
	err := revokeUserSessionsScript.Run(ctx, RDB,
		[]string{sessionUserKeyPrefix + uid, sessionRevokedBeforeKeyPrefix + uid},
		keepSessionID, ttlSeconds, sessionKeyPrefix, time.Now().Unix(),
	).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

// sessionActiveKeyPrefix holds "unix|ip" of a session's last authenticated
// request (eflow: device list). It expires with the session.
const sessionActiveKeyPrefix = "session:active:"

// UserSessionRecord is one stored browser session of a user.
type UserSessionRecord struct {
	ID           string
	Values       map[any]any
	LastActiveAt int64
	LastActiveIP string
}

// SessionPublicID is the identifier shown to clients for a server session id.
// The raw id is a bearer secret (it is what the cookie carries) and is never exposed.
func SessionPublicID(sid string) string {
	return strings.ToLower(GenerateHMAC("session-public-id:" + sid))[:26]
}

// ListUserSessions returns the user's live sessions, dropping revoked members
// from the user's session set.
func ListUserSessions(userID int) ([]UserSessionRecord, error) {
	if !RedisEnabled || RDB == nil || userID <= 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	userKey := sessionUserKeyPrefix + strconv.Itoa(userID)
	sids, err := RDB.SMembers(ctx, userKey).Result()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	records := make([]UserSessionRecord, 0, len(sids))
	for _, sid := range sids {
		values, err := loadSessionValues(sid)
		if err != nil {
			return nil, err
		}
		if owner, _ := values["id"].(int); len(values) == 0 || owner != userID {
			RDB.SRem(ctx, userKey, sid)
			continue
		}
		record := UserSessionRecord{ID: sid, Values: values}
		if active, err := RDB.Get(ctx, sessionActiveKeyPrefix+sid).Result(); err == nil {
			at, ip, _ := strings.Cut(active, "|")
			record.LastActiveAt, _ = strconv.ParseInt(at, 10, 64)
			record.LastActiveIP = ip
		}
		records = append(records, record)
	}
	return records, nil
}

// RevokeUserSession revokes one session of the user; false when it is not theirs.
func RevokeUserSession(userID int, sid string) (bool, error) {
	if !RedisEnabled || RDB == nil || userID <= 0 || sid == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
	defer cancel()
	userKey := sessionUserKeyPrefix + strconv.Itoa(userID)
	member, err := RDB.SIsMember(ctx, userKey, sid).Result()
	if err != nil || !member {
		return false, err
	}
	if err := tombstoneSession(sid, 30*24*time.Hour); err != nil {
		return false, err
	}
	pipe := RDB.TxPipeline()
	pipe.SRem(ctx, userKey, sid)
	pipe.Del(ctx, sessionActiveKeyPrefix+sid)
	_, err = pipe.Exec(ctx)
	return err == nil, err
}

var sessionTouches = struct {
	sync.Mutex
	last map[string]int64
}{last: map[string]int64{}}

// TouchSession records a session's last activity at most once a minute per process.
func TouchSession(sid, ip string, ttl time.Duration) {
	if !RedisEnabled || RDB == nil || sid == "" {
		return
	}
	now := time.Now().Unix()
	sessionTouches.Lock()
	if now-sessionTouches.last[sid] < 60 {
		sessionTouches.Unlock()
		return
	}
	if len(sessionTouches.last) > 100000 {
		sessionTouches.last = map[string]int64{}
	}
	sessionTouches.last[sid] = now
	sessionTouches.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sessionStoreTimeout)
		defer cancel()
		if err := RDB.Set(ctx, sessionActiveKeyPrefix+sid, strconv.FormatInt(now, 10)+"|"+ip, ttl).Err(); err != nil {
			SysError("touch session: " + err.Error())
		}
	}()
}
