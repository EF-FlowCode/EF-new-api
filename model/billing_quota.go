package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

// BillingQuotaAdjustment applies one consumed-quota delta. Positive values
// consume more quota; negative values refund quota.
type BillingQuotaAdjustment struct {
	UserID   int
	TokenID  int
	TokenKey string
	Delta    int
}

// UpdateTaskWithBillingAtomic conditionally advances a task and applies its
// quota delta in the same transaction. Losing the status CAS performs no
// billing, and a billing error rolls the task transition back.
func UpdateTaskWithBillingAtomic(task *Task, fromStatus TaskStatus, adjustment BillingQuotaAdjustment) (bool, error) {
	if task == nil {
		return false, errors.New("task is nil")
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(task).Where("status = ?", fromStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		won = true
		if adjustment.Delta == 0 {
			return nil
		}
		if adjustment.UserID > 0 {
			if err := adjustUserQuotaTx(tx, adjustment.UserID, adjustment.Delta); err != nil {
				return err
			}
		}
		return adjustTokenDeltaTx(tx, adjustment.TokenID, adjustment.Delta)
	})
	if err != nil {
		return false, err
	}
	if won && adjustment.Delta != 0 {
		updateBillingQuotaCaches(adjustment.UserID, adjustment.TokenKey, adjustment.Delta, adjustment.Delta)
	}
	return won, nil
}

// AdjustBillingQuotaAtomic keeps the funding source and token quota in one DB
// transaction. Cache updates happen only after commit.
func AdjustBillingQuotaAtomic(adjustment BillingQuotaAdjustment) error {
	if adjustment.Delta == 0 {
		return nil
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if adjustment.UserID > 0 {
			if err := adjustUserQuotaTx(tx, adjustment.UserID, adjustment.Delta); err != nil {
				return err
			}
		}
		return adjustTokenDeltaTx(tx, adjustment.TokenID, adjustment.Delta)
	})
	if err == nil {
		updateBillingQuotaCaches(adjustment.UserID, adjustment.TokenKey, adjustment.Delta, adjustment.Delta)
	}
	return err
}

type BillingQuotaRefund struct {
	UserID        int
	TokenID       int
	TokenKey      string
	FundingAmount int
	TokenAmount   int
}

// RefundBillingQuotaAtomic refunds a pre-consume in one transaction. A
// BillingSession provides in-process idempotency for wallet refunds.
func RefundBillingQuotaAtomic(refund BillingQuotaRefund) error {
	if refund.FundingAmount <= 0 && refund.TokenAmount <= 0 {
		return nil
	}
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if refund.UserID > 0 && refund.FundingAmount > 0 {
			if err := tx.Model(&User{}).
				Where("id = ?", refund.UserID).
				Update("quota", gorm.Expr("quota + ?", refund.FundingAmount)).Error; err != nil {
				return err
			}
		}
		if err := adjustTokenDeltaTx(tx, refund.TokenID, -refund.TokenAmount); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied {
		updateBillingQuotaCaches(refund.UserID, refund.TokenKey, -refund.FundingAmount, -refund.TokenAmount)
	}
	return err
}

func adjustUserQuotaTx(tx *gorm.DB, userID int, delta int) error {
	if userID <= 0 || delta == 0 {
		return nil
	}
	query := tx.Model(&User{}).Where("id = ?", userID)
	if delta > 0 {
		query = query.Where("quota >= ?", delta)
	}
	result := query.Update("quota", gorm.Expr("quota - ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota is insufficient or user does not exist")
	}
	return nil
}

func adjustTokenDeltaTx(tx *gorm.DB, tokenID int, delta int) error {
	if tokenID <= 0 || delta == 0 {
		return nil
	}
	return tx.Model(&Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"remain_quota":  gorm.Expr("remain_quota - ?", delta),
		"used_quota":    gorm.Expr("used_quota + ?", delta),
		"accessed_time": common.GetTimestamp(),
	}).Error
}

func updateBillingQuotaCaches(userID int, tokenKey string, fundingDelta, tokenDelta int) {
	updateUserQuotaCacheAfterBilling(userID, fundingDelta)
	if !common.RedisEnabled || tokenKey == "" || tokenDelta == 0 {
		return
	}
	gopool.Go(func() {
		var err error
		if tokenDelta > 0 {
			err = cacheDecrTokenQuota(tokenKey, int64(tokenDelta))
		} else {
			err = cacheIncrTokenQuota(tokenKey, int64(-tokenDelta))
		}
		if err != nil {
			common.SysLog("failed to update token quota cache after billing transaction: " + err.Error())
		}
	})
}

func updateUserQuotaCacheAfterBilling(userID int, delta int) {
	if !common.RedisEnabled || userID <= 0 || delta == 0 {
		return
	}
	gopool.Go(func() {
		var err error
		if delta > 0 {
			err = cacheDecrUserQuota(userID, int64(delta))
		} else {
			err = cacheIncrUserQuota(userID, int64(-delta))
		}
		if err != nil {
			common.SysLog("failed to update user quota cache after billing transaction: " + err.Error())
		}
	})
}
