package contract

import "fmt"

const (
	RunActive       = "active"
	RunCompleted    = "completed"
	RunFailed       = "failed"
	AudiencePlayer  = "player"
	AudienceModel   = "model"
	AudienceInquiry = "inquiry"
)

// RunState contains the module's bounded authoritative state. Persistence,
// revision locks, branch epochs and account assets belong to mode adapters.
type RunState struct {
	SchemaVersion  int            `json:"schema_version"`
	Revision       int64          `json:"revision"`
	GameTime       int64          `json:"game_time"`
	Status         string         `json:"status"`
	OutcomeKey     string         `json:"outcome_key,omitempty"`
	Data           map[string]any `json:"data"`
	RandomEvidence []RandomDraw   `json:"random_evidence,omitempty"`
}

// RandomDraw is server-generated audit evidence for an already resolved draw.
// It stays in the authoritative checkpoint, outside public state projections.
type RandomDraw struct {
	Purpose string `json:"purpose"`
	Upper   int64  `json:"upper"`
	Value   int64  `json:"value"`
}

type InitialInput struct {
	PersonaKey string
	Random     RandomSource
}

// RandomSource is supplied only during initialization or execution. Preview
// and projection cannot ask for future random values.
type RandomSource interface {
	Intn(purpose string, upper int64) (int64, error)
}

type RuntimeParameter struct {
	Key          string            `json:"key"`
	Label        string            `json:"label"`
	Type         string            `json:"type"`
	Required     bool              `json:"required"`
	Minimum      int64             `json:"minimum,omitempty"`
	Maximum      int64             `json:"maximum,omitempty"`
	Options      []string          `json:"options,omitempty"`
	OptionLabels map[string]string `json:"option_labels,omitempty"`
}

type RuntimeAction struct {
	Ending      bool               `json:"ending,omitempty"`
	Key         string             `json:"key"`
	Label       string             `json:"label"`
	Description string             `json:"description"`
	Actors      []string           `json:"actors"`
	Parameters  []RuntimeParameter `json:"parameters"`
	Writes      []string           `json:"writes"`
	Events      []string           `json:"events"`
}

type ActionPreview struct {
	Action     RuntimeAction     `json:"action"`
	Available  bool              `json:"available"`
	Reason     string            `json:"reason,omitempty"`
	Minutes    int64             `json:"minutes"`
	Costs      map[string]int64  `json:"costs,omitempty"`
	Risk       string            `json:"risk,omitempty"`
	CostLabels map[string]string `json:"cost_labels,omitempty"`
	Plans      []ActionPlan      `json:"plans,omitempty"`
}

// ActionPlan describes one concrete parameter choice at the preview revision.
// Plans never authorize an action; the module rechecks the current state.
type ActionPlan struct {
	Label     string           `json:"label"`
	Arguments map[string]any   `json:"arguments"`
	Minutes   int64            `json:"minutes"`
	Costs     map[string]int64 `json:"costs,omitempty"`
	Risk      string           `json:"risk,omitempty"`
}

type RuntimeRequest struct {
	ActionKey string
	Actor     string
	Arguments map[string]any
	Random    RandomSource
}

type RuntimeEvent struct {
	Kind       string         `json:"kind"`
	Visibility string         `json:"visibility"`
	Data       map[string]any `json:"data"`
}

type Transition struct {
	State  RunState
	Events []RuntimeEvent
	Result map[string]any
}

// RuntimeModule is implemented by compiled trusted modules. It receives no
// database, account balance or unrestricted state writer. The same rules run
// behind Challenge and Campaign transaction adapters.
type RuntimeModule interface {
	Actions() []RuntimeAction
	Initialize(Content, InitialInput) (RunState, error)
	ValidateState(Content, RunState) error
	Preview(Content, RunState, string) ([]ActionPreview, error)
	Execute(Content, RunState, RuntimeRequest) (Transition, error)
	Project(Content, RunState, string) (map[string]any, error)
}

type RuleViolation struct{ Code, Message string }

func (e *RuleViolation) Error() string { return e.Message }
func Reject(code, format string, args ...any) error {
	return &RuleViolation{Code: code, Message: fmt.Sprintf(format, args...)}
}

func (a RuntimeAction) ArgumentSchema() map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, parameter := range a.Parameters {
		property := map[string]any{"type": parameter.Type, "description": parameter.Label}
		switch parameter.Type {
		case "integer":
			property["minimum"], property["maximum"] = parameter.Minimum, parameter.Maximum
		case "string":
			property["minLength"], property["maxLength"] = 1, 128
			if len(parameter.Options) > 0 {
				property["enum"] = parameter.Options
			}
		}
		properties[parameter.Key] = property
		if parameter.Required {
			required = append(required, parameter.Key)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
