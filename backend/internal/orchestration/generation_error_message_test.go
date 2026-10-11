package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/llm"
)

func TestGenerationErrorMessagesExplainKnownFailuresWithoutProviderSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "超时"},
		{llm.ErrRelayNoKey, "API Key"},
		{io.ErrUnexpectedEOF, "中断"},
		{llm.ErrTruncated, "Token 上限"},
		{&llm.HTTPError{StatusCode: 401, Body: "private-api-key"}, "HTTP 401"},
		{&llm.HTTPError{StatusCode: 429, Body: "private-api-key"}, "HTTP 429"},
		{&llm.HTTPError{StatusCode: 503, Body: "private-api-key"}, "HTTP 503"},
		{errors.New("private-api-key"), "执行流程"},
	} {
		got := generationErrorMessage(fmt.Errorf("wrapped: %w", tc.err))
		if !strings.Contains(got, tc.want) || strings.Contains(got, "private-api-key") {
			t.Fatalf("message = %q; want %q without secret", got, tc.want)
		}
		partial := partialGenerationErrorMessage(tc.err)
		if !strings.Contains(partial, "继续叙事") || strings.Contains(partial, "重试") {
			t.Fatalf("unsafe partial advice: %q", partial)
		}
	}
}
