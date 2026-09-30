package storage

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrDetailedLLMLoggingScope  = errors.New("详细日志范围必须为 all、user 或 conversation；指定范围需要有效的目标 ID，全部范围不能指定目标")
	ErrDetailedLLMLoggingTarget = errors.New("详细日志采集目标不存在或对话已删除，请重新选择")
)

func (s *SystemSettings) ValidateDetailedLLMLoggingScope() error {
	if s == nil {
		return ErrDetailedLLMLoggingScope
	}
	switch s.DetailedLLMLoggingScope {
	case "all":
		if s.DetailedLLMLoggingTargetID == nil {
			return nil
		}
	case "user", "conversation":
		if s.DetailedLLMLoggingTargetID != nil && *s.DetailedLLMLoggingTargetID != uuid.Nil {
			return nil
		}
	}
	return ErrDetailedLLMLoggingScope
}

// CaptureDetailedLLMLog uses trusted call attribution, including background jobs.
// Missing attribution or invalid settings fail closed without suppressing summaries.
func (s *SystemSettings) CaptureDetailedLLMLog(playerID, conversationID string) bool {
	if s == nil || !s.DetailedLLMLoggingEnabled || s.ValidateDetailedLLMLoggingScope() != nil {
		return false
	}
	var candidate string
	switch s.DetailedLLMLoggingScope {
	case "all":
		return true
	case "user":
		candidate = playerID
	case "conversation":
		candidate = conversationID
	}
	id, err := uuid.Parse(candidate)
	return err == nil && id != uuid.Nil && id == *s.DetailedLLMLoggingTargetID
}

func validateDetailedLLMLoggingTarget(tx *gorm.DB, previous, next *SystemSettings) error {
	if next.DetailedLLMLoggingTargetID == nil {
		return nil
	}
	changed := previous.DetailedLLMLoggingScope != next.DetailedLLMLoggingScope ||
		previous.DetailedLLMLoggingTargetID == nil || *previous.DetailedLLMLoggingTargetID != *next.DetailedLLMLoggingTargetID
	if !changed && (previous.DetailedLLMLoggingEnabled || !next.DetailedLLMLoggingEnabled) {
		return nil
	}
	query := "SELECT EXISTS (SELECT 1 FROM users WHERE id = ?)"
	if next.DetailedLLMLoggingScope == "conversation" {
		query = "SELECT EXISTS (SELECT 1 FROM conversations WHERE id = ? AND deleted_at IS NULL)"
	}
	var exists bool
	if err := tx.Raw(query, *next.DetailedLLMLoggingTargetID).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return ErrDetailedLLMLoggingTarget
	}
	return nil
}
