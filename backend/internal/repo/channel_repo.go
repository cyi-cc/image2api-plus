package repo

import (
	"context"
	"time"

	"backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChannelRepository struct {
	db *gorm.DB
}

func NewChannelRepository(db *gorm.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

// ChannelWithUpstream pairs a channel row with the upstream model name a
// binding carries — the shape dispatch needs in one fetch.
type ChannelWithUpstream struct {
	Channel       model.Channel
	UpstreamModel string
}

func (r *ChannelRepository) List(ctx context.Context) ([]model.Channel, error) {
	var items []model.Channel
	if err := r.db.WithContext(ctx).Order("created_at asc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ChannelRepository) Get(ctx context.Context, id string) (*model.Channel, error) {
	var item model.Channel
	if err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ChannelRepository) Create(ctx context.Context, item *model.Channel) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *ChannelRepository) Update(ctx context.Context, id string, patch map[string]any) (*model.Channel, error) {
	patch["updated_at"] = time.Now()
	if err := r.db.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(patch).Error; err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *ChannelRepository) Delete(ctx context.Context, id string) (int64, error) {
	res := r.db.WithContext(ctx).Delete(&model.Channel{}, "id = ?", id)
	return res.RowsAffected, res.Error
}

// Bindings returns all channel_models rows for a channel.
func (r *ChannelRepository) Bindings(ctx context.Context, channelID string) ([]model.ChannelModel, error) {
	var items []model.ChannelModel
	if err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// Bind upserts one channel↔model binding (upstream model name is refreshed on
// conflict so re-import can rename it).
func (r *ChannelRepository) Bind(ctx context.Context, channelID, modelID, upstreamModel string) error {
	row := model.ChannelModel{ChannelID: channelID, ModelID: modelID, UpstreamModel: upstreamModel}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "model_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"upstream_model", "updated_at"}),
	}).Create(&row).Error
}

func (r *ChannelRepository) Unbind(ctx context.Context, channelID, modelID string) error {
	return r.db.WithContext(ctx).Delete(&model.ChannelModel{}, "channel_id = ? AND model_id = ?", channelID, modelID).Error
}

func (r *ChannelRepository) DeleteBindingsByChannel(ctx context.Context, channelID string) error {
	return r.db.WithContext(ctx).Delete(&model.ChannelModel{}, "channel_id = ?", channelID).Error
}

func (r *ChannelRepository) DeleteBindingsByModel(ctx context.Context, modelID string) error {
	return r.db.WithContext(ctx).Delete(&model.ChannelModel{}, "model_id = ?", modelID).Error
}

// ForModel returns enabled channels bound to modelID (channel rows + the
// upstream model name each binding carries), stable-ordered for dispatch.
func (r *ChannelRepository) ForModel(ctx context.Context, modelID string) ([]ChannelWithUpstream, error) {
	var rows []struct {
		model.Channel
		UpstreamModel string `gorm:"column:upstream_model"`
	}
	err := r.db.WithContext(ctx).
		Table("channels").
		Select("channels.*, channel_models.upstream_model").
		Joins("JOIN channel_models ON channel_models.channel_id = channels.id").
		Where("channel_models.model_id = ? AND channels.enabled = ?", modelID, true).
		Order("channels.id asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ChannelWithUpstream, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChannelWithUpstream{Channel: row.Channel, UpstreamModel: row.UpstreamModel})
	}
	return out, nil
}

// ForModelAnyState is ForModel without the enabled filter — used by admin
// channel-pinned tests which deliberately target one channel even when it's
// disabled for traffic.
func (r *ChannelRepository) ForModelAnyState(ctx context.Context, modelID string) ([]ChannelWithUpstream, error) {
	var rows []struct {
		model.Channel
		UpstreamModel string `gorm:"column:upstream_model"`
	}
	err := r.db.WithContext(ctx).
		Table("channels").
		Select("channels.*, channel_models.upstream_model").
		Joins("JOIN channel_models ON channel_models.channel_id = channels.id").
		Where("channel_models.model_id = ?", modelID).
		Order("channels.id asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ChannelWithUpstream, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChannelWithUpstream{Channel: row.Channel, UpstreamModel: row.UpstreamModel})
	}
	return out, nil
}

// ModelIDsByChannel lists local model ids bound to a channel.
func (r *ChannelRepository) ModelIDsByChannel(ctx context.Context, channelID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ChannelModel{}).
		Where("channel_id = ?", channelID).Pluck("model_id", &ids).Error
	return ids, err
}

// UpstreamModelMap returns model_id → upstream_model for a channel.
func (r *ChannelRepository) UpstreamModelMap(ctx context.Context, channelID string) (map[string]string, error) {
	var rows []model.ChannelModel
	if err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.ModelID] = row.UpstreamModel
	}
	return out, nil
}

// ChannelIDsByModel lists channel ids bound to a model (any enabled state).
func (r *ChannelRepository) ChannelIDsByModel(ctx context.Context, modelID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ChannelModel{}).
		Where("model_id = ?", modelID).Pluck("channel_id", &ids).Error
	return ids, err
}

// OrphanModelIDs returns local model ids that no channel binds anymore.
func (r *ChannelRepository) OrphanModelIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Table("model_configs").
		Select("model_configs.id").
		Joins("LEFT JOIN channel_models ON channel_models.model_id = model_configs.id").
		Where("channel_models.model_id IS NULL").
		Scan(&ids).Error
	return ids, err
}

// BumpSuccess records one successful upstream call (count + reported credits
// spent + last-used timestamp). Best-effort.
func (r *ChannelRepository) BumpSuccess(ctx context.Context, channelID string, credits float64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"success_total": gorm.Expr("success_total + 1"),
		"used_credits":  gorm.Expr("used_credits + ?", credits),
		"last_used_at":  now,
	}).Error
}
