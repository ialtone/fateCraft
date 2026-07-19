package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

// getAccount 获取账号信息。
func (s *Server) getAccount(c *gin.Context) {
	acc, err := s.account.GetOrCreate(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, acc)
}

// useRewind 使用天道回溯。
func (s *Server) useRewind(c *gin.Context) {
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	// 原子扣减：次数不足时返回 ErrNoRewinds，无需先查再扣。
	if err := s.account.UseRewind(playerID); err != nil {
		if errors.Is(err, storage.ErrNoRewinds) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用的天道回溯次数"})
			return
		}
		fail(c, err)
		return
	}

	// 返回更新后的账号信息（Get 对不存在账号会映射为 404）
	acc, err := s.account.Get(playerID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"account": acc,
	})
}
