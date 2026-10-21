package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
	"github.com/ialtone/fatecraft/internal/llmproxy"
	"github.com/ialtone/fatecraft/internal/storage"
)

type adminLLMProxyBody struct {
	ExpectedRevision *int64  `json:"expected_revision"`
	Enabled          *bool   `json:"enabled"`
	URL              *string `json:"url"`
	Username         *string `json:"username"`
	Password         *string `json:"password"`
	ClearPassword    bool    `json:"clear_password"`
	CAPEM            *string `json:"ca_pem"`
}

type adminLLMProxyTestResponse struct {
	OK           bool   `json:"ok"`
	Code         string `json:"code,omitempty"`
	Error        string `json:"error,omitempty"`
	TargetStatus int    `json:"target_status,omitempty"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	CheckedAt    string `json:"checked_at"`
	Revision     int64  `json:"revision"`
	Stale        bool   `json:"stale"`
}

func (s *Server) getAdminLLMProxy(c *gin.Context) {
	if s.llmProxy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 出站代理服务未配置"})
		return
	}
	view, err := s.llmProxy.View(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) updateAdminLLMProxy(c *gin.Context) {
	if s.llmProxy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 出站代理服务未配置"})
		return
	}
	var body adminLLMProxyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效", "code": "llm_proxy_invalid"})
		return
	}
	if body.ExpectedRevision == nil || *body.ExpectedRevision < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供读取时的配置修订号", "code": "llm_proxy_invalid"})
		return
	}
	view, err := s.llmProxy.Update(c.Request.Context(), llmproxy.UpdateInput{
		ExpectedRevision: *body.ExpectedRevision,
		Enabled:          body.Enabled,
		URL:              body.URL,
		Username:         body.Username,
		Password:         body.Password,
		ClearPassword:    body.ClearPassword,
		CAPEM:            body.CAPEM,
	})
	if err != nil {
		proxyFail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) testAdminLLMProxy(c *gin.Context) {
	if s.llmProxy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 出站代理服务未配置"})
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效", "code": "llm_proxy_invalid"})
		return
	}
	result, err := s.llmProxy.Test(c.Request.Context(), body.URL)
	if err != nil {
		proxyFail(c, err)
		return
	}
	response := adminLLMProxyTestResponse{
		OK: result.OK, Code: result.Code, TargetStatus: result.TargetStatus,
		ElapsedMS: result.Elapsed.Milliseconds(), CheckedAt: result.CheckedAt.Format("2006-01-02T15:04:05Z07:00"),
		Revision: result.Revision, Stale: result.Stale,
	}
	if !result.OK {
		response.Error = result.Message
	}
	c.JSON(http.StatusOK, response)
}

func proxyFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrSettingsConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "settings_conflict"})
	case errors.Is(err, llmproxy.ErrPasswordConflict):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "llm_proxy_password_conflict"})
	case errors.Is(err, llmproxy.ErrEncryptionUnavailable), errors.Is(err, llmproxy.ErrDecryption):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "llm_proxy_encryption_unavailable"})
	case errors.Is(err, llmproxy.ErrMissingTestURL):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "llm_proxy_test_url_required"})
	case errors.Is(err, llmoutbound.ErrInvalidConfig):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "llm_proxy_invalid"})
	case errors.Is(err, llmoutbound.ErrProxyDisabled):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "llm_proxy_disabled"})
	default:
		fail(c, err)
	}
}
