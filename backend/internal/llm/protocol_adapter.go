package llm

import "context"

// protocolAdapter is the only boundary at which a wire protocol is selected.
// Business services provide protocol-neutral Messages and receive normalized
// text/structured/agent results; they never inspect provider JSON.
type protocolAdapter interface {
	Stream(context.Context, Params, []Message, func(string)) (string, error)
	Complete(context.Context, Params, []Message) (string, error)
	CompleteStructured(context.Context, Params, []Message) (StructuredResult, error)
	StreamStructured(context.Context, Params, []Message, func(string)) (StructuredResult, error)
	AgentStep(context.Context, Params, []Message, []ToolDefinition, bool, AgentEventHandler) (AgentStepResult, error)
}

type chatCompletionsAdapter struct{ client *Client }

func (a chatCompletionsAdapter) Stream(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	return a.client.streamChat(ctx, p, messages, onDelta)
}
func (a chatCompletionsAdapter) Complete(ctx context.Context, p Params, messages []Message) (string, error) {
	return a.client.completeChat(ctx, p, messages)
}
func (a chatCompletionsAdapter) CompleteStructured(ctx context.Context, p Params, messages []Message) (StructuredResult, error) {
	return a.client.callStructuredChat(ctx, p, messages, false, nil)
}
func (a chatCompletionsAdapter) StreamStructured(ctx context.Context, p Params, messages []Message, onDelta func(string)) (StructuredResult, error) {
	return a.client.callStructuredChat(ctx, p, messages, true, onDelta)
}
func (a chatCompletionsAdapter) AgentStep(ctx context.Context, p Params, messages []Message, tools []ToolDefinition, stream bool, onEvent AgentEventHandler) (AgentStepResult, error) {
	return a.client.agentChat(ctx, p, messages, tools, stream, onEvent)
}

type responsesAdapter struct{ client *Client }

func (a responsesAdapter) Stream(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	return a.client.streamResponses(ctx, p, messages, onDelta)
}
func (a responsesAdapter) Complete(ctx context.Context, p Params, messages []Message) (string, error) {
	return a.client.completeResponses(ctx, p, messages)
}
func (a responsesAdapter) CompleteStructured(ctx context.Context, p Params, messages []Message) (StructuredResult, error) {
	return a.client.callStructuredResponses(ctx, p, messages, false, nil)
}
func (a responsesAdapter) StreamStructured(ctx context.Context, p Params, messages []Message, onDelta func(string)) (StructuredResult, error) {
	return a.client.callStructuredResponses(ctx, p, messages, true, onDelta)
}
func (a responsesAdapter) AgentStep(ctx context.Context, p Params, messages []Message, tools []ToolDefinition, stream bool, onEvent AgentEventHandler) (AgentStepResult, error) {
	return a.client.agentResponses(ctx, p, messages, tools, stream, onEvent)
}

func (c *Client) adapter(p Params) protocolAdapter {
	if ResolveProtocolForTarget(p.Provider, p.BaseURL, string(p.Protocol)) == ProtocolChatCompletions {
		return chatCompletionsAdapter{client: c}
	}
	return responsesAdapter{client: c}
}
