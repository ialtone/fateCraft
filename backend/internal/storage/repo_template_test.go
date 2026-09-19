package storage

import "testing"

func TestDefaultLLMMaxContextTokens(t *testing.T) {
	if DefaultLLMMaxContextTokens != 128000 {
		t.Fatalf("default max context = %d, want 128000", DefaultLLMMaxContextTokens)
	}
	if DefaultLLMMaxResponseTokens != 16384 {
		t.Fatalf("default max response = %d, want 16384", DefaultLLMMaxResponseTokens)
	}
}

func TestDefaultLLMProtocol(t *testing.T) {
	if DefaultLLMProtocol != "responses" {
		t.Fatalf("default protocol = %q, want responses", DefaultLLMProtocol)
	}
}
