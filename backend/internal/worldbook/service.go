package worldbook

import (
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/filestore"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// Service 负责世界书与条目的管理（数据存外部 JSON 文件），并提供激活扫描。
type Service struct {
	store   WorldBookStore
	counter tokenizer.Counter
}

// WorldBookStore 世界书存储接口
type WorldBookStore interface {
	Get(id string) (storage.WorldBook, error)
	List() []storage.WorldBook
	Exists(id string) bool
	Save(item storage.WorldBook) error
	Delete(id string) error
}

func NewService(store WorldBookStore, counter tokenizer.Counter) *Service {
	return &Service{store: store, counter: counter}
}

func (s *Service) List() []storage.WorldBook {
	return s.store.List()
}

func (s *Service) Get(id string) (*storage.WorldBook, error) {
	b, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateInput 新建世界书入参。
type CreateInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s *Service) Create(in CreateInput) (*storage.WorldBook, error) {
	now := time.Now()
	b := storage.WorldBook{
		ID:          filestore.GenerateSlug("wb", in.Name, s.store.Exists),
		Name:        in.Name,
		Description: in.Description,
		Entries:     []storage.WorldBookEntry{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.Save(b); err != nil {
		return nil, err
	}
	return &b, nil
}

// UpdateInput 更新世界书名称/描述的入参（nil 表示不修改）。
type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (s *Service) Update(id string, in UpdateInput) (*storage.WorldBook, error) {
	b, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		b.Name = *in.Name
	}
	if in.Description != nil {
		b.Description = in.Description
	}
	b.UpdatedAt = time.Now()
	if err := s.store.Save(b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// EntryInput 新建/更新条目的入参。指针字段为 nil 表示"不修改"，
// 传空串即可清空对应字段。
type EntryInput struct {
	Title       *string  `json:"title"`
	Content     *string  `json:"content"`
	Keywords    []string `json:"keywords"`
	Constant    *bool    `json:"constant"`
	Enabled     *bool    `json:"enabled"`
	Priority    *int     `json:"priority"`
	BudgetGroup *string  `json:"budget_group"`
	Notes       *string  `json:"notes"`
}

func (s *Service) AddEntry(bookID string, in EntryInput) (*storage.WorldBookEntry, error) {
	b, err := s.store.Get(bookID)
	if err != nil {
		return nil, err
	}
	title := derefStr(in.Title, "")
	content := derefStr(in.Content, "")
	tokens := s.counter.Count(title + "\n" + content)
	e := storage.WorldBookEntry{
		ID:          uuid.New().String(),
		WorldBookID: bookID,
		Title:       title,
		Content:     content,
		Keywords:    in.Keywords,
		Constant:    deref(in.Constant, false),
		Enabled:     deref(in.Enabled, true),
		Priority:    deref(in.Priority, 100),
		BudgetGroup: derefStr(in.BudgetGroup, "default"),
		TokenCount:  &tokens,
		Notes:       derefStr(in.Notes, ""),
		Metadata:    map[string]any{},
	}
	if e.Keywords == nil {
		e.Keywords = []string{}
	}
	b.Entries = append(cloneEntries(b.Entries), e)
	b.UpdatedAt = time.Now()
	if err := s.store.Save(b); err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateEntry 按条目 id 在所有世界书中定位并局部更新。
func (s *Service) UpdateEntry(entryID string, in EntryInput) (*storage.WorldBookEntry, error) {
	b, idx, err := s.locateEntry(entryID)
	if err != nil {
		return nil, err
	}
	e := &b.Entries[idx]
	if in.Title != nil {
		e.Title = *in.Title
	}
	if in.Content != nil {
		e.Content = *in.Content
	}
	if in.Keywords != nil {
		e.Keywords = in.Keywords
	}
	if in.Constant != nil {
		e.Constant = *in.Constant
	}
	if in.Enabled != nil {
		e.Enabled = *in.Enabled
	}
	if in.Priority != nil {
		e.Priority = *in.Priority
	}
	if in.BudgetGroup != nil {
		e.BudgetGroup = *in.BudgetGroup
	}
	if in.Notes != nil {
		e.Notes = *in.Notes
	}
	// 标题或内容变化时重算 token。
	if in.Title != nil || in.Content != nil {
		tokens := s.counter.Count(e.Title + "\n" + e.Content)
		e.TokenCount = &tokens
	}
	b.UpdatedAt = time.Now()
	if err := s.store.Save(*b); err != nil {
		return nil, err
	}
	out := b.Entries[idx]
	return &out, nil
}

func (s *Service) DeleteEntry(entryID string) error {
	b, idx, err := s.locateEntry(entryID)
	if err != nil {
		return err
	}
	b.Entries = append(b.Entries[:idx], b.Entries[idx+1:]...)
	b.UpdatedAt = time.Now()
	return s.store.Save(*b)
}

// locateEntry 在全部世界书中查找条目，返回所属世界书与条目下标。
// 返回的世界书持有 Entries 的副本，调用方可安全原地修改后 Save。
func (s *Service) locateEntry(entryID string) (*storage.WorldBook, int, error) {
	for _, b := range s.store.List() {
		for i, e := range b.Entries {
			if e.ID == entryID {
				book := b
				book.Entries = cloneEntries(b.Entries)
				return &book, i, nil
			}
		}
	}
	return nil, 0, filestore.ErrNotFound
}

// cloneEntries 复制条目切片，避免与 filestore 缓存共享底层数组。
func cloneEntries(in []storage.WorldBookEntry) []storage.WorldBookEntry {
	out := make([]storage.WorldBookEntry, len(in))
	copy(out, in)
	return out
}

// EnabledEntriesForBooks 聚合给定世界书集合中所有启用的条目（顺序由 Scan 再排序）。
func (s *Service) EnabledEntriesForBooks(bookIDs []string) []storage.WorldBookEntry {
	var out []storage.WorldBookEntry
	for _, id := range bookIDs {
		b, err := s.store.Get(id)
		if err != nil {
			continue // 绑定的世界书文件可能已被删除，跳过
		}
		for _, e := range b.Entries {
			if e.Enabled {
				out = append(out, e)
			}
		}
	}
	return out
}

// Activate 读取给定世界书集合的启用条目并在预算内扫描激活。
func (s *Service) Activate(bookIDs []string, scanText string, budget int) ([]Activated, []storage.WorldBookEntry) {
	entries := s.EnabledEntriesForBooks(bookIDs)
	return Scan(entries, scanText, budget, s.counter)
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func derefStr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
