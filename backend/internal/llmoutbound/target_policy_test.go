package llmoutbound

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserSuppliedTargetsCannotDialPrivateAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "::1", "fd00:ec2::254", "100.100.100.200"} {
		if !forbiddenTarget(net.ParseIP(raw)) {
			t.Fatalf("%s must be forbidden", raw)
		}
	}
	if forbiddenTarget(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address must be allowed")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer upstream.Close()
	snapshot, err := Build(Config{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	client := snapshot.Client()
	do := func(ctx context.Context) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if err := do(WithUserSuppliedTarget(context.Background())); !errors.Is(err, ErrPrivateTarget) {
		t.Fatalf("user target err = %v, want ErrPrivateTarget", err)
	}
	if err := do(context.Background()); err != nil {
		t.Fatalf("operator target must stay reachable: %v", err)
	}
	t.Setenv("LLM_ALLOW_PRIVATE_TARGETS", "true")
	if err := do(WithUserSuppliedTarget(context.Background())); err != nil {
		t.Fatalf("env flag must disable the guard: %v", err)
	}
}
