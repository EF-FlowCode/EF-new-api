package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// useGroupModelRateLimit enables the model request limit for one test with a
// one-minute window, then restores the previous settings and store.
func useGroupModelRateLimit(t *testing.T, backend string, total, success int, groups string) {
	t.Helper()
	previous := setting.GetModelRequestRateLimitSettings()
	previousGroups := setting.ModelRequestRateLimitGroup2JSONString()
	previousRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateModelRequestRateLimitSettings(map[string]string{
			"ModelRequestRateLimitEnabled":         strconv.FormatBool(previous.Enabled),
			"ModelRequestRateLimitDurationMinutes": strconv.Itoa(previous.DurationMinutes),
			"ModelRequestRateLimitCount":           strconv.Itoa(previous.TotalCount),
			"ModelRequestRateLimitSuccessCount":    strconv.Itoa(previous.SuccessCount),
			"ModelRequestRateLimitGroup":           previousGroups,
		}))
	})
	useModelLimitStore(t, backend)
	common.RedisEnabled = backend == "redis"
	require.NoError(t, setting.UpdateModelRequestRateLimitSettings(map[string]string{
		"ModelRequestRateLimitEnabled":         "true",
		"ModelRequestRateLimitDurationMinutes": "1",
		"ModelRequestRateLimitCount":           strconv.Itoa(total),
		"ModelRequestRateLimitSuccessCount":    strconv.Itoa(success),
		"ModelRequestRateLimitGroup":           groups,
	}))
}

// groupLimitRouter takes the user from ?user, the token group from ?group and
// the user's own group from ?user_group, then runs the real middleware.
func groupLimitRouter(handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Query("user"))
		c.Set("id", id)
		common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Query("group"))
		common.SetContextKey(c, constant.ContextKeyUserGroup, c.Query("user_group"))
	}, ModelRequestRateLimit(), handler)
	return router
}

func groupLimitRequest(router *gin.Engine, user int, group, query string) int {
	w := httptest.NewRecorder()
	target := fmt.Sprintf("/?user=%d&group=%s&%s", user, group, query)
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w.Code
}

type groupLimitStep struct {
	user   int
	group  string
	query  string
	status int
}

func runGroupLimitSteps(t *testing.T, router *gin.Engine, steps []groupLimitStep) {
	t.Helper()
	for i, step := range steps {
		require.Equal(t, step.status, groupLimitRequest(router, step.user, step.group, step.query),
			"step %d: user %d group %q %s", i, step.user, step.group, step.query)
	}
}

func TestModelRateLimitGroupCountsSuccessSeparately(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useGroupModelRateLimit(t, backend, 0, 1, `{"small":[0,1],"large":[0,2]}`)
			router := groupLimitRouter(func(c *gin.Context) { c.Status(http.StatusOK) })
			runGroupLimitSteps(t, router, []groupLimitStep{
				{42, "small", "", 200},
				{42, "small", "", 429},
				// The smaller group's successes do not eat into the larger group.
				{42, "large", "", 200},
				{42, "large", "", 200},
				{42, "large", "", 429},
				// Without a token group the user's own group selects the same counter.
				{42, "", "user_group=small", 429},
				// Group requests did not consume the global allowance, and the
				// groups without their own limit share one per-user counter.
				{42, "default", "", 200},
				{42, "default", "", 429},
				{42, "other", "", 429},
				{42, "", "user_group=default", 429},
				// Counters are still per user.
				{43, "small", "", 200},
				{43, "default", "", 200},
			})
		})
	}
}

func TestModelRateLimitGroupCountsTotalSeparately(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useGroupModelRateLimit(t, backend, 1, 100, `{"small":[1,100],"large":[2,100]}`)
			// Failures still consume the total allowance.
			router := groupLimitRouter(func(c *gin.Context) { c.Status(http.StatusBadGateway) })
			runGroupLimitSteps(t, router, []groupLimitStep{
				{42, "small", "", 502},
				{42, "small", "", 429},
				{42, "large", "", 502},
				{42, "large", "", 502},
				{42, "large", "", 429},
				{42, "default", "", 502},
				{42, "default", "", 429},
				{42, "other", "", 429},
				{43, "small", "", 502},
				{43, "default", "", 502},
			})
		})
	}
}

func TestModelRateLimitGroupReservationReleases(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useGroupModelRateLimit(t, backend, 0, 1, `{"small":[0,1]}`)
			entered, release := make(chan struct{}), make(chan struct{})
			router := groupLimitRouter(func(c *gin.Context) {
				if c.Query("mode") == "hold" {
					entered <- struct{}{}
					<-release
					c.Status(http.StatusBadGateway)
					return
				}
				c.Status(http.StatusOK)
			})
			held := make(chan int, 1)
			go func() { held <- groupLimitRequest(router, 42, "small", "mode=hold") }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("held request never reached the handler")
			}
			runGroupLimitSteps(t, router, []groupLimitStep{
				// The in-flight reservation occupies the group's only slot ...
				{42, "small", "", 429},
				// ... but not the user's global one.
				{42, "default", "", 200},
				{42, "default", "", 429},
			})
			close(release)
			require.Equal(t, http.StatusBadGateway, <-held)
			// The failed request released its slot; a success then consumes it.
			runGroupLimitSteps(t, router, []groupLimitStep{
				{42, "small", "", 200},
				{42, "small", "", 429},
				{42, "default", "", 429},
			})
		})
	}
}
