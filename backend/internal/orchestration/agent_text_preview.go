package orchestration

import "strings"

// agentTextPreview separates player-visible streaming from trusted message
// persistence. Text can be previewed before the provider has finished deciding
// whether the step is a text or tool lane; a later tool call retracts it.
type agentTextPreview struct {
	buffer   strings.Builder
	toolSeen bool
	emitted  bool
}

func newAgentTextPreview() *agentTextPreview { return &agentTextPreview{} }

func (p *agentTextPreview) append(text string, streaming bool, emit func(string)) {
	if p == nil || p.toolSeen || text == "" {
		return
	}
	p.buffer.WriteString(text)
	if streaming && emit != nil {
		p.emitted = true
		emit(text)
	}
}

func (p *agentTextPreview) markTool(reset func()) {
	if p == nil || p.toolSeen {
		return
	}
	p.toolSeen = true
	if p.emitted && reset != nil {
		reset()
	}
}

func (p *agentTextPreview) text() string {
	if p == nil {
		return ""
	}
	return p.buffer.String()
}

func (p *agentTextPreview) sent() bool { return p != nil && p.emitted }
