package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/realtime"
)

func TestFailMapsConversationBusinessRuleErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for err, want := range map[error]int{
		conversation.ErrTrustedEndRequired: http.StatusConflict,
		conversation.ErrTitleTooLong:       http.StatusBadRequest,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		fail(c, err)
		if w.Code != want {
			t.Fatalf("fail(%v) status = %d, want %d", err, w.Code, want)
		}
	}
}

func TestPostMessageRejectsOversizedContentBeforeTouchingState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{} // nil dependencies: any downstream call would panic
	body := `{"content":"` + strings.Repeat("字", realtime.MaxUserContentRunes+1) + `"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "00000000-0000-0000-0000-000000000001"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	s.postMessage(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "content_too_long") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}
