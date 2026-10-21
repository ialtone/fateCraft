package memory

import (
	"fmt"
	"testing"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
)

func TestOutboundTaskRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		code  string
		retry bool
	}{
		{llmoutbound.CodeProxyUnreachable, true},
		{llmoutbound.CodeTargetUnreachable, true},
		{llmoutbound.CodeTimeout, true},
		{llmoutbound.CodeOutboundFailed, true},
		{llmoutbound.CodeProxyAuthFailed, false},
		{llmoutbound.CodeTLSCertificate, false},
		{llmoutbound.CodeConfigInvalid, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := fmt.Errorf("request failed: %w", &llmoutbound.Error{Code: tc.code})
			if got := isRetryableLLMError(err); got != tc.retry {
				t.Fatalf("retry=%v want=%v", got, tc.retry)
			}
			if got := classifyLLMError(err); got != "outbound_"+tc.code {
				t.Fatalf("classification=%s", got)
			}
		})
	}
}
