package model

import (
	"errors"
	"strconv"
	"sync"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"gorm.io/gorm"
)

var (
	ErrSetupAlreadyCompleted = errors.New("system setup is already completed")
	ErrSetupRootRequired     = errors.New("root user is required")
	setupMu                  sync.Mutex
)

type Setup struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Version       string `json:"version" gorm:"type:varchar(50);not null"`
	InitializedAt int64  `json:"initialized_at" gorm:"type:bigint;not null"`
}

func GetSetup() *Setup {
	var setup Setup
	err := DB.First(&setup).Error
	if err != nil {
		return nil
	}
	return &setup
}

func InitializeSystem(rootUser *User, selfUseModeEnabled bool, demoSiteEnabled bool, setup Setup) error {
	setupMu.Lock()
	defer setupMu.Unlock()

	options := map[string]string{
		"SelfUseModeEnabled": strconv.FormatBool(selfUseModeEnabled),
		"DemoSiteEnabled":    strconv.FormatBool(demoSiteEnabled),
	}
	if err := validateOptionValues(options); err != nil {
		return err
	}
	if setup.ID == 0 {
		setup.ID = 1
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existingSetup Setup
		err := tx.First(&existingSetup).Error
		if err == nil {
			return ErrSetupAlreadyCompleted
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var existingRoot User
		err = tx.Where("role = ?", common.RoleRootUser).First(&existingRoot).Error
		switch {
		case err == nil:
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case rootUser == nil:
			return ErrSetupRootRequired
		default:
			if err := tx.Create(rootUser).Error; err != nil {
				return err
			}
		}

		if err := saveOptionValuesWithTx(tx, options); err != nil {
			return err
		}
		return tx.Create(&setup).Error
	})
	if err != nil {
		return err
	}
	if err := publishOptionValues(options); err != nil {
		return err
	}
	constant.SetSetupComplete(true)
	return nil
}
