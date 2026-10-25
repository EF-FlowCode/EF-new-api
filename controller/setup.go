package controller

import (
	"errors"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
)

type Setup struct {
	Status   bool `json:"status"`
	RootInit bool `json:"root_init"`
}

type SetupRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	SelfUseModeEnabled bool   `json:"SelfUseModeEnabled"`
	DemoSiteEnabled    bool   `json:"DemoSiteEnabled"`
}

func GetSetup(c *gin.Context) {
	setupComplete := constant.IsSetupComplete()
	setup := Setup{
		Status: setupComplete,
	}
	if setupComplete {
		c.JSON(200, gin.H{
			"success": true,
			"data":    setup,
		})
		return
	}
	setup.RootInit = model.RootUserExists()
	c.JSON(200, gin.H{
		"success": true,
		"data":    setup,
	})
}

func PostSetup(c *gin.Context) {
	// Check if root user already exists
	rootExists := model.RootUserExists()
	var rootUser *model.User

	var req SetupRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "请求参数有误",
		})
		return
	}

	// If root doesn't exist, validate and create admin account
	if !rootExists {
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" {
			c.JSON(200, gin.H{
				"success": false,
				"message": "用户名不能为空",
			})
			return
		}
		if req.Password != req.ConfirmPassword {
			c.JSON(200, gin.H{
				"success": false,
				"message": "两次输入的密码不一致",
			})
			return
		}
		rootCandidate := model.User{
			Username: req.Username,
			Password: req.Password,
		}
		if err := common.Validate.Struct(&rootCandidate); err != nil {
			c.JSON(200, gin.H{
				"success": false,
				"message": "管理员账号参数不符合要求: " + err.Error(),
			})
			return
		}

		// Prepare the root user; persistence happens with all setup state below.
		hashedPassword, err := common.Password2Hash(req.Password)
		if err != nil {
			c.JSON(200, gin.H{
				"success": false,
				"message": "系统错误: " + err.Error(),
			})
			return
		}
		rootUser = &model.User{
			Username:    req.Username,
			Password:    hashedPassword,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: nil,
			Quota:       100000000,
		}
	}

	setup := model.Setup{
		ID:            1,
		Version:       common.Version,
		InitializedAt: time.Now().Unix(),
	}
	err = model.InitializeSystem(rootUser, req.SelfUseModeEnabled, req.DemoSiteEnabled, setup)
	if err != nil {
		message := "系统初始化失败: " + err.Error()
		if errors.Is(err, model.ErrSetupAlreadyCompleted) {
			message = "系统已经初始化完成"
		}
		c.JSON(200, gin.H{
			"success": false,
			"message": message,
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": "系统初始化成功",
	})
}
