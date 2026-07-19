package filestore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeTpl 是测试用的最小模板实体。
type fakeTpl struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (f fakeTpl) GetID() string           { return f.ID }
func (f fakeTpl) GetUpdatedAt() time.Time { return f.UpdatedAt }

// Save 应落盘且重新 Load 后可读回；Delete 应同时移除文件与缓存。
func TestStore_SaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewStore[fakeTpl](dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	a := fakeTpl{ID: "alpha", Name: "A", UpdatedAt: time.Now()}
	if err := s.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.json")); err != nil {
		t.Fatalf("文件未落盘: %v", err)
	}

	// 新实例重新加载应读到同一条数据。
	s2 := NewStore[fakeTpl](dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	got, err := s2.Get("alpha")
	if err != nil || got.Name != "A" {
		t.Fatalf("读回失败: %v %+v", err, got)
	}

	if err := s2.Delete("alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s2.Get("alpha"); err != ErrNotFound {
		t.Fatalf("删除后应返回 ErrNotFound，得到 %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.json")); !os.IsNotExist(err) {
		t.Fatal("删除后文件应不存在")
	}
}

// 非法 id（路径穿越等）应被拒绝。
func TestStore_RejectInvalidID(t *testing.T) {
	s := NewStore[fakeTpl](t.TempDir())
	if err := s.Save(fakeTpl{ID: "../evil"}); err == nil {
		t.Fatal("包含路径分隔符的 id 应被拒绝")
	}
	if err := s.Save(fakeTpl{ID: ""}); err == nil {
		t.Fatal("空 id 应被拒绝")
	}
}

// List 应按更新时间倒序。
func TestStore_ListOrder(t *testing.T) {
	s := NewStore[fakeTpl](t.TempDir())
	_ = s.Load()
	old := time.Now().Add(-time.Hour)
	_ = s.Save(fakeTpl{ID: "old", UpdatedAt: old})
	_ = s.Save(fakeTpl{ID: "new", UpdatedAt: time.Now()})
	got := s.List()
	if len(got) != 2 || got[0].ID != "new" {
		t.Fatalf("List 应按更新时间倒序: %+v", got)
	}
}

// Slug 生成：ASCII 名称直接转换并去重；中文名称用前缀+随机码兜底。
func TestGenerateSlug(t *testing.T) {
	if s := Slugify("Hello World 2"); s != "hello-world-2" {
		t.Fatalf("Slugify 结果错误: %q", s)
	}
	taken := map[string]bool{"hello": true}
	exists := func(id string) bool { return taken[id] }
	if s := GenerateSlug("wb", "Hello", exists); s != "hello-2" {
		t.Fatalf("冲突时应追加序号: %q", s)
	}
	s := GenerateSlug("wb", "命运茶馆", exists)
	if len(s) != len("wb-")+6 || s[:3] != "wb-" {
		t.Fatalf("中文名称应生成 wb-<随机码>: %q", s)
	}
}
