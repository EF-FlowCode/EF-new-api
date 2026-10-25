// Package taskplugin exposes only the Root control plane, never provider I/O.
package taskplugin

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/plugins"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes keeps authentication attached to every endpoint, including
// source, icon, dryrun and read-only status. Callers cannot omit RootAuth.
func RegisterRoutes(api *gin.RouterGroup, manager *management.Manager) {
	g := api.Group("/plugin/task", middleware.RootAuth(), middleware.DisableCache())
	h := handler{manager: manager}
	registerReviewRoutes(g, manager)
	g.POST("/runtime/tasks/:task_id/adopt", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid task database id"))
			return
		}
		task, err := manager.AdoptLegacyTask(c.Request.Context(), id)
		if err != nil {
			respond(c, nil, err)
			return
		}
		respond(c, gin.H{"task_id": task.TaskID, "plugin": task.PrivateData.Plugin}, nil)
	})
	g.GET("/runtime/shadow/bindings", func(c *gin.Context) {
		value, err := manager.ShadowBindings(c.Request.Context())
		respond(c, value, err)
	})
	g.PUT("/runtime/shadow/bindings/:channel_id", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("channel_id"))
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid channel_id"))
			return
		}
		var req management.ShadowBindingRequest
		if !bind(c, &req, 4096) {
			return
		}
		respond(c, nil, manager.BindShadow(c.Request.Context(), id, req))
	})
	g.DELETE("/runtime/shadow/bindings/:channel_id", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("channel_id"))
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid channel_id"))
			return
		}
		respond(c, nil, manager.UnbindShadow(c.Request.Context(), id))
	})
	g.GET("/runtime/shadow/reports", func(c *gin.Context) { respond(c, manager.ShadowReports(), nil) })
	g.GET("/runtime/production/bindings", func(c *gin.Context) {
		value, err := manager.ProductionBindings(c.Request.Context())
		respond(c, value, err)
	})
	g.PUT("/runtime/production/bindings/:channel_id", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("channel_id"))
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid channel_id"))
			return
		}
		var req management.ProductionBindingRequest
		if !bind(c, &req, 4096) {
			return
		}
		respond(c, nil, manager.BindProduction(c.Request.Context(), id, req))
	})
	g.DELETE("/runtime/production/bindings/:channel_id", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("channel_id"))
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid channel_id"))
			return
		}
		respond(c, nil, manager.UnbindProduction(c.Request.Context(), id))
	})
	g.GET("", h.list)
	g.POST("", middleware.CriticalRateLimit(), h.upload)
	g.PUT("", middleware.CriticalRateLimit(), h.upload)
	g.GET("/runtime/status", func(c *gin.Context) { respond(c, manager.Status(), nil) })
	g.GET("/runtime/settings", func(c *gin.Context) { value, err := manager.Settings(c.Request.Context()); respond(c, value, err) })
	g.PUT("/runtime/settings", middleware.CriticalRateLimit(), h.settings)
	g.GET("/:key", h.detail)
	g.GET("/:key/icon", h.icon)
	g.GET("/:key/versions", func(c *gin.Context) {
		value, err := manager.Versions(c.Request.Context(), c.Param("key"))
		respond(c, value, err)
	})
	g.POST("/:key/activate", middleware.CriticalRateLimit(), h.activate)
	g.POST("/:key/status", middleware.CriticalRateLimit(), h.status)
	g.POST("/:key/dryrun", middleware.CriticalRateLimit(), h.dryrun)
	g.DELETE("/:key/versions/:version", middleware.CriticalRateLimit(), func(c *gin.Context) {
		respond(c, nil, manager.Delete(c.Request.Context(), c.Param("key"), c.Param("version")))
	})
}

type handler struct{ manager *management.Manager }

func respond(c *gin.Context, value any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": value})
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, management.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, management.ErrConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, management.ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": management.ErrUnavailable.Error()})
		return
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

// Read the entire bounded body so trailing JSON and oversized chunked bodies
// cannot be accepted by a decoder that stops at the first JSON object.
func bind(c *gin.Context, value any, limit int64) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "message": "request body too large"})
			return false
		}
		respond(c, nil, err)
		return false
	}
	if err := common.Unmarshal(body, value); err != nil {
		respond(c, nil, err)
		return false
	}
	return true
}

func (h handler) list(c *gin.Context) {
	value, err := h.manager.List(c.Request.Context())
	respond(c, value, err)
}
func (h handler) upload(c *gin.Context) {
	var req management.UploadRequest
	// 1 MiB source may expand sixfold through JSON unicode escaping.
	if !bind(c, &req, 7<<20) {
		return
	}
	value, err := h.manager.Upload(c.Request.Context(), req)
	respond(c, value, err)
}
func (h handler) settings(c *gin.Context) {
	var req management.Settings
	if !bind(c, &req, 4096) {
		return
	}
	respond(c, nil, h.manager.Configure(c.Request.Context(), req))
}
func (h handler) detail(c *gin.Context) {
	row, err := h.manager.Detail(c.Request.Context(), c.Param("key"), c.Query("version"))
	if err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, gin.H{"plugin": row, "source": row.Source, "icon": row.Icon, "factory": row.Id == 0}, nil)
}
func (h handler) icon(c *gin.Context) {
	row, err := h.manager.Detail(c.Request.Context(), c.Param("key"), c.Query("version"))
	if err != nil {
		respond(c, nil, err)
		return
	}
	icon := row.Icon
	if icon == "" {
		icon = plugins.IconDataURI(row.Key)
	}
	if icon == "" {
		respond(c, nil, management.ErrNotFound)
		return
	}
	mediaType, data, err := jsplugin.DecodeIconDataURI(icon)
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, mediaType, data)
}
func (h handler) activate(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	if !bind(c, &req, 4096) {
		return
	}
	if req.Version == "" {
		respond(c, nil, errors.New("version is required"))
		return
	}
	respond(c, nil, h.manager.Activate(c.Request.Context(), c.Param("key"), req.Version))
}
func (h handler) status(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !bind(c, &req, 4096) {
		return
	}
	if req.Enabled == nil {
		respond(c, nil, errors.New("enabled is required"))
		return
	}
	respond(c, nil, h.manager.SetStatus(c.Request.Context(), c.Param("key"), *req.Enabled))
}
func (h handler) dryrun(c *gin.Context) {
	var req struct {
		Hook   string `json:"hook"`
		Member string `json:"member"`
		Args   []any  `json:"args"`
	}
	if !bind(c, &req, 1<<20) {
		return
	}
	value, err := h.manager.DryRun(c.Request.Context(), c.Param("key"), c.Query("version"), req.Hook, req.Member, req.Args)
	respond(c, value, err)
}
