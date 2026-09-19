// Package guidance defines optional, topic-neutral player intent suggestions.
package guidance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const ToolName = "fc_present_guidance"
const MetadataKey = "action_guidance"

type Option struct {
	Label   string `json:"label"`
	Message string `json:"message"`
}

type Proposal struct {
	Title   string   `json:"title"`
	Options []Option `json:"options"`
}

type Item struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type Active struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// Snapshot belongs to the assistant message and shares its branch lifecycle.
// Messages remain server-side: clients submit only ID + item key.
type Snapshot struct {
	Proposal         Proposal `json:"proposal"`
	StateFingerprint string   `json:"state_fingerprint"`
	CampaignRevision int64    `json:"campaign_revision"`
}

func Fingerprint(state any) string {
	raw, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func (s Snapshot) Public(id string) Active {
	a := Active{ID: id, Title: s.Proposal.Title, Items: make([]Item, 0, len(s.Proposal.Options))}
	for i, option := range s.Proposal.Options {
		a.Items = append(a.Items, Item{Key: fmt.Sprintf("option-%d", i+1), Label: option.Label})
	}
	return a
}

func (s Snapshot) Message(key string) (string, bool) {
	for i, option := range s.Proposal.Options {
		if key == fmt.Sprintf("option-%d", i+1) {
			return option.Message, true
		}
	}
	return "", false
}

func Decode(raw []byte) (Proposal, error) {
	var p Proposal
	if len(raw) > 16384 {
		return p, errors.New("行动提示参数过长")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, errors.New("行动提示必须是单个完整对象")
	}
	p.Title = strings.TrimSpace(p.Title)
	if !validText(p.Title, 80) || len(p.Options) < 2 || len(p.Options) > 4 {
		return p, errors.New("行动提示需要标题和 2 到 4 个选项")
	}
	labels, messages := map[string]bool{}, map[string]bool{}
	for i := range p.Options {
		o := &p.Options[i]
		o.Label, o.Message = strings.TrimSpace(o.Label), strings.TrimSpace(o.Message)
		if !validText(o.Label, 60) || !validText(o.Message, 500) || labels[o.Label] || messages[o.Message] {
			return p, errors.New("选项需包含不重复的短标题和玩家意图文本")
		}
		labels[o.Label], messages[o.Message] = true, true
	}
	return p, nil
}

func validText(s string, limit int) bool {
	return s != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && !strings.ContainsRune(s, '\x00')
}

func Schema() map[string]any {
	text := func(max int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"title", "options"},
		"properties": map[string]any{
			"title": text(80),
			"options": map[string]any{
				"type": "array", "minItems": 2, "maxItems": 4,
				"items": map[string]any{
					"type": "object", "additionalProperties": false, "required": []string{"label", "message"},
					"properties": map[string]any{"label": text(60), "message": text(500)},
				},
			},
		},
	}
}

type Selection struct {
	ID  string `json:"guidance_id"`
	Key string `json:"item_key"`
}

type selectionKey struct{}

func WithSelection(ctx context.Context, s Selection) context.Context {
	return context.WithValue(ctx, selectionKey{}, s)
}
func SelectionFromContext(ctx context.Context) (Selection, bool) {
	s, ok := ctx.Value(selectionKey{}).(Selection)
	return s, ok
}
