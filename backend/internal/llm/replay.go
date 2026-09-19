package llm

import (
	"errors"
	"fmt"
	"strings"
)

// ReplayRepairKind identifies a deterministic repair made before a model
// request is serialized. Repairs never execute tools or change domain state.
type ReplayRepairKind string

const (
	ReplayRepairMissingOutput ReplayRepairKind = "missing_output"
	ReplayRepairOrphanOutput  ReplayRepairKind = "orphan_output"
	ReplayRepairEmptyOutput   ReplayRepairKind = "empty_output"
)

// ReplayRepair describes one repaired tool-call history entry. CallID is kept
// for diagnostics and tests; callers that log it should hash it first.
type ReplayRepair struct {
	Kind   ReplayRepairKind
	CallID string
}

// ToolReplayResult is the request-local, protocol-neutral replay projection.
type ToolReplayResult struct {
	Messages []Message
	Repairs  []ReplayRepair
}

var (
	// ErrToolReplayInvalidHistory means the history cannot be made provider-safe
	// without guessing which call a result belongs to.
	ErrToolReplayInvalidHistory = errors.New("工具回放历史无效")
	ErrToolReplayInvalidCallID  = errors.New("工具回放包含无效 call_id")
	ErrToolReplayDuplicateCall  = errors.New("工具回放包含重复 call_id")
)

type replayCall struct {
	call      ToolCall
	completed bool
}

// PrepareToolReplay validates and repairs the tool-call/result pairing in a
// model history. It is deliberately pure: no database access, tool execution,
// retries, or provider-specific projection occurs here.
//
// A tool call without a result receives an explicit error result. This mirrors
// the provider contract while making it impossible for a failed/interrupted
// local step to be interpreted as a successful tool execution.
func PrepareToolReplay(messages []Message) (ToolReplayResult, error) {
	result := ToolReplayResult{Messages: make([]Message, 0, len(messages))}
	pending := make(map[string]replayCall)
	var pendingOrder []string

	addRepair := func(kind ReplayRepairKind, callID string) {
		result.Repairs = append(result.Repairs, ReplayRepair{Kind: kind, CallID: callID})
	}
	flushPending := func() {
		for _, callID := range pendingOrder {
			call, ok := pending[callID]
			if !ok || call.completed {
				continue
			}
			result.Messages = append(result.Messages, syntheticToolResult(call.call))
			addRepair(ReplayRepairMissingOutput, callID)
		}
		pending = make(map[string]replayCall)
		pendingOrder = nil
	}

	for _, message := range messages {
		switch message.Role {
		case "assistant":
			calls := message.ToolCallBlocks()
			// A multi-tool transaction persists one assistant tool-call row per
			// invocation, but only the first row carries the complete block list.
			// Empty assistant rows are storage placeholders, not a new model turn.
			if len(calls) == 0 && len(pending) > 0 && strings.TrimSpace(message.TextProjection()) == "" {
				continue
			}
			// A new assistant item cannot legally appear before the previous
			// assistant's tool results. Close the previous chain first.
			flushPending()
			for _, call := range calls {
				if strings.TrimSpace(call.ID) == "" || call.ID != strings.TrimSpace(call.ID) || len(call.ID) > 256 {
					return ToolReplayResult{}, fmt.Errorf("%w: %w: call_id 必须为非空且不超过 256 字符", ErrToolReplayInvalidHistory, ErrToolReplayInvalidCallID)
				}
				if previous, exists := pending[call.ID]; exists {
					if previous.call.Name != call.Name || previous.call.Arguments != call.Arguments {
						return ToolReplayResult{}, fmt.Errorf("%w: %w: %s 的工具或参数发生变化", ErrToolReplayInvalidHistory, ErrToolReplayDuplicateCall, call.ID)
					}
					return ToolReplayResult{}, fmt.Errorf("%w: %w: %s", ErrToolReplayInvalidHistory, ErrToolReplayDuplicateCall, call.ID)
				}
				pending[call.ID] = replayCall{call: call}
				pendingOrder = append(pendingOrder, call.ID)
			}
			result.Messages = append(result.Messages, message)

		case "tool":
			callID := strings.TrimSpace(message.ToolCallID)
			if callID == "" {
				addRepair(ReplayRepairOrphanOutput, "")
				continue
			}
			call, exists := pending[callID]
			if !exists || call.completed {
				// A result after a user/new assistant boundary is an orphan.
				addRepair(ReplayRepairOrphanOutput, callID)
				continue
			}
			if strings.TrimSpace(message.TextProjection()) == "" {
				message = syntheticEmptyToolResult(message)
				addRepair(ReplayRepairEmptyOutput, callID)
			}
			result.Messages = append(result.Messages, message)
			call.completed = true
			pending[callID] = call
			delete(pending, callID)

		default:
			// Any non-tool message interrupts a tool-result sequence. User,
			// system and other roles must never be placed between a call and
			// its provider-visible output.
			flushPending()
			result.Messages = append(result.Messages, message)
		}
	}
	flushPending()
	return result, nil
}

func syntheticToolResult(call ToolCall) Message {
	const text = "[tool_replay_error] 工具调用未执行或未提供结果；不要假设该工具已经成功。"
	return Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    text,
		Blocks:     []ContentBlock{{Kind: ContentText, Text: text}},
	}
}

func syntheticEmptyToolResult(message Message) Message {
	const text = "[tool_replay_error] 工具返回为空；不要假设该工具已经成功。"
	message.Content = text
	message.Blocks = []ContentBlock{{Kind: ContentText, Text: text}}
	return message
}
