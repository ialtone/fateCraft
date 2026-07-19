package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrReadOnly 表示写操作落在了只读来源上（预设包模板不可运行时增改；
// 或本 MultiSourceStore 未配置可写目录）。API 层据此返回 405。
var ErrReadOnly = errors.New("该资源为只读，不支持运行时修改")

// PresetPack 表示一个预设包的元信息
type PresetPack struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Description   string   `json:"description"`
	Author        string   `json:"author"`
	Icon          string   `json:"icon"`
	Tags          []string `json:"tags"`
	DefaultConfig struct {
		RecommendedCharacter string   `json:"recommended_character"`
		RecommendedPersona   string   `json:"recommended_persona"`
		RecommendedScenario  string   `json:"recommended_scenario"`
		RecommendedPrompt    string   `json:"recommended_prompt"`
		RequiredWorldBooks   []string `json:"required_worldbooks"`
	} `json:"default_config"`
	QuickStartGuide string   `json:"quick_start_guide"`
	Features        []string `json:"features"`
	Dependencies    []string `json:"dependencies"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// MultiSourceStore 支持从多个目录加载资源（预设包 + 共享资源）。
//
// 读路径合并全部来源；写路径（Save/Delete）仅作用于可写目录 writable：
//   - writable 为 nil：整个 store 只读，Save/Delete 返回 ErrReadOnly；
//   - writable 非 nil：新建/改删只落到该目录（通常是 shared/<type>），
//     预设包内的模板仍视为只读（改写预设包资源返回 ErrReadOnly）。
//
// 运行时写会并发改动 items 缓存，故用 mu 保护（Load 期间为单线程，Load 后
// 只有 Save/Delete/读取会触及缓存）。
type MultiSourceStore[T Entity] struct {
	mu         sync.RWMutex
	items      map[string]T
	basePath   string    // 基础路径，如 backend/data
	sourceDirs []string  // 源目录列表，如 ["preset_packs/*/characters", "shared/characters"]
	writable   *Store[T] // 可写目录的落盘存储；nil 表示整体只读
}

// NewMultiSourceStore 创建一个支持多源加载的存储。
// writeDir 为相对 basePath 的可写目录（如 "shared/characters"）；
// 传空串表示该类资源整体只读（如世界书，必须属于某个预设包）。
func NewMultiSourceStore[T Entity](basePath string, sourceDirs []string, writeDir string) *MultiSourceStore[T] {
	var writable *Store[T]
	if writeDir != "" {
		writable = NewStore[T](filepath.Join(basePath, writeDir))
	}
	return &MultiSourceStore[T]{
		items:      make(map[string]T),
		basePath:   basePath,
		sourceDirs: sourceDirs,
		writable:   writable,
	}
}

// Load 从所有源目录加载资源
func (s *MultiSourceStore[T]) Load() error {
	// 先加载可写目录自身的缓存（用于区分「可写资源」与「预设包只读资源」）。
	if s.writable != nil {
		if err := s.writable.Load(); err != nil {
			return fmt.Errorf("加载可写目录失败: %w", err)
		}
	}
	for _, sourceDir := range s.sourceDirs {
		pattern := filepath.Join(s.basePath, sourceDir)

		// 如果包含通配符，需要先展开
		if strings.Contains(pattern, "*") {
			dirs, err := filepath.Glob(pattern)
			if err != nil {
				return fmt.Errorf("展开路径 %s 失败: %w", pattern, err)
			}
			for _, dir := range dirs {
				if err := s.loadFromDir(dir); err != nil {
					// 目录可能不存在，跳过
					continue
				}
			}
		} else {
			if err := s.loadFromDir(pattern); err != nil {
				// 目录可能不存在，跳过
				continue
			}
		}
	}
	return nil
}

// loadFromDir 从单个目录加载
func (s *MultiSourceStore[T]) loadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// 从路径推断预设包ID
	packID := extractPackIDFromPath(dir)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取文件 %s 失败: %w", path, err)
		}

		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return fmt.Errorf("解析文件 %s 失败: %w", path, err)
		}

		// 如果资源支持 SetPackID 方法，设置包ID
		if setter, ok := any(&item).(interface{ SetPackID(string) }); ok && packID != "" {
			setter.SetPackID(packID)
		}

		s.items[item.GetID()] = item
	}
	return nil
}

// extractPackIDFromPath 从文件路径提取预设包ID
// 例如：preset_packs/reincarnation-trials/characters -> reincarnation-trials
// 例如：shared/characters -> ""
func extractPackIDFromPath(path string) string {
	// 标准化路径分隔符
	path = filepath.ToSlash(path)

	// 检查是否在 preset_packs 目录下
	if strings.Contains(path, "preset_packs/") {
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if part == "preset_packs" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}

	// shared 目录或其他情况返回空
	return ""
}

// Get 获取单个资源
func (s *MultiSourceStore[T]) Get(id string) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var zero T
	item, ok := s.items[id]
	if !ok {
		return zero, ErrNotFound
	}
	return item, nil
}

// List 列出所有资源
func (s *MultiSourceStore[T]) List() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]T, 0, len(s.items))
	for _, item := range s.items {
		result = append(result, item)
	}
	return result
}

// Exists 检查资源是否存在
func (s *MultiSourceStore[T]) Exists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.items[id]
	return ok
}

// Save 保存资源：仅落到可写目录（如 shared/<type>）。
//   - 未配置可写目录：返回 ErrReadOnly；
//   - 目标 id 已属于某预设包（在合并缓存中但不在可写目录）：返回 ErrReadOnly；
//   - 其余情况（新建或改写既有共享资源）：写盘并更新合并缓存。
func (s *MultiSourceStore[T]) Save(item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writable == nil {
		return ErrReadOnly
	}
	id := item.GetID()
	if _, inMerged := s.items[id]; inMerged && !s.writable.Exists(id) {
		// 已存在但不在可写目录 → 来自预设包，只读。
		return ErrReadOnly
	}
	if err := s.writable.Save(item); err != nil {
		return err
	}
	s.items[id] = item
	return nil
}

// Delete 删除资源：仅能删除可写目录中的资源。
//   - 未配置可写目录：返回 ErrReadOnly；
//   - 目标属于预设包（不在可写目录但在合并缓存）：返回 ErrReadOnly；
//   - 目标不存在：返回 ErrNotFound；
//   - 其余：删盘并从合并缓存移除。
func (s *MultiSourceStore[T]) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writable == nil {
		return ErrReadOnly
	}
	if !s.writable.Exists(id) {
		if _, inMerged := s.items[id]; inMerged {
			return ErrReadOnly // 预设包资源不可删
		}
		return ErrNotFound
	}
	if err := s.writable.Delete(id); err != nil {
		return err
	}
	delete(s.items, id)
	return nil
}

// LoadPresetPacks 加载所有预设包的元信息
func LoadPresetPacks(basePath string) ([]PresetPack, error) {
	packsDir := filepath.Join(basePath, "preset_packs")
	entries, err := os.ReadDir(packsDir)
	if err != nil {
		return nil, fmt.Errorf("读取预设包目录失败: %w", err)
	}

	var packs []PresetPack
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		packFile := filepath.Join(packsDir, entry.Name(), "pack.json")
		data, err := os.ReadFile(packFile)
		if err != nil {
			continue // 跳过没有 pack.json 的目录
		}

		var pack PresetPack
		if err := json.Unmarshal(data, &pack); err != nil {
			continue
		}

		packs = append(packs, pack)
	}

	return packs, nil
}
