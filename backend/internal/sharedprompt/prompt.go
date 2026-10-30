// Package sharedprompt loads the platform common prompt every gameplay pack goes through.
package sharedprompt

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	shareddata "github.com/ialtone/fatecraft/data/shared"
	"github.com/ialtone/fatecraft/internal/storage"
)

type commonPromptFile struct {
	ID       string                  `json:"id"`
	Name     string                  `json:"name"`
	Segments []storage.PromptSegment `json:"segments"`
}

var (
	commonOnce     sync.Once
	commonSegments []storage.PromptSegment
	commonErr      error
)

// Segments returns the platform shared prompt segments.
func Segments() ([]storage.PromptSegment, error) {
	commonOnce.Do(func() {
		var file commonPromptFile
		if err := json.Unmarshal(shareddata.CommonPromptJSON, &file); err != nil {
			commonErr = fmt.Errorf("解析公用 Prompt: %w", err)
			return
		}
		if file.ID == "" || len(file.Segments) == 0 {
			commonErr = fmt.Errorf("公用 Prompt 缺少 id 或 segments")
			return
		}
		for i, segment := range file.Segments {
			if segment.Key == "" || segment.Role != storage.RoleSystem || !segment.Enabled ||
				!segment.Required || strings.TrimSpace(segment.Content) == "" {
				commonErr = fmt.Errorf("公用 Prompt 分段 %d 无效", i)
				return
			}
		}
		commonSegments = append([]storage.PromptSegment(nil), file.Segments...)
	})
	if commonErr != nil {
		return nil, commonErr
	}
	return append([]storage.PromptSegment(nil), commonSegments...), nil
}

// Ensure appends missing shared prompt segments so every pack goes through them.
func Ensure(segments []storage.PromptSegment) ([]storage.PromptSegment, error) {
	shared, err := Segments()
	if err != nil {
		return segments, err
	}
	have := make(map[string]bool, len(segments))
	for _, segment := range segments {
		have[segment.Key] = true
	}
	out := append([]storage.PromptSegment(nil), segments...)
	for _, segment := range shared {
		if have[segment.Key] {
			continue
		}
		out = append(out, segment)
		have[segment.Key] = true
	}
	return out, nil
}
