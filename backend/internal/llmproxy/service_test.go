package llmproxy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
	"github.com/ialtone/fatecraft/internal/storage"
)

type fakeSystemRepo struct {
	mu       sync.Mutex
	settings storage.SystemSettings
	saveErr  error
}

func (f *fakeSystemRepo) Get(context.Context) (*storage.SystemSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := f.settings
	return &clone, nil
}

func (f *fakeSystemRepo) SaveLLMProxy(_ context.Context, next *storage.SystemSettings, expected int64) (*storage.SystemSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settings.LLMProxyRevision != expected {
		return nil, storage.ErrSettingsConflict
	}
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	f.settings.LLMProxyEnabled = next.LLMProxyEnabled
	f.settings.LLMProxyURL = next.LLMProxyURL
	f.settings.LLMProxyUsername = next.LLMProxyUsername
	f.settings.LLMProxyPasswordEncrypted = next.LLMProxyPasswordEncrypted
	f.settings.LLMProxyCAPEM = next.LLMProxyCAPEM
	f.settings.LLMProxyRevision++
	clone := f.settings
	return &clone, nil
}

func newTestService(t *testing.T) (*Service, *fakeSystemRepo, *llmoutbound.Manager) {
	t.Helper()
	repo := &fakeSystemRepo{settings: *storage.DefaultSystemSettings()}
	manager := llmoutbound.NewManager()
	return New(repo, manager, strings.Repeat("k", 32)), repo, manager
}

func boolPtr(v bool) *bool       { return &v }
func stringPtr(v string) *string { return &v }

func TestUpdateStoresEncryptedPasswordAndPublishes(t *testing.T) {
	service, repo, manager := newTestService(t)
	view, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 0,
		Enabled:          boolPtr(true),
		URL:              stringPtr("http://127.0.0.1:3128"),
		Username:         stringPtr("alice"),
		Password:         stringPtr("secret-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || !view.HasPassword || view.Revision != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
	if repo.settings.LLMProxyPasswordEncrypted == "" || repo.settings.LLMProxyPasswordEncrypted == "secret-value" {
		t.Fatal("password was not encrypted")
	}
	if !manager.Snapshot().Enabled() || manager.Snapshot().Revision() != 1 {
		t.Fatalf("snapshot was not published: enabled=%v revision=%d", manager.Snapshot().Enabled(), manager.Snapshot().Revision())
	}
}

func TestUpdateKeepsPasswordWhenOmittedAndClearsTogether(t *testing.T) {
	service, repo, _ := newTestService(t)
	if _, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 0, Enabled: boolPtr(true), URL: stringPtr("http://127.0.0.1:3128"),
		Username: stringPtr("alice"), Password: stringPtr("secret"),
	}); err != nil {
		t.Fatal(err)
	}
	encrypted := repo.settings.LLMProxyPasswordEncrypted
	if _, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 1, URL: stringPtr("http://127.0.0.1:3129"),
	}); err != nil {
		t.Fatal(err)
	}
	if repo.settings.LLMProxyPasswordEncrypted != encrypted {
		t.Fatal("omitted password should be preserved")
	}
	if _, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 2, Username: stringPtr(""), ClearPassword: true,
	}); err != nil {
		t.Fatal(err)
	}
	if repo.settings.LLMProxyPasswordEncrypted != "" {
		t.Fatal("clear_password should remove the saved password")
	}
}

func TestUpdateRejectsClearAndReplaceTogether(t *testing.T) {
	service, _, _ := newTestService(t)
	_, err := service.Update(context.Background(), UpdateInput{ClearPassword: true, Password: stringPtr("new")})
	if !errors.Is(err, ErrPasswordConflict) {
		t.Fatalf("expected password conflict, got %v", err)
	}
}

func TestUpdateConflictDoesNotPublish(t *testing.T) {
	service, _, manager := newTestService(t)
	_, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 9, Enabled: boolPtr(true), URL: stringPtr("http://127.0.0.1:3128"),
	})
	if !errors.Is(err, storage.ErrSettingsConflict) {
		t.Fatalf("expected settings conflict, got %v", err)
	}
	if manager.Snapshot().Revision() != 0 || manager.Snapshot().Enabled() {
		t.Fatal("failed save must not publish a candidate snapshot")
	}
}

func TestTestRejectsDisabledProxyBeforeDerivingTarget(t *testing.T) {
	service, _, _ := newTestService(t)
	if _, err := service.Test(context.Background(), ""); !errors.Is(err, llmoutbound.ErrProxyDisabled) {
		t.Fatalf("expected disabled proxy error, got %v", err)
	}
}

func TestUpdateRejectsUnsafeURLWithoutSaving(t *testing.T) {
	service, repo, _ := newTestService(t)
	_, err := service.Update(context.Background(), UpdateInput{
		ExpectedRevision: 0, Enabled: boolPtr(true), URL: stringPtr("http://user:pass@127.0.0.1:3128"),
	})
	if !errors.Is(err, llmoutbound.ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
	if repo.settings.LLMProxyRevision != 0 {
		t.Fatal("invalid config must not be saved")
	}
}

func TestReloadEnabledCorruptPasswordFails(t *testing.T) {
	service, repo, _ := newTestService(t)
	repo.settings.LLMProxyEnabled = true
	repo.settings.LLMProxyURL = "http://127.0.0.1:3128"
	repo.settings.LLMProxyUsername = "alice"
	repo.settings.LLMProxyPasswordEncrypted = "not-valid-ciphertext"
	if err := service.Reload(context.Background()); !errors.Is(err, ErrDecryption) {
		t.Fatalf("expected explicit decryption error, got %v", err)
	}
}

func TestSaveFailureKeepsRuntimeAndPersistedSettings(t *testing.T) {
	service, repo, manager := newTestService(t)
	before := manager.Snapshot()
	repo.saveErr = errors.New("transaction failed")
	_, err := service.Update(t.Context(), UpdateInput{ExpectedRevision: 0, Enabled: boolPtr(true), URL: stringPtr("http://127.0.0.1:3128")})
	if !errors.Is(err, repo.saveErr) || manager.Snapshot() != before || repo.settings.LLMProxyRevision != 0 {
		t.Fatalf("failed save changed configuration: %v", err)
	}
}

func TestDisableDoesNotRequireDecryptingOldPassword(t *testing.T) {
	service, repo, manager := newTestService(t)
	repo.settings.LLMProxyEnabled = true
	repo.settings.LLMProxyURL = "http://127.0.0.1:3128"
	repo.settings.LLMProxyPasswordEncrypted = "unreadable-ciphertext"
	view, err := service.Update(t.Context(), UpdateInput{ExpectedRevision: 0, Enabled: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if view.Enabled || !view.HasPassword || manager.Snapshot().Enabled() || repo.settings.LLMProxyPasswordEncrypted != "unreadable-ciphertext" {
		t.Fatal("disable must retain ciphertext and publish direct mode")
	}
	if err := service.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(t.Context(), UpdateInput{ExpectedRevision: 1, Enabled: boolPtr(true)})
	if !errors.Is(err, ErrDecryption) || manager.Snapshot().Enabled() {
		t.Fatalf("re-enable must fail closed: %v", err)
	}
}

func TestConcurrentSavesPublishOnlyCommittedRevision(t *testing.T) {
	service, repo, manager := newTestService(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, port := range []string{"3128", "3129"} {
		wg.Add(1)
		go func(port string) {
			defer wg.Done()
			_, err := service.Update(t.Context(), UpdateInput{ExpectedRevision: 0, Enabled: boolPtr(true), URL: stringPtr("http://127.0.0.1:" + port)})
			errs <- err
		}(port)
	}
	wg.Wait()
	close(errs)
	succeeded, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, storage.ErrSettingsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicts != 1 || manager.Snapshot().Revision() != 1 || repo.settings.LLMProxyRevision != 1 {
		t.Fatal("concurrent saves lost revision isolation")
	}
}
