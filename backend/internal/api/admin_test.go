package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/challengeimport"
)

type builtinImporterStub struct {
	items        []challengeimport.BuiltinInfo
	importResult *challengeimport.ImportResult
	importedSlug string
	listErr      error
	importErr    error
}

func (s *builtinImporterStub) ListBuiltins(context.Context) ([]challengeimport.BuiltinInfo, error) {
	return s.items, s.listErr
}

func (s *builtinImporterStub) ImportBuiltin(_ context.Context, slug string) (*challengeimport.ImportResult, error) {
	s.importedSlug = slug
	return s.importResult, s.importErr
}

func TestImportAdminBuiltinRejectsVersionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &builtinImporterStub{importErr: challengeimport.ErrVersionConflict}
	server := &Server{builtins: stub}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/builtin-challenges/trial/import", nil)
	ctx.Params = gin.Params{{Key: "slug", Value: "trial"}}

	server.importAdminBuiltin(ctx)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

func TestAllowedRollDifficulties(t *testing.T) {
	for _, mode := range []string{"easy", "normal", "hard"} {
		if !allowedRollDifficulties[mode] {
			t.Fatalf("expected %q to be allowed", mode)
		}
	}
	for _, mode := range []string{"", "custom", "impossible"} {
		if allowedRollDifficulties[mode] {
			t.Fatalf("expected %q to be rejected", mode)
		}
	}
}

func TestListAdminBuiltins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &builtinImporterStub{items: []challengeimport.BuiltinInfo{{Slug: "trial", Version: "1.0.0"}}}
	server := &Server{builtins: stub}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/builtin-challenges", nil)

	server.listAdminBuiltins(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Builtins []challengeimport.BuiltinInfo `json:"builtins"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Builtins) != 1 || body.Builtins[0].Slug != "trial" {
		t.Fatalf("body = %#v", body)
	}
}

func TestImportAdminBuiltinUsesSlugOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &builtinImporterStub{importResult: &challengeimport.ImportResult{Slug: "trial", Version: "1.0.0", Created: true}}
	server := &Server{builtins: stub}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/builtin-challenges/trial/import", nil)
	ctx.Params = gin.Params{{Key: "slug", Value: "trial"}}

	server.importAdminBuiltin(ctx)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	if stub.importedSlug != "trial" {
		t.Fatalf("imported slug = %q", stub.importedSlug)
	}
}
