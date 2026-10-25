package taskplugin

import (
	"errors"
	"strconv"

	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func registerReviewRoutes(g *gin.RouterGroup, m *management.Manager) {
	result := func(c *gin.Context, value any, err error) {
		if errors.Is(err, model.ErrPluginSubmissionConflict) {
			err = management.ErrConflict
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = management.ErrNotFound
		}
		respond(c, value, err)
	}
	g.GET("/runtime/reviews", func(c *gin.Context) {
		after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
		var tasks []model.Task
		err := model.DB.WithContext(c.Request.Context()).Select("id", "task_id", "status", "quota", "revision").Where("id > ? AND status IN ?", after, []model.TaskStatus{model.TaskStatusSubmitting, model.TaskStatusSubmitUnknown, model.TaskStatusBillingPending}).Where("JSON_TYPE(JSON_EXTRACT(private_data, '$.plugin')) = 'OBJECT'").Order("id").Limit(50).Find(&tasks).Error
		rows := make([]service.PluginReviewDetail, 0, len(tasks))
		for _, task := range tasks {
			rows = append(rows, service.PluginReviewDetail{ID: task.ID, TaskID: task.TaskID, Status: task.Status, Revision: task.Revision, Quota: task.Quota})
		}
		result(c, rows, err)
	})
	g.GET("/runtime/tasks/:task_id/review", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid task id"))
			return
		}
		value, err := service.PluginTaskReviewDetail(c.Request.Context(), id)
		result(c, value, err)
	})
	g.POST("/runtime/tasks/:task_id/review/preview", middleware.CriticalRateLimit(), func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
		if err != nil || id <= 0 {
			respond(c, nil, errors.New("invalid task id"))
			return
		}
		var req service.PluginReviewRequest
		if !bind(c, &req, 8192) {
			return
		}
		value, err := service.PreviewPluginTaskReview(c.Request.Context(), m, c.GetInt("id"), id, req)
		result(c, value, err)
	})
	g.POST("/runtime/reviews/:operation/confirm", middleware.CriticalRateLimit(), func(c *gin.Context) {
		op := c.Param("operation")
		if len(op) > 96 {
			respond(c, nil, errors.New("invalid operation id"))
			return
		}
		task, err := service.ConfirmPluginTaskReview(c.Request.Context(), c.GetInt("id"), op)
		if err != nil {
			result(c, nil, err)
			return
		}
		result(c, gin.H{"id": task.ID, "task_id": task.TaskID, "status": task.Status, "quota": task.Quota}, nil)
	})
}
