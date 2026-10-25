package model

import (
	"encoding/json"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/logmetadata"
)

var privateLogOtherKeys = logmetadata.PrivateKeys()

// projectLogOther also handles historical flat metadata. RawMessage preserves
// token counts and other integers without converting them through float64.
func projectLogOther(value string, role int) string {
	return encodeLogOther(value, role, false)
}

// legacyRejectReason is only enabled for API responses. Keeping it out of
// storage also prevents older user-log readers from exposing the private value.
func encodeLogOther(value string, role int, legacyRejectReason bool) string {
	if value == "" {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil || fields == nil {
		return "{}"
	}
	if role < common.RoleAdminUser {
		for _, key := range privateLogOtherKeys {
			delete(fields, key)
		}
		delete(fields, "admin_info")
		delete(fields, "audit_info")
		delete(fields, "root_info")
	} else {
		var admin map[string]json.RawMessage
		_ = json.Unmarshal(fields["admin_info"], &admin)
		if admin == nil {
			admin = make(map[string]json.RawMessage)
		}
		for _, key := range privateLogOtherKeys {
			if raw, ok := fields[key]; ok {
				if _, exists := admin[key]; !exists {
					admin[key] = raw
				}
				delete(fields, key)
			}
		}
		if len(admin) > 0 {
			fields["admin_info"], _ = json.Marshal(admin)
		}
		if legacyRejectReason {
			if reason, exists := admin["reject_reason"]; exists {
				fields["reject_reason"] = reason
			}
		}
		if role < common.RoleRootUser {
			delete(fields, "root_info")
		}
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// All log writers share this boundary so privileged legacy keys are stored in
// admin_info instead of being introduced into the public top level again.
func marshalLogOther(other map[string]any) string {
	return projectLogOther(common.MapToJsonStr(other), common.RoleRootUser)
}

func FormatAdminLogs(logs []*Log, role int) {
	for _, log := range logs {
		log.Other = encodeLogOther(log.Other, role, true)
	}
}
