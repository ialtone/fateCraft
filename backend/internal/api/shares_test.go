package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	shareapp "github.com/ialtone/fatecraft/internal/share"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestShareDescriptionIsBounded(t *testing.T) {
	snapshot := shareapp.Snapshot{Messages: []shareapp.MessageSnapshot{{Markdown: strings.Repeat("故事 ", 200)}}}
	description := shareDescription(snapshot)
	if got := len([]rune(description)); got != 163 {
		t.Fatalf("description rune length = %d, want 163", got)
	}
}

func TestConversationShareHTMLEscapesPublicText(t *testing.T) {
	data := conversationShareHTMLData{
		Title:       `<script>alert("title")</script>`,
		Description: `<img src=x onerror=alert(1)>`,
		Token:       `bad"><script>alert(1)</script>`,
	}
	var output bytes.Buffer
	if err := conversationShareHTML.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, unsafe := range []string{`<script>alert`, `<img src=x`} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("unsafe text was not escaped: %s", html)
		}
	}
}

func TestWritePublicConversationShareETag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	view := &shareapp.PublicView{SnapshotHash: "abc123", Snapshot: shareapp.Snapshot{SchemaVersion: 1}}

	for _, test := range []struct {
		name        string
		ifNoneMatch string
		wantStatus  int
	}{
		{name: "body", wantStatus: http.StatusOK},
		{name: "not_modified", ifNoneMatch: `"abc123"`, wantStatus: http.StatusNotModified},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/public/conversation-shares/token", nil)
			ctx.Request.Header.Set("If-None-Match", test.ifNoneMatch)
			writePublicConversationShare(ctx, view)
			if ctx.Writer.Status() != test.wantStatus {
				t.Fatalf("status = %d, want %d", ctx.Writer.Status(), test.wantStatus)
			}
			if recorder.Header().Get("ETag") != `"abc123"` {
				t.Fatalf("ETag = %q", recorder.Header().Get("ETag"))
			}
			if recorder.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
				t.Fatalf("unexpected Cache-Control: %q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestPublicConversationShareErrorsDoNotLeakState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validKey := strings.Repeat("k", 32)
	tests := []struct {
		name       string
		service    *shareapp.Service
		wantStatus int
	}{
		{
			name:       "invalid_token",
			service:    shareapp.New(storage.NewShareRepository(nil), validKey),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "signing_unavailable",
			service:    shareapp.New(nil, "short"),
			wantStatus: http.StatusServiceUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "token", Value: "invalid-token"}}
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/public/conversation-shares/invalid-token", nil)
			server := &Server{shares: test.service}
			server.getPublicConversationShare(ctx)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus == http.StatusNotFound && !strings.Contains(recorder.Body.String(), "分享不存在或已失效") {
				t.Fatalf("public 404 leaked a distinct state: %s", recorder.Body.String())
			}
		})
	}
}
