package controller

import (
	"eflow.local/new-api/model"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Both routes are registered under the existing Root-only options group.
func GetTaskPricingUsage(c *gin.Context) {
	models, err := model.GetTaskPricingEditorModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": models})
}

func PreviewTaskPricingMigration(c *gin.Context) {
	report, err := model.TaskPricingMigration(c.Request.Context(), "", false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}

func ApplyTaskPricingMigration(c *gin.Context) {
	var request struct {
		SourceHash string `json:"source_hash"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.SourceHash) != 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "valid migration preview required"})
		return
	}
	report, err := model.TaskPricingMigration(c.Request.Context(), request.SourceHash, true)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error(), "data": gin.H{"saved": errors.Is(err, model.ErrOptionSavedNotPublished)}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}
