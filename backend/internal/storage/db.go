package storage

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/logging"
)

// Open 通过 GORM 连接 PostgreSQL。表结构只由 scripts/sql/init.sql 维护。
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// GORM 日志桥接到 zap：SQL 错误/慢查询与业务日志同源可检索。
		Logger: logging.GormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 postgres 失败: %w", err)
	}

	return db, nil
}
