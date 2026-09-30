package storage

import (
	"context"
	"testing"

	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresClientRouteDefaultsOnAndCanBeDisabled(t *testing.T) {
	db := testdb.Open(t, "client_route_defaults")
	ctx := context.Background()
	repo := NewSystemSettingsRepo(db)
	settings, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.ClientLLMRouteEnabled {
		t.Fatal("initial database must allow client route choice")
	}
	for _, enabled := range []bool{false, true, false} {
		settings.ClientLLMRouteEnabled = enabled
		if err := repo.Save(ctx, settings); err != nil {
			t.Fatal(err)
		}
		stored, err := NewSystemSettingsRepo(db).Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if stored.ClientLLMRouteEnabled != enabled {
			t.Fatalf("persisted client route = %v, want %v", stored.ClientLLMRouteEnabled, enabled)
		}
	}
}
