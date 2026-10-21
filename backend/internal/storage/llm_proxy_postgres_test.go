package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresSaveLLMProxyIsIsolatedRevisionedAndAudited(t *testing.T) {
	db := testdb.Open(t, "llm_proxy")
	ctx := context.Background()
	repo := NewSystemSettingsRepo(db)

	initial, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.LLMProxyEnabled || initial.LLMProxyRevision != 0 {
		t.Fatalf("unexpected proxy defaults: enabled=%v revision=%d", initial.LLMProxyEnabled, initial.LLMProxyRevision)
	}

	initial.RollDifficultyMode = "hard"
	if err := repo.Save(ctx, initial); err != nil {
		t.Fatalf("save non-proxy settings: %v", err)
	}

	actorID := uuid.New()
	if err := db.Exec(
		`INSERT INTO users (id, username, display_name) VALUES (?, ?, ?)`,
		actorID, "llm-proxy-"+actorID.String(), "LLM Proxy Test",
	).Error; err != nil {
		t.Fatalf("seed audit actor: %v", err)
	}
	actorCtx := identity.WithPlayerID(ctx, actorID.String())

	const ciphertext = "secret-ciphertext-value"
	const caPEM = "-----BEGIN CERTIFICATE-----\nsecret-ca-body\n-----END CERTIFICATE-----"
	candidate := *initial
	candidate.LLMProxyEnabled = true
	candidate.LLMProxyURL = "http://127.0.0.1:3128"
	candidate.LLMProxyUsername = "alice"
	candidate.LLMProxyPasswordEncrypted = ciphertext
	candidate.LLMProxyCAPEM = caPEM

	saved, err := repo.SaveLLMProxy(actorCtx, &candidate, 0)
	if err != nil {
		t.Fatalf("save proxy settings: %v", err)
	}
	if saved.LLMProxyRevision != 1 || !saved.LLMProxyEnabled {
		t.Fatalf("unexpected saved proxy settings: %+v", saved)
	}
	if saved.RollDifficultyMode != "hard" {
		t.Fatalf("proxy save overwrote non-proxy settings: %q", saved.RollDifficultyMode)
	}

	fresh, err := NewSystemSettingsRepo(db).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.LLMProxyRevision != 1 || fresh.LLMProxyURL != candidate.LLMProxyURL ||
		fresh.LLMProxyUsername != candidate.LLMProxyUsername ||
		fresh.LLMProxyPasswordEncrypted != ciphertext || fresh.LLMProxyCAPEM != caPEM {
		t.Fatalf("proxy settings were not persisted: %+v", fresh)
	}

	// A generic settings save must not clear the proxy group, even when its
	// in-memory struct predates the proxy update and has zero proxy fields.
	generic := *initial
	generic.RollDifficultyMode = "normal"
	if err := repo.Save(ctx, &generic); err != nil {
		t.Fatalf("generic save after proxy update: %v", err)
	}
	cached, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cached.LLMProxyRevision != 1 || cached.LLMProxyURL != candidate.LLMProxyURL || cached.LLMProxyPasswordEncrypted != ciphertext {
		t.Fatalf("generic save left stale proxy settings in repository cache: %+v", cached)
	}
	fresh, err = NewSystemSettingsRepo(db).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.RollDifficultyMode != "normal" || fresh.LLMProxyRevision != 1 ||
		fresh.LLMProxyURL != candidate.LLMProxyURL || fresh.LLMProxyPasswordEncrypted != ciphertext {
		t.Fatalf("generic save overwrote proxy settings: %+v", fresh)
	}

	stale := *fresh
	stale.LLMProxyURL = "http://127.0.0.1:3129"
	if _, err := repo.SaveLLMProxy(actorCtx, &stale, 0); !errors.Is(err, ErrSettingsConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	fresh, err = NewSystemSettingsRepo(db).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.LLMProxyRevision != 1 || fresh.LLMProxyURL != candidate.LLMProxyURL {
		t.Fatalf("conflicting save changed proxy settings: %+v", fresh)
	}

	var audit AdminAuditLog
	if err := db.Where("action = ?", "llm_proxy_change").First(&audit).Error; err != nil {
		t.Fatalf("load proxy audit: %v", err)
	}
	raw, err := json.Marshal(audit.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata := string(raw)
	if strings.Contains(metadata, ciphertext) || strings.Contains(metadata, "secret-ca-body") {
		t.Fatalf("audit leaked proxy secret material: %s", metadata)
	}
	if !strings.Contains(metadata, "llm_proxy_revision") ||
		!strings.Contains(metadata, "llm_proxy_ca_fingerprint") {
		t.Fatalf("audit is missing revision or redacted CA metadata: %s", metadata)
	}
}
