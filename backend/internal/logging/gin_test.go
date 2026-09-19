package logging

import "testing"

func TestSafeRequestPathRedactsShareTokens(t *testing.T) {
	cases := map[string]string{
		"/s/v1.secret.signature":                                      "/s/[redacted]",
		"/api/public/conversation-shares/v1.secret.signature":         "/api/public/conversation-shares/[redacted]",
		"/api/public/conversation-shares/v1.secret.signature/reports": "/api/public/conversation-shares/[redacted]/reports",
		"/api/health": "/api/health",
	}
	for input, expected := range cases {
		if actual := safeRequestPath(input); actual != expected {
			t.Fatalf("safeRequestPath(%q) = %q, want %q", input, actual, expected)
		}
	}
}
