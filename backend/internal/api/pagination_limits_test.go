package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
)

func TestListHandlersRejectOversizedLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.NewString()
	tests := []struct {
		name    string
		target  string
		params  gin.Params
		server  *Server
		handler func(*Server, *gin.Context)
	}{
		{name: "conversations", target: "/api/conversations?limit=101", server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listConversations(c) }},
		{name: "messages", target: "/api/conversations/" + id + "/messages?limit=501", params: gin.Params{{Key: "id", Value: id}}, server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listMessages(c) }},
		{name: "state revisions", target: "/api/conversations/" + id + "/state/revisions?limit=201", params: gin.Params{{Key: "id", Value: id}}, server: &Server{}, handler: func(s *Server, c *gin.Context) { s.stateRevisions(c) }},
		{name: "campaign events", target: "/api/campaigns/" + id + "/events?limit=101", params: gin.Params{{Key: "id", Value: id}}, server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listCampaignEvents(c) }},
		{name: "reward ledger", target: "/api/me/reward-ledger?limit=201", server: &Server{identity: identity.NewContext()}, handler: func(s *Server, c *gin.Context) { s.listRewardLedger(c) }},
		{name: "llm logs", target: "/api/admin/llm-request-logs?limit=201", server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listAdminLLMRequestLogs(c) }},
		{name: "context logs", target: "/api/admin/context-build-logs?limit=201", server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listAdminContextBuildLogs(c) }},
		{name: "owned shares", target: "/api/conversation-shares?page_size=101", server: &Server{}, handler: func(s *Server, c *gin.Context) { s.listOwnedConversationShares(c) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.name == "reward ledger" {
				request = request.WithContext(identity.WithPlayerID(request.Context(), id))
			}
			ctx.Request = request
			ctx.Params = tt.params
			tt.handler(tt.server, ctx)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
