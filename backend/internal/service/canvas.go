package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"backend/internal/model"
	"backend/internal/repo"
)

// CanvasService owns the infinite-canvas documents (无限画布): whole-document
// JSON blobs per user, written by the frontend's debounced autosave. The
// service validates shape/size and enforces a per-user document cap so a
// runaway client can't fan out unbounded rows.
type CanvasService struct {
	canvases *repo.CanvasRepository
}

var (
	ErrCanvasNotFound = errors.New("画布不存在")
	ErrCanvasLimit    = errors.New("画布数量已达上限")
	ErrCanvasTooLarge = errors.New("画布数据过大")
	ErrCanvasBadJSON  = errors.New("画布数据格式错误")
)

const (
	maxCanvasesPerUser = 50
	maxCanvasDataBytes = 4 << 20 // 4 MiB per document
	maxCanvasTitle     = 120
)

func NewCanvasService(canvases *repo.CanvasRepository) *CanvasService {
	return &CanvasService{canvases: canvases}
}

// ListSummary is the picker row — Data deliberately omitted.
type ListSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *CanvasService) List(ctx context.Context, userID string) ([]ListSummary, error) {
	items, err := s.canvases.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ListSummary, 0, len(items))
	for _, c := range items {
		out = append(out, ListSummary{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt})
	}
	return out, nil
}

// Create provisions an empty canvas with a starter document so the frontend
// can render immediately without a special "new" state.
func (s *CanvasService) Create(ctx context.Context, userID, title string) (*model.Canvas, error) {
	if n, err := s.canvases.CountByUser(ctx, userID); err != nil {
		return nil, err
	} else if n >= maxCanvasesPerUser {
		return nil, ErrCanvasLimit
	}
	title = normalizeCanvasTitle(title)
	starter, _ := json.Marshal(map[string]any{
		"version": 1,
		"viewport": map[string]any{"x": 0, "y": 0, "scale": 1},
		"nodes":    []any{},
		"edges":    []any{},
	})
	c := &model.Canvas{
		ID:        "cv-" + randomUpper(10),
		UserID:    userID,
		Title:     title,
		Data:      string(starter),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.canvases.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *CanvasService) Get(ctx context.Context, userID, id string) (*model.Canvas, error) {
	c, err := s.canvases.Get(ctx, userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCanvasNotFound
	}
	return c, err
}

// Save replaces the document (and optionally renames). The blob must be valid
// JSON with the expected envelope; size is capped before it hits the DB.
func (s *CanvasService) Save(ctx context.Context, userID, id string, title *string, data string) error {
	data = strings.TrimSpace(data)
	if len(data) > maxCanvasDataBytes {
		return ErrCanvasTooLarge
	}
	if !json.Valid([]byte(data)) {
		return ErrCanvasBadJSON
	}
	fields := map[string]any{"data": data, "updated_at": time.Now()}
	if title != nil {
		fields["title"] = normalizeCanvasTitle(*title)
	}
	err := s.canvases.Update(ctx, userID, id, fields)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCanvasNotFound
	}
	return err
}

func (s *CanvasService) Delete(ctx context.Context, userID, id string) error {
	err := s.canvases.Delete(ctx, userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCanvasNotFound
	}
	return err
}

func normalizeCanvasTitle(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		t = "未命名画布"
	}
	runes := []rune(t)
	if len(runes) > maxCanvasTitle {
		t = string(runes[:maxCanvasTitle])
	}
	return t
}
