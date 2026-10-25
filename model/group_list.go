package model

import (
	"database/sql/driver"
	"fmt"
	"sort"
	"strings"

	"eflow.local/new-api/common"
)

// GroupList stores a normalized JSON string while exposing a JSON array to APIs.
type GroupList string

func NewGroupList(groups []string) GroupList {
	normalized := normalizeGroupList(groups)
	if len(normalized) == 0 {
		return ""
	}
	data, _ := common.Marshal(normalized)
	return GroupList(data)
}

func (g GroupList) Values() []string {
	groups := []string{}
	if strings.TrimSpace(string(g)) == "" {
		return groups
	}
	if err := common.Unmarshal([]byte(g), &groups); err != nil {
		return []string{}
	}
	return normalizeGroupList(groups)
}

func (g GroupList) MarshalJSON() ([]byte, error) {
	return common.Marshal(g.Values())
}

func (g *GroupList) UnmarshalJSON(data []byte) error {
	var groups []string
	if err := common.Unmarshal(data, &groups); err != nil {
		return err
	}
	*g = NewGroupList(groups)
	return nil
}

func (g *GroupList) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		*g = ""
		return nil
	case string:
		return g.scanString(typed)
	case []byte:
		return g.scanString(string(typed))
	default:
		return fmt.Errorf("unsupported group list type %T", value)
	}
}

func (g GroupList) Value() (driver.Value, error) {
	return string(g), nil
}

func (g *GroupList) scanString(value string) error {
	if strings.TrimSpace(value) == "" {
		*g = ""
		return nil
	}
	var groups []string
	if err := common.Unmarshal([]byte(value), &groups); err != nil {
		return err
	}
	*g = NewGroupList(groups)
	return nil
}

func normalizeGroupList(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	sort.Strings(normalized)
	return normalized
}
