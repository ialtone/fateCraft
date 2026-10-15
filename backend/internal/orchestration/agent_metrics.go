package orchestration

import (
	"strings"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// Estimate only model-produced content. Serialized block keys, provider IDs
// and opaque replay payloads are storage/protocol overhead, not output tokens.
// Hidden reasoning cannot be recovered locally; the caller marks estimates.
func estimateAgentOutputTokens(counter tokenizer.Counter, result llm.AgentStepResult) int {
	var output strings.Builder
	for _, block := range result.CanonicalBlocks() {
		switch block.Kind {
		case llm.ContentText, llm.ContentThinking, llm.ContentRefusal:
			output.WriteString(block.Text)
		case llm.ContentToolCall:
			if block.ToolCall != nil {
				output.WriteString(block.ToolCall.Name)
				output.WriteString(block.ToolCall.Arguments)
			}
		}
	}
	return counter.Count(output.String())
}
