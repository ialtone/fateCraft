package prompt

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/filestore"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Service 负责 Prompt 预设与分段的管理（数据存外部 JSON 文件）。
type Service struct {
	store PromptStore
}

// PromptStore Prompt 存储接口
type PromptStore interface {
	Get(id string) (storage.PromptPreset, error)
	List() []storage.PromptPreset
	Exists(id string) bool
	Save(item storage.PromptPreset) error
	Delete(id string) error
}

func NewService(store PromptStore) *Service {
	return &Service{store: store}
}

func (s *Service) List() []storage.PromptPreset {
	return s.store.List()
}

// Get 读取预设，分段按 position 排序返回。
func (s *Service) Get(id string) (*storage.PromptPreset, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	p.Segments = sortedSegments(p.Segments)
	return &p, nil
}

// CreateInput 新建预设的入参。
type CreateInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s *Service) Create(in CreateInput) (*storage.PromptPreset, error) {
	now := time.Now()
	p := storage.PromptPreset{
		ID:          filestore.GenerateSlug("prompt", in.Name, s.store.Exists),
		Name:        in.Name,
		Description: in.Description,
		Segments:    []storage.PromptSegment{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.Save(p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) Update(id string, name string, description *string) (*storage.PromptPreset, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	if name != "" {
		p.Name = name
	}
	p.Description = description
	p.UpdatedAt = time.Now()
	if err := s.store.Save(p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// SegmentInput 新建/更新分段的入参。
type SegmentInput struct {
	Key      string `json:"key"`
	Role     string `json:"role"`
	Content  string `json:"content"`
	Enabled  *bool  `json:"enabled"`
	Required *bool  `json:"required"`
	Priority *int   `json:"priority"`
	Position *int   `json:"position"`
}

func (s *Service) AddSegment(presetID string, in SegmentInput) (*storage.PromptSegment, error) {
	p, err := s.store.Get(presetID)
	if err != nil {
		return nil, err
	}
	seg := storage.PromptSegment{
		ID:       uuid.New().String(),
		PresetID: presetID,
		Key:      in.Key,
		Role:     in.Role,
		Content:  in.Content,
		Enabled:  boolOr(in.Enabled, true),
		Required: boolOr(in.Required, false),
		Priority: intOr(in.Priority, 100),
		Position: intOr(in.Position, 0),
	}
	if seg.Role == "" {
		seg.Role = storage.RoleSystem
	}
	p.Segments = append(cloneSegments(p.Segments), seg)
	p.UpdatedAt = time.Now()
	if err := s.store.Save(p); err != nil {
		return nil, err
	}
	return &seg, nil
}

// UpdateSegment 按分段 id 在所有预设中定位并局部更新。
func (s *Service) UpdateSegment(segID string, in SegmentInput) (*storage.PromptSegment, error) {
	p, idx, err := s.locateSegment(segID)
	if err != nil {
		return nil, err
	}
	seg := &p.Segments[idx]
	if in.Key != "" {
		seg.Key = in.Key
	}
	if in.Role != "" {
		seg.Role = in.Role
	}
	if in.Content != "" {
		seg.Content = in.Content
	}
	if in.Enabled != nil {
		seg.Enabled = *in.Enabled
	}
	if in.Required != nil {
		seg.Required = *in.Required
	}
	if in.Priority != nil {
		seg.Priority = *in.Priority
	}
	if in.Position != nil {
		seg.Position = *in.Position
	}
	p.UpdatedAt = time.Now()
	if err := s.store.Save(*p); err != nil {
		return nil, err
	}
	out := p.Segments[idx]
	return &out, nil
}

func (s *Service) DeleteSegment(segID string) error {
	p, idx, err := s.locateSegment(segID)
	if err != nil {
		return err
	}
	p.Segments = append(p.Segments[:idx], p.Segments[idx+1:]...)
	p.UpdatedAt = time.Now()
	return s.store.Save(*p)
}

// locateSegment 在全部预设中查找分段，返回所属预设与分段下标。
// 返回的预设持有 Segments 的副本，调用方可安全原地修改后 Save。
func (s *Service) locateSegment(segID string) (*storage.PromptPreset, int, error) {
	for _, p := range s.store.List() {
		for i, seg := range p.Segments {
			if seg.ID == segID {
				preset := p
				preset.Segments = cloneSegments(p.Segments)
				return &preset, i, nil
			}
		}
	}
	return nil, 0, filestore.ErrNotFound
}

// sortedSegments 返回按 position 升序排列的分段副本。
func sortedSegments(in []storage.PromptSegment) []storage.PromptSegment {
	out := cloneSegments(in)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// cloneSegments 复制分段切片，避免与 filestore 缓存共享底层数组。
func cloneSegments(in []storage.PromptSegment) []storage.PromptSegment {
	out := make([]storage.PromptSegment, len(in))
	copy(out, in)
	return out
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}
