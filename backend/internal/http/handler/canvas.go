package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"backend/internal/model"
	"backend/internal/service"
)

// CanvasHandler exposes the infinite-canvas CRUD under the session-authed
// /admin/api group. Every operation is scoped to the logged-in user.
type CanvasHandler struct {
	svc *service.CanvasService
}

func NewCanvasHandler(svc *service.CanvasService) *CanvasHandler {
	return &CanvasHandler{svc: svc}
}

// List serves the canvas picker (id/title/updatedAt only).
func (h *CanvasHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), currentUser(c).ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list canvases"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// Create provisions a new canvas; title is optional.
func (h *CanvasHandler) Create(c *gin.Context) {
	var body struct {
	 Title *string `json:"title"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		// Empty body is fine — treat as no title.
		body.Title = nil
	}
	cv, err := h.svc.Create(c.Request.Context(), currentUser(c).ID, deref(body.Title))
	if err != nil {
		respondCanvasError(c, err)
		return
	}
	c.JSON(http.StatusOK, canvasDoc(cv))
}

// Get returns the full document blob (parsed by the client).
func (h *CanvasHandler) Get(c *gin.Context) {
	cv, err := h.svc.Get(c.Request.Context(), currentUser(c).ID, c.Param("id"))
	if err != nil {
		respondCanvasError(c, err)
		return
	}
	c.JSON(http.StatusOK, canvasDoc(cv))
}

// Save replaces the whole document (autosave) and/or renames.
func (h *CanvasHandler) Save(c *gin.Context) {
	var body struct {
		Title *string `json:"title"`
		Data  string  `json:"data"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	if err := h.svc.Save(c.Request.Context(), currentUser(c).ID, c.Param("id"), body.Title, body.Data); err != nil {
		respondCanvasError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *CanvasHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), currentUser(c).ID, c.Param("id")); err != nil {
		respondCanvasError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func canvasDoc(cv *model.Canvas) gin.H {
	return gin.H{"id": cv.ID, "title": cv.Title, "data": cv.Data, "updated_at": cv.UpdatedAt}
}

func respondCanvasError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCanvasNotFound):
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
	case errors.Is(err, service.ErrCanvasLimit),
		errors.Is(err, service.ErrCanvasTooLarge),
		errors.Is(err, service.ErrCanvasBadJSON):
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "canvas operation failed"})
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
