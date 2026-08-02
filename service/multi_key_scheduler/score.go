package multi_key_scheduler

import (
	"context"
	"math/rand"

	"github.com/QuantumNous/new-api/dto"
)

// headroomBand：把 headroom 距最优 ≤ 此值的候选视为「同等健康」，在这条带内改用
// in-flight 最少来选，从而把流量摊开。0.10 = 余量相差 10 个百分点以内算平手；真正
// 接近限额（headroom 明显更低）的 key 仍被排除在带外，保留原 headroom 的撞限规避语义。
const headroomBand = 0.10

// pickBest 在 candidates 中挑最佳：先按 headroom 框出「同等健康」的候选，再在其中按
// in-flight 最少挑，彻底并列时随机。
//
// headroom：每个 candidate 用 backend.Peek 拿当前 (rpm, tpm, in_flight, cooling)，算
// 「最紧的那一维剩余比例」作为健康度——多维取 min，避免 RPM 余量 90% 但并发撞死时被错挑。
// 三上限都为 0 时健康度记 1.0。Cool 候选直接淘汰；全员 cool 返回 nil。
//
// eflow: 原实现是 strict-max(headroom) + 列表序平手，会把流量长期堆在列表第一把 key 上：
// 该 key 即便正卡在多个 hung 请求(in-flight 高)，其 RPM 计数 60s 后衰减回 0、headroom
// 复位 1.0，又被重新选中 → 单账号被反复打、卡死率畸高（实测 channel 69 / glm-5.2 的慢
// 请求 67% 集中在 index 0，而它只占 ~40% 流量）。改成「healthy band 内 least-in-flight，
// 平手随机」后：正在服务 hung 请求的 key 被自动绕开，低流量(1~N req/min)也能真正铺满
// 所有 key，单账号被连打的概率从 ~40% 摊到 ~1/N。
func pickBest(ctx context.Context, be backend, channelID int, model string, candidates []KeyCandidate, cfg *dto.SchedulerConfig) *KeyCandidate {
	type scored struct {
		cand     KeyCandidate
		score    float64
		inFlight int
	}

	scoredAll := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		quota := QuotaFor(cfg, c.Fp, model)
		dec := be.Peek(ctx, channelID, c.Fp, model, quota)
		if dec.Cooling {
			continue
		}
		// Peek 已返回 Allowed=false 时也不直接淘汰——Acquire 会原子复检，Peek 快照
		// 可能已过期。让 RPM 略撞但即将过窗的 key 也能竞标。
		scoredAll = append(scoredAll, scored{
			cand:     c,
			score:    computeHeadroom(quota, dec),
			inFlight: dec.InFlight,
		})
	}

	if len(scoredAll) == 0 {
		return nil
	}

	// 1) 取最高 headroom。候选数 ≤ 单 channel 下的 sk 数（一般 ≤ 100），线性扫即可。
	maxScore := scoredAll[0].score
	for i := 1; i < len(scoredAll); i++ {
		if scoredAll[i].score > maxScore {
			maxScore = scoredAll[i].score
		}
	}

	// 2) 在 [maxScore-headroomBand, maxScore] 这条「同等健康」带内按 in-flight 最少挑；
	//    in-flight 也并列时用蓄水池抽样在并列集合内均匀随机，避免长期偏向列表序。
	//    至少有一个候选满足 score==maxScore，故 bestIdx 一定会被赋值。
	bestIdx := -1
	minInFlight := 0
	ties := 0
	for i := range scoredAll {
		if scoredAll[i].score < maxScore-headroomBand {
			continue
		}
		switch {
		case bestIdx < 0 || scoredAll[i].inFlight < minInFlight:
			bestIdx = i
			minInFlight = scoredAll[i].inFlight
			ties = 1
		case scoredAll[i].inFlight == minInFlight:
			ties++
			if rand.Intn(ties) == 0 {
				bestIdx = i
			}
		}
	}
	return &scoredAll[bestIdx].cand
}

// computeHeadroom 算一个 key 在某 model 上的健康度。
// 取值范围 [0, 1]，越大代表余量越多。三上限都没配时返回 1（视为完全健康）。
func computeHeadroom(quota dto.ModelQuota, dec Decision) float64 {
	score := 1.0
	any := false

	if quota.RPMMax > 0 {
		any = true
		left := float64(quota.RPMMax-dec.RPMCount) / float64(quota.RPMMax)
		score = minFloat(score, clamp01(left))
	}
	if quota.TPMMax > 0 {
		any = true
		left := float64(quota.TPMMax-dec.TPMCount) / float64(quota.TPMMax)
		score = minFloat(score, clamp01(left))
	}
	if quota.ConcurrencyMax > 0 {
		any = true
		left := float64(quota.ConcurrencyMax-dec.InFlight) / float64(quota.ConcurrencyMax)
		score = minFloat(score, clamp01(left))
	}
	if !any {
		return 1.0
	}
	return score
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}