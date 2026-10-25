package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockForUpdate 为事务查询添加 SELECT ... FOR UPDATE。
// 本项目只支持 MySQL 8，所有事务行锁统一走此入口。
func lockForUpdate(tx *gorm.DB) *gorm.DB {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}
