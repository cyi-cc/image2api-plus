package repo

import (
	"context"

	"gorm.io/gorm"

	"backend/internal/model"
)

// CanvasRepository persists infinite-canvas documents. All queries are
// userID-scoped — a canvas is only ever read or written by its owner.
type CanvasRepository struct {
	db *gorm.DB
}

func NewCanvasRepository(db *gorm.DB) *CanvasRepository {
	return &CanvasRepository{db: db}
}

// List returns the user's canvases newest-first, without the (potentially
// large) Data blob — the picker only needs id/title/updatedAt.
func (r *CanvasRepository) List(ctx context.Context, userID string) ([]model.Canvas, error) {
	var items []model.Canvas
	err := r.db.WithContext(ctx).
		Select("id", "user_id", "title", "updated_at").
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&items).Error
	return items, err
}

func (r *CanvasRepository) Get(ctx context.Context, userID, id string) (*model.Canvas, error) {
	var c model.Canvas
	if err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CanvasRepository) Create(ctx context.Context, c *model.Canvas) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// Update replaces title and/or the document blob. A no-match update reports
// ErrRecordNotFound (gorm's Updates is silent about missing rows).
func (r *CanvasRepository) Update(ctx context.Context, userID, id string, fields map[string]any) error {
	res := r.db.WithContext(ctx).
		Model(&model.Canvas{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *CanvasRepository) Delete(ctx context.Context, userID, id string) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.Canvas{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CountByUser backs the per-user document cap.
func (r *CanvasRepository) CountByUser(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&model.Canvas{}).
		Where("user_id = ?", userID).
		Count(&n).Error
	return n, err
}
