package kernel

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const ModuleStatePath = "module_state"

// Envelope is stored only in the internal, module-owned conversation field.
// Seed is decimal text so JSON round trips never lose integer precision.
type Envelope struct {
	Engine contract.EngineRef `json:"engine"`
	Seed   string             `json:"seed"`
	State  contract.RunState  `json:"state"`
}

func NewEnvelope(module contract.RuntimeModule, content contract.Content, persona string) (Envelope, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Envelope{}, err
	}
	seed := int64(binary.BigEndian.Uint64(bytes[:]) & 0x7fffffffffffffff)
	state, err := Initialize(module, content, contract.InitialInput{PersonaKey: persona, Random: NewRandom(seed, "initial")})
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Engine: content.Runtime.Engine, Seed: strconv.FormatInt(seed, 10), State: state}, nil
}

func ReadEnvelope(content contract.Content, state map[string]any) (Envelope, error) {
	raw, ok := state[ModuleStatePath]
	if !ok {
		return Envelope{}, fmt.Errorf("可信玩法状态缺失，不能用默认值重建存档")
	}
	envelope, err := decodeEnvelope(raw)
	if err != nil {
		return Envelope{}, err
	}
	if envelope.Engine != content.Runtime.Engine {
		return Envelope{}, fmt.Errorf("玩法状态与固定模块版本不一致")
	}
	if _, err := strconv.ParseInt(envelope.Seed, 10, 64); err != nil {
		return Envelope{}, fmt.Errorf("玩法随机种子无效")
	}
	return envelope, nil
}

func decodeEnvelope(value any) (Envelope, error) {
	encoded, err := clone(value, MaxStateBytes+4096)
	if err != nil {
		return Envelope{}, err
	}
	// The JSON conversion is intentionally isolated from domain persistence.
	return convert[Envelope](encoded)
}

func convert[T any](value any) (T, error) {
	var out T
	raw, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func EnvelopePatch(module contract.RuntimeModule, content contract.Content, envelope Envelope) (map[string]any, error) {
	view, err := Project(module, content, envelope.State, contract.AudiencePlayer)
	if err != nil {
		return nil, err
	}
	patch := map[string]any{ModuleStatePath: envelope}
	for _, field := range content.Runtime.StateSchema.Fields {
		if field.Path == "gameplay" {
			if contract.CanReplaceObject(content.Runtime.StateSchema, field.Path) {
				patch[field.Path] = view
			}
			continue
		}
		if !strings.HasPrefix(field.Path, "gameplay.") || !contract.CanReplaceObject(content.Runtime.StateSchema, field.Path) {
			continue
		}
		value, ok := contract.ValueAtPath(view, strings.TrimPrefix(field.Path, "gameplay."))
		if !ok {
			return nil, fmt.Errorf("模块投影缺少已声明字段 %s", field.Path)
		}
		patch[field.Path] = value
	}
	return convert[map[string]any](patch)
}

func ExecuteEnvelope(module contract.RuntimeModule, content contract.Content, envelope Envelope, expectedRevision int64, actor, key string, arguments map[string]any) (Envelope, contract.Transition, error) {
	if envelope.State.Revision != expectedRevision {
		return Envelope{}, contract.Transition{}, contract.Reject("state.conflict", "状态已变化，请刷新后重新选择")
	}
	seed, err := strconv.ParseInt(envelope.Seed, 10, 64)
	if err != nil {
		return Envelope{}, contract.Transition{}, err
	}
	transition, err := Execute(module, content, envelope.State, contract.RuntimeRequest{ActionKey: key, Actor: actor, Arguments: arguments, Random: NewRandom(seed, fmt.Sprintf("revision/%d", envelope.State.Revision))})
	if err != nil {
		return Envelope{}, contract.Transition{}, err
	}
	envelope.State = transition.State
	return envelope, transition, nil
}
