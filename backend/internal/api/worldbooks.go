package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/worldbook"
)

func (s *Server) listWorldBooks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"worldbooks": s.world.List()})
}

func (s *Server) getWorldBook(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	b, err := s.world.Get(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, b)
}

func (s *Server) createWorldBook(c *gin.Context) {
	var in worldbook.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	b, err := s.world.Create(in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, b)
}

func (s *Server) updateWorldBook(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var in worldbook.UpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	book, err := s.world.Update(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, book)
}

func (s *Server) deleteWorldBook(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	if err := s.world.Delete(id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) addEntry(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var in worldbook.EntryInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	e, err := s.world.AddEntry(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, e)
}

func (s *Server) updateEntry(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var in worldbook.EntryInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	e, err := s.world.UpdateEntry(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}

func (s *Server) deleteEntry(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	if err := s.world.DeleteEntry(id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) linkWorldBook(c *gin.Context) {
	convID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	bookID, ok := paramSlug(c, "world_book_id")
	if !ok {
		return
	}
	// 模板已不在数据库（无外键约束），绑定前确认世界书文件存在。
	if _, err := s.world.Get(bookID); err != nil {
		fail(c, err)
		return
	}
	if err := s.conv.AddWorldBook(c.Request.Context(), convID, bookID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "linked"})
}

func (s *Server) unlinkWorldBook(c *gin.Context) {
	convID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	bookID, ok := paramSlug(c, "world_book_id")
	if !ok {
		return
	}
	if err := s.conv.RemoveWorldBook(c.Request.Context(), convID, bookID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unlinked"})
}
