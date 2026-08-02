package service

import (
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

// MatchErrorFilter 遍历规则，返回第一条匹配的规则。
// 匹配逻辑：所有非空条件 AND，同类条件内 OR。
func MatchErrorFilter(rules []dto.ErrorFilterRule, statusCode int, errorCode string, message string) (matched bool, rule *dto.ErrorFilterRule) {
	for i := range rules {
		r := &rules[i]
		if matchErrorFilterRule(r, statusCode, errorCode, message) {
			return true, r
		}
	}
	return false, nil
}

func matchErrorFilterRule(rule *dto.ErrorFilterRule, statusCode int, errorCode string, message string) bool {
	if rule == nil || !hasErrorFilterCondition(rule) {
		return false
	}

	if len(rule.StatusCodes) > 0 && !containsStatusCode(rule.StatusCodes, statusCode) {
		return false
	}

	if len(rule.MessageContains) > 0 && !containsMessage(rule.MessageContains, message) {
		return false
	}

	if len(rule.ErrorCodes) > 0 && !containsErrorCode(rule.ErrorCodes, errorCode) {
		return false
	}

	return true
}

func hasErrorFilterCondition(rule *dto.ErrorFilterRule) bool {
	if rule == nil {
		return false
	}
	if len(rule.StatusCodes) > 0 {
		return true
	}
	for _, keyword := range rule.MessageContains {
		if strings.TrimSpace(keyword) != "" {
			return true
		}
	}
	for _, code := range rule.ErrorCodes {
		if strings.TrimSpace(code) != "" {
			return true
		}
	}
	return false
}

func containsStatusCode(statusCodes []int, target int) bool {
	for _, code := range statusCodes {
		if code == target {
			return true
		}
	}
	return false
}

func containsMessage(keywords []string, message string) bool {
	messageLower := strings.ToLower(message)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(messageLower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func containsErrorCode(errorCodes []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, code := range errorCodes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if strings.EqualFold(code, target) {
			return true
		}
	}
	return false
}
