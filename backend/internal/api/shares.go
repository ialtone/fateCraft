package api

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	shareapp "github.com/ialtone/fatecraft/internal/share"
	"github.com/ialtone/fatecraft/internal/storage"
)

var conversationShareHTML = template.Must(template.New("share-preview").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · 命运茶馆</title><meta name="robots" content="noindex,nofollow"><meta name="referrer" content="no-referrer">
<meta property="og:type" content="article"><meta property="og:site_name" content="命运茶馆 Fatecraft">
<meta property="og:title" content="{{.Title}}"><meta property="og:description" content="{{.Description}}"><meta property="og:image" content="/favicon.svg">
<meta name="twitter:card" content="summary"><link rel="canonical" href="/s/{{.Token}}">
<meta http-equiv="refresh" content="0;url=/shared/{{.Token}}">
<style>body{max-width:44rem;margin:12vh auto;padding:0 1.5rem;background:#f6f3ed;color:#292520;font:16px/1.7 system-ui}h1{font:700 2rem/1.25 serif}a{color:#8b4f2b}</style>
</head><body><main><h1>{{.Title}}</h1><p>{{.Description}}</p><p><a href="/shared/{{.Token}}">阅读这份故事分享</a></p></main></body></html>`))

type conversationShareHTMLData struct {
	Title       string
	Description string
	Token       string
}

func (s *Server) createConversationShare(c *gin.Context) {
	conversationID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var input shareapp.CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, ok := currentShareUser(c)
	if !ok {
		return
	}
	view, err := s.shares.Create(c.Request.Context(), user.ID.String(), user.DisplayName, shareAvatarURL(user), conversationID, input)
	if err != nil {
		fail(c, err)
		return
	}
	status := http.StatusCreated
	if view.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, view)
}

func (s *Server) listOwnedConversationShares(c *gin.Context) {
	pageSize, ok := positiveShareQuery(c, "page_size", 20, 100)
	if !ok {
		return
	}
	rows, err := s.shares.ListOwned(
		c.Request.Context(),
		s.identity.CurrentPlayerID(c.Request.Context()),
		strings.TrimSpace(c.Query("status")),
		strings.TrimSpace(c.Query("search")),
		strings.TrimSpace(c.Query("cursor")),
		pageSize,
	)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

func positiveShareQuery(c *gin.Context, name string, fallback, maximum int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maximum {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " 参数无效"})
		return 0, false
	}
	return parsed, true
}

func (s *Server) revokeConversationShare(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if err := s.shares.Revoke(c.Request.Context(), s.identity.CurrentPlayerID(c.Request.Context()), id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func (s *Server) replaceConversationShare(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var input shareapp.CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, ok := currentShareUser(c)
	if !ok {
		return
	}
	view, err := s.shares.Replace(c.Request.Context(), user.ID.String(), user.DisplayName, shareAvatarURL(user), id, input)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, view)
}

func currentShareUser(c *gin.Context) (*storage.User, bool) {
	value, exists := c.Get(authUserKey)
	user, ok := value.(*storage.User)
	if !exists || !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return nil, false
	}
	return user, true
}

func shareAvatarURL(user *storage.User) string {
	if user == nil || user.AvatarURL == nil {
		return ""
	}
	return *user.AvatarURL
}

func (s *Server) getPublicConversationShare(c *gin.Context) {
	if !s.shares.AllowPublicRead(c.Param("token"), c.ClientIP()) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": shareapp.ErrRateLimited.Error()})
		return
	}
	view, err := s.shares.Public(c.Request.Context(), c.Param("token"))
	if err != nil {
		if errors.Is(err, shareapp.ErrSigningUnavailable) {
			fail(c, err)
			return
		}
		publicShareNotFound(c)
		return
	}
	writePublicConversationShare(c, view)
}

func writePublicConversationShare(c *gin.Context, view *shareapp.PublicView) {
	etag := `"` + view.SnapshotHash + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=0, must-revalidate")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, view)
}

// getConversationShareHTML gives social crawlers server-rendered metadata.
// Browsers immediately continue to the isolated SPA route; no snapshot body is
// embedded in the document beyond a short, escaped public excerpt.
func (s *Server) getConversationShareHTML(c *gin.Context) {
	token := c.Param("token")
	if !s.shares.AllowPublicRead(token, c.ClientIP()) {
		c.Header("Retry-After", "60")
		c.String(http.StatusTooManyRequests, "请求过于频繁")
		return
	}
	view, err := s.shares.Public(c.Request.Context(), token)
	data := conversationShareHTMLData{Title: "分享已失效", Description: "这份故事分享不存在或已失效。", Token: token}
	status := http.StatusOK
	if err != nil {
		if errors.Is(err, shareapp.ErrSigningUnavailable) {
			c.String(http.StatusServiceUnavailable, "分享服务暂不可用")
			return
		}
		status = http.StatusNotFound
	} else {
		data.Title = view.Snapshot.Title
		data.Description = shareDescription(view.Snapshot)
	}
	var output bytes.Buffer
	if err := conversationShareHTML.Execute(&output, data); err != nil {
		c.String(http.StatusInternalServerError, "分享页面生成失败")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	c.Data(status, "text/html; charset=utf-8", output.Bytes())
}

func shareDescription(snapshot shareapp.Snapshot) string {
	for _, message := range snapshot.Messages {
		plain := strings.Join(strings.Fields(message.Markdown), " ")
		if plain == "" {
			continue
		}
		runes := []rune(plain)
		if len(runes) > 160 {
			plain = string(runes[:160]) + "..."
		}
		return plain
	}
	return snapshot.Attribution + " 分享的 Fatecraft 故事"
}

func (s *Server) reportPublicConversationShare(c *gin.Context) {
	var input shareapp.ReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reporter := c.ClientIP() + "|" + c.GetHeader("User-Agent")
	err := s.shares.Report(c.Request.Context(), c.Param("token"), reporter, input)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			publicShareNotFound(c)
			return
		}
		if errors.Is(err, shareapp.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, shareapp.ErrRateLimited) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			c.JSON(http.StatusOK, gin.H{"status": "reported"})
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "reported"})
}

func publicShareNotFound(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.JSON(http.StatusNotFound, gin.H{"error": "分享不存在或已失效"})
}

func (s *Server) listAdminConversationShares(c *gin.Context) {
	limit, ok := adminShareLimit(c)
	if !ok {
		return
	}
	rows, err := s.shares.ListAdmin(c.Request.Context(), strings.TrimSpace(c.Query("status")), limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"shares": rows})
}

func (s *Server) moderateAdminConversationShare(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	adminID, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var input shareapp.AdminModerateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.shares.Moderate(c.Request.Context(), id, adminID, input.Reason); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "moderated"})
}

func (s *Server) listAdminShareReports(c *gin.Context) {
	limit, ok := adminShareLimit(c)
	if !ok {
		return
	}
	rows, err := s.shares.ListReports(c.Request.Context(), strings.TrimSpace(c.Query("status")), limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": rows})
}

func (s *Server) resolveAdminShareReport(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	adminID, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var input shareapp.AdminResolveReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.shares.ResolveReport(c.Request.Context(), id, adminID, input.Status); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": input.Status})
}

func adminShareLimit(c *gin.Context) (int, bool) {
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 200 的整数"})
			return 0, false
		}
		limit = parsed
	}
	return limit, true
}
