package handler

import (
	"net/http"

	"backend/internal/service"
	"github.com/gin-gonic/gin"
)

type ShowcaseHandler struct {
	showcase *service.ShowcaseService
}

func NewShowcaseHandler(showcase *service.ShowcaseService) *ShowcaseHandler {
	return &ShowcaseHandler{showcase: showcase}
}

func (h *ShowcaseHandler) List(c *gin.Context) {
	grouped, err := h.showcase.Grouped(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load showcase"})
		return
	}
	out := gin.H{}
	for kind, items := range grouped {
		rows := make([]gin.H, 0, len(items))
		for _, item := range items {
			rows = append(rows, gin.H{
				"id":         item.ID,
				"kind":       item.Kind,
				"title":      item.Title,
				"subtitle":   item.Subtitle,
				"prompt":     item.Prompt,
				"gradient":   item.Gradient,
				"span":       item.Span,
				"image":      item.Image,
				"weight":     item.Weight,
				"created_at": item.CreatedAt,
				"updated_at": item.UpdatedAt,
			})
		}
		out[kind] = rows
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// AdminList is the paginated flat view for the admin 首页内容 page — the public
// List keeps its grouped shape for the home page, this one adds kind filter +
// limit/offset/total. Order mirrors the old client flattening: hero → work.
func (h *ShowcaseHandler) AdminList(c *gin.Context) {
	grouped, err := h.showcase.Grouped(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load showcase"})
		return
	}
	kindFilter := c.Query("kind") // "" | hero | work
	flat := make([]gin.H, 0, 16)
	for _, kind := range []string{"hero", "work"} {
		if kindFilter != "" && kind != kindFilter {
			continue
		}
		for _, item := range grouped[kind] {
			flat = append(flat, gin.H{
				"id":         item.ID,
				"kind":       item.Kind,
				"title":      item.Title,
				"subtitle":   item.Subtitle,
				"prompt":     item.Prompt,
				"gradient":   item.Gradient,
				"span":       item.Span,
				"image":      item.Image,
				"weight":     item.Weight,
				"created_at": item.CreatedAt,
				"updated_at": item.UpdatedAt,
			})
		}
	}
	total := len(flat)
	limit, offset := pageParams(c, 12)
	page := pageSlice(flat, limit, offset)
	c.JSON(http.StatusOK, gin.H{"data": page, "total": total, "limit": limit, "offset": offset})
}
