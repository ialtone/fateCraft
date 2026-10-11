package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

var ErrToolCallConflict = errors.New("模型工具调用的标识、名称或参数不一致")

// Output position and item IDs link early fragments; call_id is the stable
// execution identity. Aliases are kept on one accumulator, never as duplicate
// entries in the materialized tool batch.
func ensureResponseCall(calls map[string]*accumulatedAgentCall, index int, itemID, callID, name string, onEvent AgentEventHandler) (*accumulatedAgentCall, error) {
	var selected *accumulatedAgentCall
	for _, call := range calls {
		if call.index != index && !(callID != "" && call.id == callID) && !(itemID != "" && call.itemIDs[itemID]) {
			continue
		}
		if selected != nil && selected != call {
			return nil, ErrToolCallConflict
		}
		selected = call
	}
	if selected == nil {
		selected = &accumulatedAgentCall{index: index, itemIDs: map[string]bool{}}
		calls[fmt.Sprintf("index:%d", index)] = selected
	}
	if selected.index != index || callID != "" && selected.id != "" && selected.id != callID || name != "" && selected.name != "" && selected.name != name {
		return nil, ErrToolCallConflict
	}
	if itemID != "" {
		selected.itemIDs[itemID] = true
		if selected.itemID == "" {
			selected.itemID = itemID
		}
	}
	if callID != "" {
		selected.id = callID
	}
	if name != "" {
		selected.name = name
	}
	if !selected.started {
		selected.started = true
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallStarted, CallID: selected.id, Name: selected.name})
	}
	return selected, nil
}

// Terminal arguments are a complete projection, not another delta. Accept a
// consistent final projection, including a missing suffix, but never overwrite
// already completed arguments with a conflicting result.
func reconcileToolArguments(call *accumulatedAgentCall, full string) error {
	if full == "" {
		return nil
	}
	if len(full) > maxAgentToolArguments {
		return errors.New("模型工具参数超过 16 KiB")
	}
	current := call.arguments.String()
	if current != "" && current != full && !equalToolArguments(current, full) {
		if call.completed || json.Valid([]byte(current)) || !strings.HasPrefix(full, current) {
			return ErrToolCallConflict
		}
	}
	call.arguments.Reset()
	call.arguments.WriteString(full)
	call.completed = true
	return nil
}

func equalToolArguments(left, right string) bool {
	decode := func(raw string) (any, bool) {
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, false
		}
		return value, true
	}
	a, okA := decode(left)
	b, okB := decode(right)
	return okA && okB && reflect.DeepEqual(a, b)
}
