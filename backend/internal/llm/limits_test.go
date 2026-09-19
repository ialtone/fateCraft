package llm

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestReadAllBoundedDistinguishesLimitFromOverflow(t *testing.T) {
	const limit = 16
	exact, err := readAllBounded(strings.NewReader(strings.Repeat("x", limit)), limit)
	if err != nil || len(exact) != limit {
		t.Fatalf("exact response: len=%d err=%v", len(exact), err)
	}

	overflow, err := readAllBounded(strings.NewReader(strings.Repeat("x", limit+1)), limit)
	if !errors.Is(err, ErrLLMResponseTooLarge) || len(overflow) != limit {
		t.Fatalf("overflow response: len=%d err=%v", len(overflow), err)
	}
}

func TestReadSSELineRejectsOversizedLine(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", maxSSELineBytes+1)+"\n"), 128)
	line, err := readSSELine(reader)
	if !errors.Is(err, ErrLLMSSELineTooLarge) || len(line) != maxSSELineBytes {
		t.Fatalf("line len=%d err=%v", len(line), err)
	}
}

func TestAccumulatedResponsesTextIsBounded(t *testing.T) {
	var text strings.Builder
	text.WriteString(strings.Repeat("x", maxLLMResponseBytes-1))
	if err := appendResponsesText(&text, "yz", nil); !errors.Is(err, ErrLLMResponseTooLarge) {
		t.Fatalf("err=%v, want ErrLLMResponseTooLarge", err)
	}
	if text.Len() != maxLLMResponseBytes-1 {
		t.Fatalf("overflow mutated accumulated text: len=%d", text.Len())
	}
}

func TestAuditBoundsMarkOnlyActualTruncation(t *testing.T) {
	exact := strings.Repeat("x", maxLLMAuditBytes)
	bounded, truncated := boundedAuditString(exact)
	if truncated || len(bounded) != maxLLMAuditBytes {
		t.Fatalf("exact audit value: len=%d truncated=%v", len(bounded), truncated)
	}

	overflow := exact + "x"
	bounded, truncated = boundedAuditString(overflow)
	if !truncated || len(bounded) != maxLLMAuditBytes {
		t.Fatalf("overflow audit value: len=%d truncated=%v", len(bounded), truncated)
	}
}
