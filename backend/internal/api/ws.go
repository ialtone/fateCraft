package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/realtime"
)

// serveWS 升级为 WebSocket，绑定单个 conversation_id。
func (s *Server) serveWS(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	// 校验对话存在。
	if _, err := s.conv.Get(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	_ = s.hub.Serve(c.Writer, c.Request, id)
}

// endedEvent 构造 conversation_ended 事件。
func endedEvent(id uuid.UUID) realtime.Event {
	return realtime.Event{Type: realtime.TypeConversationEnded, ConversationID: id.String()}
}
