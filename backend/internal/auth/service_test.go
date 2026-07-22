package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ialtone/fatecraft/internal/storage"
)

type fakeRepository struct {
	flow           *storage.OAuthLoginFlow
	user           *storage.User
	session        *storage.AuthSession
	settings       *storage.AuthSettings
	verification   *storage.EmailVerificationCode
	invitations    []storage.InvitationCode
	verificationID *uuid.UUID
	invitationHash *string
}

func (f *fakeRepository) CreateOAuthFlow(_ context.Context, flow *storage.OAuthLoginFlow) error {
	f.flow = flow
	return nil
}
func (f *fakeRepository) ConsumeOAuthFlow(_ context.Context, provider, hash string, now time.Time) (*storage.OAuthLoginFlow, error) {
	if f.flow == nil || f.flow.Provider != provider || f.flow.StateHash != hash || !f.flow.ExpiresAt.After(now) {
		return nil, storage.ErrNotFound
	}
	flow := f.flow
	f.flow = nil
	return flow, nil
}
func (f *fakeRepository) CreateSession(_ context.Context, session *storage.AuthSession) error {
	f.session = session
	return nil
}
func (f *fakeRepository) DeleteSession(context.Context, string) error { return nil }
func (f *fakeRepository) UserBySession(context.Context, string, time.Time) (*storage.User, error) {
	return f.user, nil
}
func (f *fakeRepository) UserByEmail(context.Context, string) (*storage.User, error) {
	return nil, storage.ErrNotFound
}
func (f *fakeRepository) RegisterEmailUser(_ context.Context, user *storage.User, verificationID *uuid.UUID, invitationHash *string, _ time.Time) error {
	f.user = user
	f.verificationID = verificationID
	f.invitationHash = invitationHash
	return nil
}
func (f *fakeRepository) UpsertExternalUser(_ context.Context, in storage.ExternalIdentityInput) (*storage.User, error) {
	if f.user == nil {
		f.user = &storage.User{ID: uuid.New(), DisplayName: in.DisplayName, Email: in.Email, Status: storage.UserStatusActive}
	}
	return f.user, nil
}
func (f *fakeRepository) GetAuthSettings(context.Context) (*storage.AuthSettings, error) {
	if f.settings == nil {
		f.settings = storage.DefaultAuthSettings()
	}
	copy := *f.settings
	return &copy, nil
}
func (f *fakeRepository) SaveAuthSettings(_ context.Context, settings *storage.AuthSettings) error {
	copy := *settings
	f.settings = &copy
	return nil
}
func (f *fakeRepository) LatestEmailVerification(context.Context, string, string) (*storage.EmailVerificationCode, error) {
	if f.verification == nil {
		return nil, storage.ErrNotFound
	}
	return f.verification, nil
}
func (f *fakeRepository) CreateEmailVerification(_ context.Context, code *storage.EmailVerificationCode) error {
	f.verification = code
	return nil
}
func (f *fakeRepository) DeleteEmailVerification(context.Context, uuid.UUID) error {
	f.verification = nil
	return nil
}
func (f *fakeRepository) IncrementEmailVerificationAttempts(context.Context, uuid.UUID) error {
	if f.verification != nil {
		f.verification.AttemptCount++
	}
	return nil
}
func (f *fakeRepository) ListInvitationCodes(context.Context) ([]storage.InvitationCode, error) {
	return f.invitations, nil
}
func (f *fakeRepository) InvitationCodeByID(_ context.Context, id uuid.UUID) (*storage.InvitationCode, error) {
	for i := range f.invitations {
		if f.invitations[i].ID == id {
			return &f.invitations[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (f *fakeRepository) CreateInvitationCode(_ context.Context, invitation *storage.InvitationCode) error {
	f.invitations = append(f.invitations, *invitation)
	return nil
}
func (f *fakeRepository) UpdateInvitationCode(_ context.Context, invitation *storage.InvitationCode) error {
	for i := range f.invitations {
		if f.invitations[i].ID == invitation.ID {
			f.invitations[i] = *invitation
		}
	}
	return nil
}

func TestStartLinuxDOUsesPKCEAndPersistsFlow(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, Config{
		LinuxDOClientID: "client-id", LinuxDOClientSecret: "secret", LinuxDORedirectURI: "http://localhost/callback",
	})
	redirect, err := svc.StartLinuxDO(context.Background(), "/conversations/abc")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for _, key := range []string{"code_challenge", "code_challenge_method", "state", "client_id", "redirect_uri"} {
		if q.Get(key) == "" {
			t.Fatalf("missing OAuth query parameter %q", key)
		}
	}
	if q.Get("code_challenge_method") != "S256" || repo.flow == nil {
		t.Fatalf("PKCE flow was not persisted correctly")
	}
	if repo.flow.ReturnTo != "/conversations/abc" {
		t.Fatalf("unexpected return_to: %q", repo.flow.ReturnTo)
	}
}

func TestCompleteLinuxDOExchangesCodeAndCreatesSession(t *testing.T) {
	repo := &fakeRepository{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil || r.PostForm.Get("code") != "auth-code" || r.PostForm.Get("code_verifier") == "" {
				http.Error(w, "bad token request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"linuxdo-42","username":"fate","name":"Fate User","email":"fate@example.com","active":true,"trust_level":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	svc := NewService(repo, Config{
		LinuxDOClientID: "client-id", LinuxDOClientSecret: "secret", LinuxDORedirectURI: "http://localhost/callback",
		TokenURL: provider.URL + "/token", UserInfoURL: provider.URL + "/user",
	})
	start, err := svc.StartLinuxDO(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	state := mustParseURL(t, start).Query().Get("state")
	result, err := svc.CompleteLinuxDO(context.Background(), "auth-code", state)
	if err != nil {
		t.Fatal(err)
	}
	if result.User.DisplayName != "Fate User" || result.User.Email == nil || *result.User.Email != "fate@example.com" {
		t.Fatalf("unexpected user: %+v", result.User)
	}
	if result.SessionToken == "" || repo.session == nil || repo.session.UserID != result.User.ID {
		t.Fatalf("session was not created")
	}
	if repo.flow != nil {
		t.Fatal("OAuth flow was not consumed")
	}
}

func TestEmailAuthDisabledByDefault(t *testing.T) {
	svc := NewService(&fakeRepository{}, Config{})
	if _, err := svc.RegisterEmail(context.Background(), "user@example.com", "password123", "User", "", ""); err != ErrEmailRegistrationDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
	config, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.EmailRegistrationEnabled {
		t.Fatal("email registration must be disabled by default")
	}
}

type fakeMailer struct {
	message MailMessage
}

func (m *fakeMailer) Send(_ context.Context, _ SMTPConfig, message MailMessage) error {
	m.message = message
	return nil
}

func TestRegistrationCodeAndInvitationAreRequiredByRuntimeSettings(t *testing.T) {
	repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
	repo.settings.EmailRegistrationEnabled = true
	repo.settings.EmailVerificationEnabled = true
	repo.settings.InvitationRequired = true
	repo.settings.SMTPHost = "smtp.example.com"
	repo.settings.SenderEmail = "noreply@example.com"
	svc := NewService(repo, Config{DataEncryptionKey: strings.Repeat("k", 32)})
	mailer := &fakeMailer{}
	svc.mailer = mailer

	if err := svc.SendRegistrationCode(context.Background(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`\d{6}`).FindString(mailer.message.Body)
	if code == "" {
		t.Fatalf("verification code missing from body: %q", mailer.message.Body)
	}
	result, err := svc.RegisterEmail(
		context.Background(), "user@example.com", "password123", "User", code, "FC-ABCD-EFGH-IJKL-MNOP",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.User == nil || repo.verificationID == nil || repo.invitationHash == nil {
		t.Fatal("registration proofs were not passed to the atomic repository operation")
	}
	if *repo.invitationHash != sha256Hex(normalizeInvitationCode("FC-ABCD-EFGH-IJKL-MNOP")) {
		t.Fatal("invitation code was not hashed before storage lookup")
	}
}

func TestInvalidVerificationCodeIncrementsAttemptCount(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{
		settings: &storage.AuthSettings{EmailRegistrationEnabled: true, EmailVerificationEnabled: true},
		verification: &storage.EmailVerificationCode{
			ID: uuid.New(), Email: "user@example.com", Purpose: registrationPurpose,
			CodeHash: string(hash), ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now(),
		},
	}
	svc := NewService(repo, Config{})
	_, err = svc.RegisterEmail(context.Background(), "user@example.com", "password123", "User", "000000", "")
	if !errors.Is(err, ErrInvalidVerificationCode) {
		t.Fatalf("expected invalid verification error, got %v", err)
	}
	if repo.verification.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", repo.verification.AttemptCount)
	}
}

func TestSMTPPasswordIsEncryptedAndNeverReturned(t *testing.T) {
	repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
	svc := NewService(repo, Config{DataEncryptionKey: strings.Repeat("secret", 6)})
	view, err := svc.UpdateAdminSettings(context.Background(), UpdateAuthSettingsInput{
		EmailAuthEnabled: true, SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPSecurity: "starttls",
		SMTPUsername: "mailer", SMTPPassword: "plain-password", SenderEmail: "noreply@example.com",
		SenderName: "Fatecraft", VerificationSubject: "验证码", VerificationTemplate: "验证码：{{code}}",
		VerificationCodeTTLMinutes: 10, VerificationResendSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasSMTPPassword || repo.settings.SMTPPasswordEncrypted == "" || strings.Contains(repo.settings.SMTPPasswordEncrypted, "plain-password") {
		t.Fatal("SMTP password was not stored as an opaque ciphertext")
	}
	decrypted, err := svc.decryptSMTPPassword(repo.settings)
	if err != nil || decrypted != "plain-password" {
		t.Fatalf("decrypt password: value=%q err=%v", decrypted, err)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Query().Get("state")) == "" {
		t.Fatalf("invalid OAuth URL: %v", err)
	}
	return u
}
