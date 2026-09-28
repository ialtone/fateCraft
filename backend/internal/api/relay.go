package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// relayRegistry 是设置与管理接口需要的 Relay 注册表能力子集（接口便于测试断言）。
type relayRegistry interface {
	CloseAll()
	ClosePlayer(playerID string)
	Serve(w http.ResponseWriter, req *http.Request, playerID uuid.UUID)
}

// serveLLMRelay 升级为用户级 Relay 连接，承载模型请求转发。
// requireAuth 已校验会话；Registry 内部再校验当前有效路由为 client。
func (s *Server) serveLLMRelay(c *gin.Context) {
	if s.relays == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "客户端直连服务未配置"})
		return
	}
	value, ok := c.Get(authUserKey)
	user, valid := value.(*storage.User)
	if !ok || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	s.relays.Serve(c.Writer, c.Request, user.ID)
}
