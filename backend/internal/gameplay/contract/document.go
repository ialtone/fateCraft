package contract

import "encoding/json"

// RuntimeDocument is the serialized module contract. Database identity and
// timestamps belong to the storage adapter, not to a rule module.
type RuntimeDocument struct {
	EngineKey        string
	EngineVersion    int
	Mode             string
	CampaignConfig   *json.RawMessage
	StateSchema      json.RawMessage
	ActionConfig     json.RawMessage
	SettlementConfig json.RawMessage
	Presentation     json.RawMessage
	NarrativeGuards  json.RawMessage
}

const (
	SourceAI     = "ai"
	SourceSystem = "system"
	SourceModule = "module"
)

// ValidStableKey validates a module or content identity without reading storage.
func ValidStableKey(key string) bool { return len(key) <= 128 && validStableKey(key) }
