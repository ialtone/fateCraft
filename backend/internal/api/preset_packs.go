package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// listPresetPacks 列出所有预设包
func (s *Server) listPresetPacks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"packs": s.presetPacks})
}

// getPresetPack 获取单个预设包详情
func (s *Server) getPresetPack(c *gin.Context) {
	id := c.Param("id")
	for _, pack := range s.presetPacks {
		if pack.ID == id {
			c.JSON(http.StatusOK, pack)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "预设包不存在"})
}
