package controller

import (
	"errors"
	"net/http"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
)

func ExportTicketInvoices(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetInt("role") < common.RoleAdminUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Forbidden"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	var request struct {
		IDs []int `json:"ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": model.ErrInvoiceExportSelection.Error()})
		return
	}
	data, err := service.ExportTicketInvoices(c.Request.Context(), request.IDs)
	if err != nil {
		if errors.Is(err, model.ErrInvoiceExportSelection) || errors.Is(err, model.ErrInvoiceExportIncomplete) || errors.Is(err, service.ErrInvoiceExportCellTooLong) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		} else {
			common.SysError("Invoice export failed: " + err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to export invoices"})
		}
		return
	}
	c.Header("Content-Disposition", `attachment; filename="invoice-requests-`+time.Now().UTC().Format("20060102-150405")+`.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}
