// Package filestore 提供基于 JSON 文件的模板存储：
// 每个模板对应一个 <数据目录>/<类型>/<slug>.json 文件，
// 启动时全量加载进内存，读走缓存、写同时落盘并更新缓存。
package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound 表示模板文件/缓存中不存在该 id。
var ErrNotFound = errors.New("模板不存在")

// Entity 是可被文件存储的模板实体约束。
type Entity interface {
	// GetID 返回 slug 形式的 id，同时也是文件名（不含扩展名）。
	GetID() string
	// GetUpdatedAt 用于列表排序（最近更新优先）。
	GetUpdatedAt() time.Time
}

// validID 限制 id 只能由字母、数字、点、下划线、连字符组成，防止路径穿越。
var validID = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Store 是某一类模板的内存缓存 + 文件落盘存储，读写并发安全。
type Store[T Entity] struct {
	dir   string
	mu    sync.RWMutex
	items map[string]T
}

// NewStore 创建指向 dir 的存储；调用 Load 之前缓存为空。
func NewStore[T Entity](dir string) *Store[T] {
	return &Store[T]{dir: dir, items: map[string]T{}}
}

// Load 确保目录存在并把目录下全部 *.json 读进内存（重复调用即整体重载）。
func (s *Store[T]) Load() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败: %w", s.dir, err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("读取数据目录 %s 失败: %w", s.dir, err)
	}
	items := map[string]T{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取模板文件 %s 失败: %w", path, err)
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return fmt.Errorf("解析模板文件 %s 失败: %w", path, err)
		}
		id := item.GetID()
		if id == "" {
			// 文件内缺少 id 时以文件名兜底。
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		items[id] = item
	}
	s.mu.Lock()
	s.items = items
	s.mu.Unlock()
	return nil
}

// List 返回全部模板，按更新时间倒序（与原数据库行为一致）。
func (s *Store[T]) List() []T {
	s.mu.RLock()
	out := make([]T, 0, len(s.items))
	for _, v := range s.items {
		out = append(out, v)
	}
	s.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].GetUpdatedAt(), out[j].GetUpdatedAt()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].GetID() < out[j].GetID()
	})
	return out
}

// Get 按 id 读取单个模板。
func (s *Store[T]) Get(id string) (T, error) {
	s.mu.RLock()
	item, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		var zero T
		return zero, ErrNotFound
	}
	return item, nil
}

// Exists 判断 id 是否已存在。
func (s *Store[T]) Exists(id string) bool {
	s.mu.RLock()
	_, ok := s.items[id]
	s.mu.RUnlock()
	return ok
}

// Save 写盘并更新缓存（新建与更新同一入口）。
func (s *Store[T]) Save(item T) error {
	id := item.GetID()
	if !validID.MatchString(id) {
		return fmt.Errorf("非法的模板 id: %q", id)
	}
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化模板 %s 失败: %w", id, err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(filepath.Join(s.dir, id+".json"), data); err != nil {
		return err
	}
	s.items[id] = item
	return nil
}

// Delete 删除文件并移除缓存；id 不存在时返回 ErrNotFound。
func (s *Store[T]) Delete(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("非法的模板 id: %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除模板文件 %s 失败: %w", id, err)
	}
	delete(s.items, id)
	return nil
}

// writeFileAtomic 先写临时文件再替换，尽量避免半截文件。
// Windows 上 rename 不能覆盖已存在文件，故先删除旧文件（调用方已持锁）。
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入临时文件 %s 失败: %w", tmp, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		os.Remove(tmp)
		return fmt.Errorf("替换模板文件 %s 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("落盘模板文件 %s 失败: %w", path, err)
	}
	return nil
}
