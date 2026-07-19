package storage

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/logging"
)

// Open 通过 GORM 连接 PostgreSQL。autoMigrate 为 true 时运行 GORM AutoMigrate；
// 否则以 scripts/sql/init.sql 中的脚本作为权威 schema。
func Open(dsn string, autoMigrate bool) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// GORM 日志桥接到 zap：SQL 错误/慢查询与业务日志同源可检索。
		Logger: logging.GormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 postgres 失败: %w", err)
	}

	if autoMigrate {
		if err := db.AutoMigrate(AllModels()...); err != nil {
			return nil, fmt.Errorf("自动迁移失败: %w", err)
		}
	}
	return db, nil
}
