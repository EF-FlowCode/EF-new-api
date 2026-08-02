package service

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// ErrNoEligibleAssignee 表示分配引擎找不到合适的候选人（规则为空、策略为 manual、候选被禁用等）。
// 调用方应把工单留在待认领池并通知管理员。
var ErrNoEligibleAssignee = errors.New("no eligible ticket assignee")

// 轮询计数器：按规则类型独立自增，保证"每来一条就换下一位"。
// 在内存里维持，重启后归零，无需精确全局一致——工单数量不大时偏差可忽略。
var (
	roundRobinMu sync.Mutex
	roundRobin   = map[string]*uint64{}
)

// AutoAssignTicketOnCreate 在工单创建后异步执行分配。
// 它不会让创建流程失败：任何错误都会被记录日志，调用方只要乐观继续即可。
//
// 工作流程：
//  1. 未启用自动分配时直接返回
//  2. 按工单类型拿规则 -> 拉候选 -> 过滤已禁用/角色不足 -> 应用策略 -> 乐观锁分配
//  3. 分配成功后调用 notifyCb 发送"已分配"通知（由调用方传入）
//
// notifyCb 为空时只做分配，不发通知。
func AutoAssignTicketOnCreate(ticketID int, ticketType string, notifyCb func(ticket *model.Ticket, assigneeID int)) {
	if ticketID <= 0 {
		return
	}
	if !setting.IsTicketAssignEnabled() {
		return
	}
	rule, ok := setting.GetTicketAssignRule(ticketType)
	if !ok || rule.Strategy == setting.TicketAssignStrategyManual {
		// 手动策略：由管理员事后分配；这里直接返回，保留在待认领池。
		return
	}
	assigneeID, err := pickAssignee(ticketType, rule)
	if err != nil {
		// 当前类型挑不出人 -> 尝试兜底：回落到 general 组
		if fallback, hasFallback := setting.GetTicketAssignFallbackRule(); hasFallback &&
			fallback.Strategy != setting.TicketAssignStrategyManual {
			assigneeID, err = pickAssignee("general", fallback)
		}
	}
	if err != nil {
		common.SysLog(fmt.Sprintf("ticket auto-assign: no eligible staff for ticket %d (type=%s): %s",
			ticketID, ticketType, err.Error()))
		return
	}

	// 乐观锁要求工单当前为未分配（0）。如果已经被客服首条回复抢先认领，这里会返回
	// ErrTicketAssigneeInvalid——属于正常竞态，静默即可。
	expected := 0
	ticket, _, err := model.AssignTicket(ticketID, assigneeID, &expected)
	if err != nil {
		if !errors.Is(err, model.ErrTicketAssigneeInvalid) {
			common.SysLog(fmt.Sprintf("ticket auto-assign: failed to assign ticket %d to user %d: %s",
				ticketID, assigneeID, err.Error()))
		}
		return
	}
	if notifyCb != nil {
		notifyCb(ticket, assigneeID)
	}
}

// pickAssignee 根据策略从 rule.Users 中挑一个。会过滤掉：
//   - 账号被禁用的用户
//   - 角色低于客服的用户（防止管理员在规则里误填普通用户）
func pickAssignee(ticketType string, rule setting.TicketAssignRule) (int, error) {
	candidates := filterEligibleUsers(rule.Users)
	if len(candidates) == 0 {
		return 0, ErrNoEligibleAssignee
	}
	switch rule.Strategy {
	case setting.TicketAssignStrategyRoundRobin:
		return pickRoundRobin(ticketType, candidates), nil
	case setting.TicketAssignStrategyLeastLoaded:
		return pickLeastLoaded(candidates)
	case setting.TicketAssignStrategyRandom:
		return candidates[rand.Intn(len(candidates))], nil
	default:
		// 未知策略一律走轮询，而不是把工单留在待认领池。
		return pickRoundRobin(ticketType, candidates), nil
	}
}

// filterEligibleUsers 按用户状态 + 角色过滤候选列表。遇到 DB 错误时跳过该用户而非整体失败。
// 这里走 GetUserById 而不是 GetUserCache，是因为缓存版本的 UserBase 不包含 Role 字段——
// 候选数量通常很小（十来个），多几次 DB 查询可以接受，但若以后规模扩大可以考虑
// 在 UserBase 中增加 Role 字段以复用缓存。
func filterEligibleUsers(users []int) []int {
	out := make([]int, 0, len(users))
	for _, uid := range users {
		if uid <= 0 {
			continue
		}
		user, err := model.GetUserById(uid, false)
		if err != nil || user == nil {
			if err != nil {
				common.SysLog(fmt.Sprintf("ticket auto-assign: skip user %d: %s", uid, err.Error()))
			}
			continue
		}
		if user.Status != common.UserStatusEnabled {
			continue
		}
		if user.Role < common.RoleCustomerServiceUser {
			continue
		}
		out = append(out, uid)
	}
	return out
}

// pickRoundRobin 在一个按类型独立的计数器上轮转选择。
// 读取/自增用原子操作，避免跨请求锁竞争。
func pickRoundRobin(ticketType string, candidates []int) int {
	if len(candidates) == 1 {
		return candidates[0]
	}
	roundRobinMu.Lock()
	counter, ok := roundRobin[ticketType]
	if !ok {
		var c uint64
		counter = &c
		roundRobin[ticketType] = counter
	}
	roundRobinMu.Unlock()
	idx := int(atomic.AddUint64(counter, 1)-1) % len(candidates)
	return candidates[idx]
}

// pickLeastLoaded 选择当前活跃工单数最少的候选；并列时选 ID 较小者（确定性）。
func pickLeastLoaded(candidates []int) (int, error) {
	load, err := model.CountAssigneeLoad(candidates)
	if err != nil {
		return 0, err
	}
	best := candidates[0]
	bestLoad := load[best]
	for _, uid := range candidates[1:] {
		l := load[uid]
		if l < bestLoad || (l == bestLoad && uid < best) {
			best = uid
			bestLoad = l
		}
	}
	return best, nil
}

// ResolveAssigneeIdsForUser 对客服视角计算"我能看到的工单池"。
// - 管理员（role>=Admin）：返回 nil，表示不过滤（可以看到全部）
// - 客服：返回自己 + 所在组的同事 ID 列表
// 返回的切片不包含 0。
func ResolveAssigneeIdsForUser(userId int, role int) []int {
	if role >= common.RoleAdminUser {
		return nil
	}
	return setting.AssigneeIdsForUser(userId)
}
