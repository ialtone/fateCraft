package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/jsonstrict"
	"github.com/ialtone/fatecraft/internal/storage"
)

func bindGameplayJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, challengeimport.MaxUploadBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	return jsonstrict.Decode(raw, target)
}

type gameplayExchange interface {
	PreviewUpload(context.Context, uuid.UUID, string, []byte, challengeimport.PreviewOptions) (*storage.GameplayImport, error)
	ConfirmUpload(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*storage.GameplayImport, error)
	ListImports(context.Context, uuid.UUID, *storage.ImportCursor) ([]storage.GameplayImport, error)
	ImportRecord(context.Context, uuid.UUID, uuid.UUID) (*storage.GameplayImport, error)
	CancelImport(context.Context, uuid.UUID, uuid.UUID) error
	ExportVersion(context.Context, uuid.UUID, string) ([]byte, string, error)
}

func (s *Server) exchange(c *gin.Context) gameplayExchange {
	e, ok := s.builtins.(gameplayExchange)
	if !ok {
		c.JSON(503, gin.H{"error": "玩法包交换服务未配置"})
		return nil
	}
	return e
}
func (s *Server) previewGameplayUpload(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, challengeimport.MaxUploadBytes+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.JSON(400, gin.H{"error": "上传格式无效或文件超过 16 MiB"})
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请选择玩法包文件"})
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, challengeimport.MaxUploadBytes+1))
	if err != nil || len(raw) > challengeimport.MaxUploadBytes {
		c.JSON(413, gin.H{"error": "上传文件超过 16 MiB"})
		return
	}
	result, err := e.PreviewUpload(c.Request.Context(), rewardActor(c), header.Filename, raw, challengeimport.PreviewOptions{Strategy: c.PostForm("strategy"), TargetSlug: c.PostForm("target_slug"), TrustedUpdate: c.PostForm("trusted_update") == "true"})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, result)
}
func (s *Server) confirmGameplayUpload(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RequestID == uuid.Nil {
		c.JSON(400, gin.H{"error": "请提供 request_id"})
		return
	}
	row, err := e.ConfirmUpload(c.Request.Context(), rewardActor(c), id, body.RequestID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, row)
}
func (s *Server) listGameplayImports(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	var before *storage.ImportCursor
	if raw := c.Query("before"); raw != "" {
		parts := strings.Split(raw, "|")
		if len(parts) != 2 {
			c.JSON(400, gin.H{"error": "分页游标无效"})
			return
		}
		cursorID, err := uuid.Parse(parts[1])
		if err != nil {
			c.JSON(400, gin.H{"error": "分页游标无效"})
			return
		}
		parsed, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			c.JSON(400, gin.H{"error": "分页时间无效"})
			return
		}
		before = &storage.ImportCursor{CreatedAt: parsed, ID: cursorID}
	}
	rows, err := e.ListImports(c.Request.Context(), rewardActor(c), before)
	if err != nil {
		fail(c, err)
		return
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].CreatedAt.Format(time.RFC3339Nano) + "|" + rows[49].ID.String()
	}
	c.JSON(200, gin.H{"imports": rows, "next_cursor": next})
}
func (s *Server) getGameplayImport(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := e.ImportRecord(c.Request.Context(), rewardActor(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, row)
}
func (s *Server) cancelGameplayImport(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if err := e.CancelImport(c.Request.Context(), rewardActor(c), id); err != nil {
		fail(c, err)
		return
	}
	c.Status(204)
}
func (s *Server) exportGameplayVersion(c *gin.Context) {
	e := s.exchange(c)
	if e == nil {
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	raw, name, err := e.ExportVersion(c.Request.Context(), id, c.Query("format"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Header("Cache-Control", "no-store")
	mime := "application/json; charset=utf-8"
	if c.Query("format") == "zip" {
		mime = "application/zip"
	}
	c.Data(200, mime, raw)
}
