package storage

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/logging"
)

type DBPoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Open 通过 GORM 连接 PostgreSQL。表结构只由 scripts/sql/init.sql 维护。
func Open(dsn string, poolConfigs ...DBPoolConfig) (*gorm.DB, error) {
	pool := DBPoolConfig{}
	if len(poolConfigs) > 0 {
		pool = poolConfigs[0]
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// GORM 日志桥接到 zap：SQL 错误/慢查询与业务日志同源可检索。
		Logger: logging.GormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 postgres 失败: %w", err)
	}
	// GORM otherwise inherits database/sql defaults, which leave the pool
	// effectively unbounded for this workload. Keep model requests, workers
	// and WebSocket-triggered writes within a predictable connection budget.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("读取 postgres 连接池失败: %w", err)
	}
	if pool.MaxOpenConns <= 0 {
		pool.MaxOpenConns = 10
	}
	if pool.MaxIdleConns < 0 {
		pool.MaxIdleConns = 0
	}
	if pool.MaxIdleConns > pool.MaxOpenConns {
		pool.MaxIdleConns = pool.MaxOpenConns
	}
	if pool.ConnMaxLifetime <= 0 {
		pool.ConnMaxLifetime = 30 * time.Minute
	}
	if pool.ConnMaxIdleTime <= 0 {
		pool.ConnMaxIdleTime = 5 * time.Minute
	}
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	return db, nil
}
