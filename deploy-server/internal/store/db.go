package store

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store 包住 *gorm.DB，后面所有数据访问都从这里出发。
type Store struct {
	db *gorm.DB
}

// Open 打开（必要时创建）SQLite 数据库并完成迁移。
//
// SQLite 的并发写是整库锁：多个 Run 同时更新状态很容易撞上
// "database is locked"。这里的做法是 WAL + busy_timeout，并把连接池上限
// 设为 1，把写入彻底串行化 —— 本项目数据量极小，串行化的代价可以忽略，
// 换来的是这一类错误彻底消失。
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)",
		filepath.ToSlash(path),
	)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	s := &Store{db: gdb}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	return s.db.AutoMigrate(
		&User{}, &APIToken{}, &Credential{},
		&Project{}, &ProjectCommand{}, &ProjectEnv{}, &HookToken{},
		&Run{}, &RunStep{}, &AuditLog{}, &Setting{},
	)
}

// DB 暴露底层句柄，给需要自定义查询的调用方。
func (s *Store) DB() *gorm.DB { return s.db }

// Close 关闭数据库。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetSetting 读一条设置；不存在时返回空串。
func (s *Store) GetSetting(key string) (string, bool) {
	var v Setting
	if err := s.db.First(&v, "key = ?", key).Error; err != nil {
		return "", false
	}
	return v.Value, true
}

// SetSetting 写入/覆盖一条设置。
func (s *Store) SetSetting(key, value string) error {
	return s.db.Save(&Setting{Key: key, Value: value, UpdatedAt: time.Now()}).Error
}

// Audit 记一条审计日志（失败不影响主流程，调用方可忽略返回）。
func (s *Store) Audit(userID uint, username, action, target, detail, ip string) {
	_ = s.db.Create(&AuditLog{
		UserID: userID, Username: username, Action: action,
		Target: target, Detail: truncate(detail, 1000), IP: ip,
	}).Error
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
