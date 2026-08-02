package controller

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Public model usage leaderboard. Aggregates the per-model rollups already
// maintained in quota_data (refreshed hourly), so it never touches the hot
// logs table and never exposes per-user data. Cached in-memory per range.

const (
	modelRankingTopN        = 50 // ranking list length
	modelRankingChartTopN   = 10 // models stacked in the usage chart
	modelRankingChartMaxDay = 30 // chart window cap (days) for long ranges
	modelRankingCacheTTL    = 60 * time.Second
	// eflow: 「今天」按东八区(UTC+8)零点切，而非 UTC 零点——面向国内用户，
	// 否则北京时间 08:00 前「今天」会落到 UTC 的昨天。其余区间是滚动窗口(now-Nd)，
	// 与时区无关，无需偏移。
	leaderboardTZOffsetSeconds = 8 * 3600
)

type modelRankingEntry struct {
	Rank       int     `json:"rank"`
	ModelName  string  `json:"model_name"`
	TokenUsed  int64   `json:"token_used"`
	Count      int64   `json:"count"`
	Quota      int64   `json:"quota"`
	TrendPct   float64 `json:"trend_pct"`
	TrendKnown bool    `json:"trend_known"`
}

type modelRankingSeriesPoint struct {
	Time      int64  `json:"time"`
	ModelName string `json:"model_name"`
	TokenUsed int64  `json:"token_used"`
}

// eflow: 名次走势点——只携带「时间桶 + 模型 + 该桶内名次」,绝不带 token。
// 名次本身就是公开排名榜暴露的相对量,因此对外发它不破坏脱敏。
type modelRankingRankPoint struct {
	Time      int64  `json:"time"`
	ModelName string `json:"model_name"`
	Rank      int    `json:"rank"`
}

type modelRankingPayload struct {
	Range       string                    `json:"range"`
	Bucket      string                    `json:"bucket"` // "hour" | "day"
	TotalTokens int64                     `json:"total_tokens"`
	GeneratedAt int64                     `json:"generated_at"`
	// eflow: 区间元数据——前端「区间上下文条」据此显示真实起止日期与涨跌对比基准区间。
	// 都是时间戳(非用量)，不破脱敏。WindowStart=统计区间起点(0=全部时间)；
	// PrevStart=涨跌对比的上一等长区间起点(0=无对比，即全部时间)。窗口终点=GeneratedAt。
	WindowStart int64                     `json:"window_start"`
	PrevStart   int64                     `json:"prev_start"`
	Ranking     []modelRankingEntry       `json:"ranking"`
	Series      []modelRankingSeriesPoint `json:"series"`
	RankSeries  []modelRankingRankPoint   `json:"rank_series"`
}

type modelRankingCached struct {
	payload modelRankingPayload
	at      time.Time
}

var (
	modelRankingCacheMu sync.Mutex
	modelRankingCache   = map[string]modelRankingCached{}
)

var modelRankingValidRanges = map[string]struct{}{
	"today": {}, "week": {}, "month": {}, "year": {}, "all": {},
}

// rangeWindow resolves a range key into the ranking window [start, now] and the
// chart window [chartStart, now] with its bucket size (seconds).
func rangeWindow(rangeKey string, now int64) (start int64, chartStart int64, bucketSeconds int) {
	day := int64(86400)
	switch rangeKey {
	case "today":
		// 东八区零点的 unix 秒：把 now 平移到 UTC+8 时区再对 day 取模。
		// quota_data 桶本身存 UTC 秒，比时间戳不受影响。
		start = now - ((now + leaderboardTZOffsetSeconds) % day)
		return start, start, 3600
	case "week":
		start = now - 7*day
		return start, start, int(day)
	case "month":
		start = now - 30*day
		return start, start, int(day)
	case "year":
		start = now - 365*day
		return start, now - modelRankingChartMaxDay*day, int(day)
	default: // all
		return 0, now - modelRankingChartMaxDay*day, int(day)
	}
}

func GetModelRanking(c *gin.Context) {
	rangeKey := strings.TrimSpace(c.Query("range"))
	if rangeKey == "" {
		rangeKey = "week"
	}
	if _, ok := modelRankingValidRanges[rangeKey]; !ok {
		rangeKey = "week"
	}

	modelRankingCacheMu.Lock()
	if cached, ok := modelRankingCache[rangeKey]; ok && time.Since(cached.at) < modelRankingCacheTTL {
		payload := cached.payload
		modelRankingCacheMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
		return
	}
	modelRankingCacheMu.Unlock()

	payload, err := buildModelRankingPayload(rangeKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	modelRankingCacheMu.Lock()
	modelRankingCache[rangeKey] = modelRankingCached{payload: payload, at: time.Now()}
	modelRankingCacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
}

func buildModelRankingPayload(rangeKey string) (modelRankingPayload, error) {
	now := time.Now().Unix()
	start, chartStart, bucketSeconds := rangeWindow(rangeKey, now)

	// Current-window per-model aggregates (ordered by token_used DESC).
	curRows, err := model.GetModelRankingData(start, 0)
	if err != nil {
		return modelRankingPayload{}, err
	}

	// Previous equal-length window, for trend %. Skipped for all-time (start=0).
	prevTokens := map[string]int64{}
	var prevStart int64
	if start > 0 {
		prevStart = start - (now - start)
		if prevStart < 0 {
			prevStart = 0
		}
		prevRows, perr := model.GetModelRankingData(prevStart, start)
		if perr == nil {
			for _, r := range prevRows {
				prevTokens[r.ModelName] = int64(r.TokenUsed)
			}
		}
	}

	ranking := make([]modelRankingEntry, 0, len(curRows))
	var totalTokens int64
	for i, r := range curRows {
		entry := modelRankingEntry{
			Rank:      i + 1,
			ModelName: r.ModelName,
			TokenUsed: int64(r.TokenUsed),
			Count:     int64(r.Count),
			Quota:     int64(r.Quota),
		}
		totalTokens += entry.TokenUsed
		if prev, ok := prevTokens[r.ModelName]; ok && prev > 0 {
			entry.TrendKnown = true
			entry.TrendPct = float64(entry.TokenUsed-prev) / float64(prev) * 100
		}
		if i < modelRankingTopN {
			ranking = append(ranking, entry)
		}
	}

	// eflow: 这是面向用户的「脱敏」公开排行榜——只暴露相对信息(排名 / 模型名 /
	// 涨跌%),绝对用量(总量、各模型 token、热门模型用量曲线)一律不返回,否则
	// 用户能反推平台流水。真实数字由站长在 admin-api 后台单独查看,不走这个公开接口。
	for i := range ranking {
		ranking[i].TokenUsed = 0
		ranking[i].Count = 0
		ranking[i].Quota = 0
	}
	_ = totalTokens

	// eflow: 名次走势——逐时间桶对所有模型按用量排名,只把榜上 top-N 模型的「名次」
	// 序列发出去(整数 1..N),用量绝对值一律不出。脱敏在这里完成。
	listed := make(map[string]bool, len(ranking))
	for _, e := range ranking {
		listed[e.ModelName] = true
	}
	rankSeries := buildModelRankSeries(chartStart, now, int64(bucketSeconds), listed)

	bucketLabel := "day"
	if bucketSeconds == 3600 {
		bucketLabel = "hour"
	}

	return modelRankingPayload{
		Range:       rangeKey,
		Bucket:      bucketLabel,
		TotalTokens: 0,
		GeneratedAt: now,
		WindowStart: start,
		PrevStart:   prevStart,
		Ranking:     ranking,
		Series:      []modelRankingSeriesPoint{},
		RankSeries:  rankSeries,
	}, nil
}

// buildModelRankSeries computes each model's rank position per time bucket from
// the quota_data rollups (never the hot logs table). Models are ranked within
// each bucket by token usage, but only the rank integer is emitted for models
// that appear in the public top-N list — absolute volume never leaves here.
func buildModelRankSeries(chartStart, now, bucketSeconds int64, listed map[string]bool) []modelRankingRankPoint {
	buckets, err := model.GetRankingQuotaBuckets(chartStart, now, bucketSeconds)
	if err != nil || len(buckets) == 0 {
		return []modelRankingRankPoint{}
	}

	byBucket := map[int64][]model.RankingQuotaBucket{}
	order := make([]int64, 0)
	for _, b := range buckets {
		if _, ok := byBucket[b.Bucket]; !ok {
			order = append(order, b.Bucket)
		}
		byBucket[b.Bucket] = append(byBucket[b.Bucket], b)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	points := make([]modelRankingRankPoint, 0)
	for _, ts := range order {
		rows := byBucket[ts]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Tokens > rows[j].Tokens })
		for i, r := range rows {
			if listed[r.ModelName] {
				points = append(points, modelRankingRankPoint{
					Time:      ts,
					ModelName: r.ModelName,
					Rank:      i + 1,
				})
			}
		}
	}
	return points
}
