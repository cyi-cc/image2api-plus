package handler

import (
	"errors"
	"net/http"
	"strings"

	"backend/internal/service"
	"backend/internal/upstream"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChannelAdminHandler exposes channel CRUD + upstream model import under
// /admin/api/channels*. Replaces the old token/account admin surface.
type ChannelAdminHandler struct {
	channels *service.ChannelService
}

func NewChannelAdminHandler(channels *service.ChannelService) *ChannelAdminHandler {
	return &ChannelAdminHandler{channels: channels}
}

func (h *ChannelAdminHandler) List(c *gin.Context) {
	data, err := h.channels.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load channels"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (h *ChannelAdminHandler) Save(c *gin.Context) {
	var body service.SaveChannelDto
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	body.BaseURL = strings.TrimSpace(body.BaseURL)
	ch, err := h.channels.Save(c.Request.Context(), body)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "channel not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": ch.ID})
}

func (h *ChannelAdminHandler) Delete(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "channel id required"})
		return
	}
	if err := h.channels.Delete(c.Request.Context(), body.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"detail": "channel not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// FetchModels proxies GET /v1/models on a channel (stored creds or ad-hoc
// base_url+api_key for a not-yet-saved channel form).
func (h *ChannelAdminHandler) FetchModels(c *gin.Context) {
	var body service.FetchModelsDto
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	items, err := h.channels.FetchModels(c.Request.Context(), body)
	if err != nil {
		writeUpstreamErr(c, err)
		return
	}
	if items == nil {
		items = []upstream.RemoteModel{}
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// ImportModels binds the ticked upstream models to the channel (creating local
// model rows on first import).
func (h *ChannelAdminHandler) ImportModels(c *gin.Context) {
	var body struct {
		ID     string                     `json:"id"`
		Models []service.ChannelModelSpec `json:"models"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "channel id required"})
		return
	}
	if err := h.channels.ImportModels(c.Request.Context(), body.ID, body.Models); err != nil {
		writeUpstreamErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// writeUpstreamErr translates upstream-protocol errors into sensible admin
// responses (auth → 400 with the upstream's message, temporary → 502).
func writeUpstreamErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, upstream.ErrAuth):
		c.JSON(http.StatusBadRequest, gin.H{"detail": "上游鉴权失败：" + err.Error()})
	case errors.Is(err, upstream.ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"detail": "channel not found"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"detail": "上游请求失败：" + err.Error()})
	}
}
