package passkey

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

var errSessionNotFound = errors.New("Passkey 会话不存在或已过期")

func SaveSessionData(c *gin.Context, key string, data *webauthn.SessionData) error {
	session := sessions.Default(c)
	if data == nil {
		session.Delete(key)
		return session.Save()
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	session.Set(key, string(payload))
	return session.Save()
}

func PopSessionData(c *gin.Context, key string) (*webauthn.SessionData, error) {
	session := sessions.Default(c)
	raw := session.Get(key)
	if raw == nil {
		return nil, errSessionNotFound
	}
	session.Delete(key)
	_ = session.Save()
	var data webauthn.SessionData
	switch value := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(value), &data); err != nil {
			return nil, err
		}
	case []byte:
		if err := json.Unmarshal(value, &data); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("Passkey 会话格式无效")
	}
	return &data, nil
}

// Flow is a WebAuthn ceremony waiting for its finish step. It lives in the
// server-side session and is bound to one step-up operation; FlowToken lets the
// client name the ceremony it is finishing.
type Flow struct {
	FlowToken     string               `json:"flow_token"`
	ExpiresAt     int64                `json:"expires_at"`
	SessionData   webauthn.SessionData `json:"session_data"`
	Scope         string               `json:"scope,omitempty"`
	ContextHash   string               `json:"context_hash,omitempty"`
	Authorization json.RawMessage      `json:"authorization,omitempty"`
}

// FlowTTL bounds how long a begun ceremony may be finished.
const FlowTTL = 5 * time.Minute

var errFlowInvalid = errors.New("Passkey 验证流程已失效，请重新开始")

// ErrFlowInvalid reports a missing, expired or mismatched ceremony.
func ErrFlowInvalid() error { return errFlowInvalid }

func IsFlowInvalid(err error) bool { return errors.Is(err, errFlowInvalid) }

// SaveFlow stores a ceremony under key and returns its flow token and expiry.
func SaveFlow(c *gin.Context, key string, flow Flow) (string, int64, error) {
	flow.FlowToken = uuid.NewString()
	flow.ExpiresAt = time.Now().Add(FlowTTL).Unix()
	payload, err := json.Marshal(flow)
	if err != nil {
		return "", 0, err
	}
	session := sessions.Default(c)
	session.Set(key, string(payload))
	if err := session.Save(); err != nil {
		return "", 0, err
	}
	return flow.FlowToken, flow.ExpiresAt, nil
}

// PopFlow removes the ceremony under key and returns it if flowToken names it
// and it has not expired. The ceremony is removed even when it does not match.
func PopFlow(c *gin.Context, key, flowToken string) (*Flow, error) {
	session := sessions.Default(c)
	raw, _ := session.Get(key).(string)
	session.Delete(key)
	if err := session.Save(); err != nil {
		return nil, err
	}
	if raw == "" || flowToken == "" {
		return nil, errFlowInvalid
	}
	var flow Flow
	if err := json.Unmarshal([]byte(raw), &flow); err != nil {
		return nil, errFlowInvalid
	}
	if flow.FlowToken != flowToken || time.Now().Unix() > flow.ExpiresAt {
		return nil, errFlowInvalid
	}
	return &flow, nil
}
