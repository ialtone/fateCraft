package config

import "testing"

func TestGetIntRangeUsesConfiguredValueAndRejectsOutOfRange(t *testing.T) {
	t.Setenv("FATECRAFT_TEST_CACHE_LIMIT", "32")
	if got := getintRange("FATECRAFT_TEST_CACHE_LIMIT", 16, 1, 64); got != 32 {
		t.Fatalf("getintRange() = %d, want 32", got)
	}
	t.Setenv("FATECRAFT_TEST_CACHE_LIMIT", "65")
	if got := getintRange("FATECRAFT_TEST_CACHE_LIMIT", 16, 1, 64); got != 16 {
		t.Fatalf("out-of-range getintRange() = %d, want fallback 16", got)
	}
}

func TestLoadReadsShareSigningKey(t *testing.T) {
	t.Setenv("SHARE_LINK_SIGNING_KEY", "independent-share-signing-key-1234")
	if got := Load().ShareLinkSigningKey; got != "independent-share-signing-key-1234" {
		t.Fatalf("ShareLinkSigningKey = %q", got)
	}
}
