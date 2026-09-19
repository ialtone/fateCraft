package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ialtone/fatecraft/internal/storage"
)

type AgentSource struct {
	SchemaVersion  int                   `json:"schema_version"`
	ConversationID string                `json:"conversation_id"`
	BranchEpoch    int64                 `json:"branch_epoch"`
	Previous       json.RawMessage       `json:"previous_memory,omitempty"`
	Turns          []SourceTurn          `json:"turns"`
	Revisions      []SourceRevision      `json:"state_revisions,omitempty"`
	Rolls          []SourceRoll          `json:"rolls,omitempty"`
	Events         []SourceCampaignEvent `json:"campaign_events,omitempty"`
}

type SourceTurn struct {
	ID       string          `json:"turn_id"`
	Sequence int64           `json:"turn_seq"`
	Trigger  string          `json:"trigger_type"`
	Messages []SourceMessage `json:"messages"`
}

type SourceMessage struct {
	ID       string `json:"message_id"`
	Sequence int64  `json:"message_seq"`
	Role     string `json:"role"`
	Content  string `json:"content"`
}

type SourceRevision struct {
	ID     string         `json:"id"`
	TurnID string         `json:"turn_id"`
	Source string         `json:"source"`
	Patch  map[string]any `json:"patch"`
}

type SourceRoll struct {
	ID      string `json:"id"`
	TurnID  string `json:"turn_id"`
	Title   string `json:"title"`
	Outcome string `json:"outcome"`
}

type SourceCampaignEvent struct {
	ID       string         `json:"id"`
	TurnID   string         `json:"turn_id"`
	Sequence int64          `json:"sequence"`
	Type     string         `json:"event_type"`
	Payload  map[string]any `json:"payload"`
}

func NormalizeSource(source *storage.MemorySource) (AgentSource, EvidenceSet, []byte, error) {
	out := AgentSource{
		SchemaVersion: SchemaVersion, ConversationID: source.Conversation.ID.String(),
		BranchEpoch: source.Conversation.BranchEpoch,
	}
	evidence := EvidenceSet{
		"message": {}, "state_revision": {}, "roll": {}, "campaign_event": {},
	}
	if source.Previous != nil {
		out.Previous = append(json.RawMessage(nil), source.Previous.Content...)
		var previous Document
		if err := json.Unmarshal(source.Previous.Content, &previous); err != nil {
			return out, nil, nil, fmt.Errorf("读取上一版记忆失败: %w", err)
		}
		collectDocumentEvidence(previous, evidence)
	}
	turnIndex := make(map[string]int, len(source.Turns))
	for _, turn := range source.Turns {
		turnIndex[turn.ID.String()] = len(out.Turns)
		out.Turns = append(out.Turns, SourceTurn{ID: turn.ID.String(), Sequence: turn.TurnSeq, Trigger: turn.TriggerType})
	}
	for _, message := range source.Messages {
		if message.TurnID == nil {
			continue
		}
		index, ok := turnIndex[message.TurnID.String()]
		if !ok {
			continue
		}
		out.Turns[index].Messages = append(out.Turns[index].Messages, SourceMessage{
			ID: message.ID.String(), Sequence: message.MessageSeq, Role: message.Role, Content: message.Content,
		})
		evidence["message"][message.ID.String()] = struct{}{}
	}
	for _, revision := range source.Revisions {
		if revision.TurnID == nil {
			continue
		}
		out.Revisions = append(out.Revisions, SourceRevision{
			ID: revision.ID.String(), TurnID: revision.TurnID.String(), Source: revision.Source, Patch: revision.Patch,
		})
		evidence["state_revision"][revision.ID.String()] = struct{}{}
	}
	for _, roll := range source.Rolls {
		if roll.TurnID == nil {
			continue
		}
		out.Rolls = append(out.Rolls, SourceRoll{
			ID: roll.ID, TurnID: roll.TurnID.String(), Title: roll.TitleSnapshot,
			Outcome: fmt.Sprintf("category=%s success=%t", roll.Category, roll.Success),
		})
		evidence["roll"][roll.ID] = struct{}{}
	}
	for _, event := range source.Events {
		if event.TurnID == nil {
			continue
		}
		out.Events = append(out.Events, SourceCampaignEvent{
			ID: event.ID.String(), TurnID: event.TurnID.String(), Sequence: event.Sequence,
			Type: event.EventType, Payload: event.Payload,
		})
		evidence["campaign_event"][event.ID.String()] = struct{}{}
	}
	encoded, err := json.Marshal(out)
	return out, evidence, encoded, err
}

func SourceHash(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func Fingerprint(model, baseURL, protocol string, maxTokens int) string {
	encoded, _ := json.Marshal(map[string]any{
		"schema_version": SchemaVersion, "prompt_version": PromptVersion,
		"renderer_version": RendererVersion, "model": model, "base_url": baseURL,
		"protocol": protocol, "max_tokens": maxTokens,
	})
	return SourceHash(encoded)
}

func collectDocumentEvidence(doc Document, evidence EvidenceSet) {
	add := func(refs []EvidenceRef) {
		for _, ref := range refs {
			if evidence[ref.Kind] == nil {
				evidence[ref.Kind] = map[string]struct{}{}
			}
			evidence[ref.Kind][ref.ID] = struct{}{}
		}
	}
	for _, item := range doc.Facts {
		add(item.EvidenceRefs)
	}
	for _, item := range doc.Entities {
		add(item.EvidenceRefs)
	}
	for _, item := range doc.Goals {
		add(item.EvidenceRefs)
	}
	for _, item := range doc.OpenThreads {
		add(item.EvidenceRefs)
	}
	for _, item := range doc.PlayerChoices {
		add(item.EvidenceRefs)
	}
}
