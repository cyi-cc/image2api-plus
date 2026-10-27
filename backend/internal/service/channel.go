package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"backend/internal/model"
	"backend/internal/repo"
	"backend/internal/upstream"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ChannelService manages upstream vivid-protocol channels: CRUD, upstream key
// encryption at rest, model fetching/import/binding, and live balance probes.
// The actual request dispatch lives on V1Service (it owns concurrency + events).
type ChannelService struct {
	channels *repo.ChannelRepository
	models   *repo.ModelRepository
	events   *repo.EventRepository
	conc     *ConcurrencyService
	up       *upstream.Client
	secret   []byte
}

func NewChannelService(channels *repo.ChannelRepository, models *repo.ModelRepository, events *repo.EventRepository, conc *ConcurrencyService, up *upstream.Client, secret string) *ChannelService {
	sum := sha256.Sum256([]byte("image2api-channel-keys|" + secret))
	return &ChannelService{channels: channels, models: models, events: events, conc: conc, up: up, secret: sum[:]}
}

// ChannelView is the admin list row: local stats + bound model names + a live
// upstream balance probe (nil when the upstream doesn't implement /v1/balance
// or the probe failed).
type ChannelView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	BaseURL         string   `json:"base_url"`
	Enabled         bool     `json:"enabled"`
	Concurrency     int      `json:"concurrency"`
	Weight          int      `json:"weight"`
	Models          []string `json:"models"`
	// RunningCount 运行中 = Redis 并发槽位实时计数（与调度器同源）；
	// FailCount 失败数 = event_logs 实时聚合（channel 数少，无需预计算）。
	RunningCount    int64    `json:"running_count"`
	FailCount       int64    `json:"fail_count"`
	SuccessTotal    int64    `json:"success_total"`
	UsedCredits     float64  `json:"used_credits"`
	UpstreamBalance *int64   `json:"upstream_balance"`
	LastUsedAt      *int64   `json:"last_used_at"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

func (s *ChannelService) List(ctx context.Context) ([]ChannelView, error) {
	items, err := s.channels.List(ctx)
	if err != nil {
		return nil, err
	}
	fails, _ := s.events.FailedByAccount(ctx)
	out := make([]ChannelView, len(items))
	var wg sync.WaitGroup
	for i, ch := range items {
		out[i] = ChannelView{
			ID:           ch.ID,
			Name:         ch.Name,
			BaseURL:      ch.BaseURL,
			Enabled:      ch.Enabled,
			Concurrency:  ch.Concurrency,
			Weight:       ch.Weight,
			RunningCount: int64(s.conc.Count(ctx, "conc:c:"+ch.ID)),
			FailCount:    fails[ch.ID],
			SuccessTotal: ch.SuccessTotal,
			UsedCredits:  ch.UsedCredits,
			CreatedAt:    ch.CreatedAt.Unix(),
			UpdatedAt:    ch.UpdatedAt.Unix(),
		}
		if ch.LastUsedAt != nil {
			t := ch.LastUsedAt.Unix()
			out[i].LastUsedAt = &t
		}
		if ids, err := s.channels.ModelIDsByChannel(ctx, ch.ID); err == nil {
			names := make([]string, 0, len(ids))
			for _, id := range ids {
				if m, err := s.models.Get(ctx, id); err == nil {
					names = append(names, m.EffectiveName())
				} else {
					names = append(names, id)
				}
			}
			out[i].Models = names
		}
		if key := s.decrypt(ch.APIKeyEnc); key != "" {
			wg.Add(1)
			go func(i int, ch model.Channel, key string) {
				defer wg.Done()
				bctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if bal, err := s.up.Balance(bctx, ch.BaseURL, key); err == nil {
					out[i].UpstreamBalance = bal
				}
			}(i, ch, key)
		}
	}
	wg.Wait()
	return out, nil
}

// SaveChannelDto is the admin create/update payload. ApiKey empty or the mask
// placeholder keeps the existing key on update; required on create.
type SaveChannelDto struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Enabled     bool   `json:"enabled"`
	Concurrency int    `json:"concurrency"`
	Weight      int    `json:"weight"`
}

// Save upserts a channel then auto-syncs its upstream models (same semantics
// as vivid: upstream /v1/models is the truth). The returned warning is a
// non-fatal sync message — "" means fully synced.
func (s *ChannelService) Save(ctx context.Context, d SaveChannelDto) (*model.Channel, string, error) {
	name := strings.TrimSpace(d.Name)
	base := strings.TrimRight(strings.TrimSpace(d.BaseURL), "/")
	if base == "" {
		return nil, "", errors.New("base_url required")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, "", errors.New("base_url must start with http:// or https://")
	}
	if d.Concurrency < 0 || d.Concurrency > math.MaxInt32 {
		return nil, "", errors.New("并发数需在 0-2147483647 之间（0 = 不限制）")
	}
	weight := d.Weight
	if weight < 1 {
		weight = 1
	}
	if d.ID == "" {
		if strings.TrimSpace(d.APIKey) == "" {
			return nil, "", errors.New("api_key required")
		}
		enc, err := s.encrypt(d.APIKey)
		if err != nil {
			return nil, "", err
		}
		ch := &model.Channel{
			ID:          "ch-" + randomUpper(10),
			Name:        name,
			BaseURL:     base,
			APIKeyEnc:   enc,
			Enabled:     d.Enabled,
			Concurrency: d.Concurrency,
			Weight:      weight,
		}
		if ch.Name == "" {
			ch.Name = base
		}
		if err := s.channels.Create(ctx, ch); err != nil {
			return nil, "", err
		}
		return ch, syncWarn(s.syncChannelModels(ctx, ch.ID)), nil
	}
	patch := map[string]any{
		"name":        name,
		"base_url":    base,
		"enabled":     d.Enabled,
		"concurrency": d.Concurrency,
		"weight":      weight,
	}
	if key := strings.TrimSpace(d.APIKey); key != "" && !strings.Contains(key, "•") && !strings.Contains(key, "*") {
		enc, err := s.encrypt(key)
		if err != nil {
			return nil, "", err
		}
		patch["api_key_enc"] = enc
	}
	ch, err := s.channels.Update(ctx, d.ID, patch)
	if err != nil {
		return nil, "", err
	}
	return ch, syncWarn(s.syncChannelModels(ctx, d.ID)), nil
}

// syncWarn folds the sync error into the save-path warning (vivid parity: a
// failed upstream fetch degrades to a non-fatal warning instead of blocking
// the channel save).
func syncWarn(warn string, err error) string {
	if err != nil {
		return err.Error()
	}
	return warn
}

// SyncModels manually triggers a full upstream-model sync for a saved channel
// (same truth semantics as the save path). Unlike Save, an unreachable upstream
// or empty model list is a hard error, not a warning.
func (s *ChannelService) SyncModels(ctx context.Context, channelID string) (string, error) {
	if _, err := s.channels.Get(ctx, strings.TrimSpace(channelID)); err != nil {
		return "", err
	}
	warn, err := s.syncChannelModels(ctx, channelID)
	if err != nil {
		return "", err
	}
	if warn != "" {
		return warn, nil
	}
	return "已同步上游模型", nil
}

// syncChannelModels runs after Save: upstream /v1/models is the truth — every
// sellable upstream model is upserted locally (alias = upstream id) and bound;
// bindings on this channel not in the upstream list are removed, and orphan
// local models are deleted. An unreachable upstream or empty list is an error
// (the save path degrades it to a warning); warn carries non-fatal skips.
func (s *ChannelService) syncChannelModels(ctx context.Context, channelID string) (string, error) {
	ch, err := s.channels.Get(ctx, channelID)
	if err != nil {
		return "", fmt.Errorf("模型同步失败：%w", err)
	}
	key := s.decrypt(ch.APIKeyEnc)
	if key == "" {
		return "", errors.New("模型同步失败：无法解密上游密钥")
	}
	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	models, err := s.up.Models(fctx, ch.BaseURL, key)
	if err != nil {
		return "", fmt.Errorf("模型同步失败：%w", err)
	}
	if len(models) == 0 {
		return "", errors.New("上游未返回任何模型，已跳过同步")
	}
	specs := make([]ChannelModelSpec, 0, len(models))
	upstreamIDs := make([]string, 0, len(models))
	var skipped []string
	for _, m := range models {
		spec, serr := normalizeRemoteModel(m)
		if serr != nil {
			skipped = append(skipped, m.ID+"："+serr.Error())
			continue
		}
		specs = append(specs, spec)
		upstreamIDs = append(upstreamIDs, spec.ID)
	}
	if len(upstreamIDs) == 0 {
		return "", fmt.Errorf("上游模型均规格异常，已跳过同步：%s", strings.Join(skipped, "；"))
	}
	if err := s.ImportModels(ctx, channelID, specs); err != nil {
		return "", fmt.Errorf("模型同步失败：%w", err)
	}
	if err := s.channels.UnbindExcept(ctx, channelID, upstreamIDs); err != nil {
		return "", fmt.Errorf("模型同步失败：%w", err)
	}
	if orphans, oerr := s.channels.OrphanModelIDs(ctx); oerr == nil {
		for _, mid := range orphans {
			_, _ = s.models.Delete(ctx, mid)
		}
	}
	if len(skipped) > 0 {
		return fmt.Sprintf("已同步上游模型，跳过 %d 个异常项：%s", len(skipped), strings.Join(skipped, "；")), nil
	}
	return "", nil
}

// normalizeRemoteModel validates an upstream /v1/models item into an import
// spec (alias = upstream id).
func normalizeRemoteModel(m upstream.RemoteModel) (ChannelModelSpec, error) {
	spec := ChannelModelSpec{
		ID:              strings.TrimSpace(m.ID),
		Kind:            strings.TrimSpace(m.Kind),
		Qualities:       m.Qualities,
		Duration:        m.Duration,
		RefImages:       m.RefImages,
		RefVideos:       m.RefVideos,
		RefAudios:       m.RefAudios,
		RefVideoSeconds: m.RefVideoSeconds,
		RefAudioSeconds: m.RefAudioSeconds,
	}
	if spec.ID == "" {
		return spec, errors.New("模型 id 为空")
	}
	switch spec.Kind {
	case "image", "video", "text":
	default:
		return spec, fmt.Errorf("未知类型 %q", spec.Kind)
	}
	if spec.Duration != nil {
		switch spec.Duration.Mode {
		case "options", "range":
		default:
			return spec, errors.New("时长模式仅支持 options / range")
		}
	}
	return spec, nil
}

// Delete removes a channel, its bindings, and any local model that ends up
// bound to no channel at all (same semantics as vivid).
func (s *ChannelService) Delete(ctx context.Context, id string) error {
	modelIDs, _ := s.channels.ModelIDsByChannel(ctx, id)
	if err := s.channels.DeleteBindingsByChannel(ctx, id); err != nil {
		return err
	}
	res := 0
	if n, err := s.channels.Delete(ctx, id); err != nil {
		return err
	} else {
		res = int(n)
	}
	if res == 0 {
		return gorm.ErrRecordNotFound
	}
	for _, mid := range modelIDs {
		rest, _ := s.channels.ChannelIDsByModel(ctx, mid)
		if len(rest) == 0 {
			_, _ = s.models.Delete(ctx, mid)
		}
	}
	return nil
}

// FetchModelsDto resolves credentials for a model fetch: Id uses the stored
// channel's key; otherwise BaseURL+ApiKey preview an unsaved channel.
type FetchModelsDto struct {
	ID      string `json:"id"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

// FetchModels lists the upstream's sellable models; when a saved channel id is
// given, items already bound to that channel are flagged Bound so the admin UI
// can render them as a bound/unbound toggle.
func (s *ChannelService) FetchModels(ctx context.Context, d FetchModelsDto) ([]upstream.RemoteModel, error) {
	base, key, err := s.resolveCreds(ctx, d.ID, d.BaseURL, d.APIKey)
	if err != nil {
		return nil, err
	}
	items, err := s.up.Models(ctx, base, key)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.ID) != "" {
		if bound, berr := s.channels.UpstreamModelMap(ctx, d.ID); berr == nil && len(bound) > 0 {
			set := make(map[string]struct{}, len(bound))
			for _, upID := range bound {
				set[upID] = struct{}{}
			}
			for i := range items {
				if _, ok := set[items[i].ID]; ok {
					items[i].Bound = true
				}
			}
		}
	}
	return items, nil
}

// PurgeOrphanModels sweeps model_configs rows left over from the provider era:
// an unbound model can never be routed, so any row that didn't come through a
// channel import (provider != "channel") is deleted at startup. Channel-typed
// orphans keep their pricing config for a later rebind.
func (s *ChannelService) PurgeOrphanModels(ctx context.Context) (int, error) {
	ids, err := s.channels.OrphanModelIDs(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		m, gerr := s.models.GetAny(ctx, id)
		if gerr != nil || m == nil || m.Provider == "channel" {
			continue
		}
		// provider 时代残留行没有复活路径，直接物理删除，不留软删垃圾。
		if _, derr := s.models.DeleteHard(ctx, id); derr == nil {
			n++
		}
	}
	return n, nil
}

// ChannelModelSpec is one model the admin ticks for import (same shape as the
// upstream /v1/models item plus an optional local alias).
type ChannelModelSpec struct {
	ID              string                   `json:"id"`
	Alias           string                   `json:"alias"`
	Kind            string                   `json:"kind"`
	Qualities       []string                 `json:"qualities"`
	Duration        *upstream.RemoteDuration `json:"duration,omitempty"`
	RefImages       int64                    `json:"refImages"`
	RefVideos       int64                    `json:"refVideos"`
	RefAudios       int64                    `json:"refAudios"`
	RefVideoSeconds int64                    `json:"refVideoSeconds,omitempty"`
	RefAudioSeconds int64                    `json:"refAudioSeconds,omitempty"`
}

func (s *ChannelService) resolveCreds(ctx context.Context, id, base, key string) (string, string, error) {
	if strings.TrimSpace(id) != "" {
		ch, err := s.channels.Get(ctx, id)
		if err != nil {
			return "", "", err
		}
		return strings.TrimRight(ch.BaseURL, "/"), s.decrypt(ch.APIKeyEnc), nil
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || strings.TrimSpace(key) == "" {
		return "", "", errors.New("id or base_url+api_key required")
	}
	return base, strings.TrimSpace(key), nil
}

// ImportModels creates/updates local model rows for the ticked specs and binds
// them to the channel. Pricing stays whatever the local row already has
// (empty for new rows — admin prices them in 模型管理).
func (s *ChannelService) ImportModels(ctx context.Context, channelID string, specs []ChannelModelSpec) error {
	ch, err := s.channels.Get(ctx, channelID)
	if err != nil {
		return err
	}
	if len(specs) > 50 {
		return errors.New("单次最多导入 50 个模型")
	}
	seenAliases := make(map[string]string, len(specs)) // localID → 上游 id，别名冲突当场拒绝
	for _, spec := range specs {
		upID := strings.TrimSpace(spec.ID)
		if upID == "" {
			continue
		}
		if spec.Duration != nil && spec.Duration.Mode != "options" && spec.Duration.Mode != "range" {
			return fmt.Errorf("模型 %s：时长模式仅支持 options / range", upID)
		}
		alias := strings.TrimSpace(spec.Alias)
		if alias == upID {
			return fmt.Errorf("「%s」别名不能与上游模型同名，留空即可", upID)
		}
		localID := alias
		if localID == "" {
			localID = upID
		}
		if prev, dup := seenAliases[localID]; dup && prev != upID {
			return fmt.Errorf("别名 %s 被多个上游模型复用", localID)
		}
		seenAliases[localID] = upID
		// GetAny: 软删行也纳入——重导同一模型时复活原行，计数/定价不丢。
		existing, gerr := s.models.GetAny(ctx, localID)
		if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
			existing = nil
		}
		if existing != nil && existing.Type != "" && spec.Kind != "" && existing.Type != spec.Kind {
			return fmt.Errorf("模型 %s 已存在且类型不同（%s）", localID, existing.Type)
		}
		var local *model.ModelConfig
		if existing != nil && existing.ID == localID {
			local = existing
		}
		patch := modelPatchFromSpec(spec, local)
		if local == nil {
			local = &model.ModelConfig{
				ID:        localID,
				Name:      localID,
				Type:      spec.Kind,
				Provider:  "channel",
				Ratios:    jsonArray([]string{"16:9"}),
				Prices:    datatypes.JSONMap{},
				CreatedAt: time.Now(),
			}
			applySpec(local, patch)
			if err := s.models.Create(ctx, local); err != nil {
				return fmt.Errorf("model %s: %w", localID, err)
			}
		} else {
			if local.DeletedAt.Valid {
				patch["deleted_at"] = nil // revive
			}
			if _, err := s.models.UpdateAny(ctx, localID, patch); err != nil {
				return fmt.Errorf("model %s: %w", localID, err)
			}
		}
		if err := s.channels.Bind(ctx, ch.ID, localID, upID); err != nil {
			return err
		}
	}
	return nil
}

// modelPatchFromSpec maps an upstream model spec onto model_configs columns.
// Prices/alias are intentionally untouched on update so admin pricing survives
// re-import.
func modelPatchFromSpec(spec ChannelModelSpec, existing *model.ModelConfig) map[string]any {
	patch := map[string]any{
		"type":                  spec.Kind,
		"resolutions":           jsonArray(spec.Qualities),
		"max_reference_images":  spec.RefImages,
		"max_reference_videos":  spec.RefVideos,
		"max_reference_audios":  spec.RefAudios,
		"max_ref_video_seconds": spec.RefVideoSeconds,
		"max_ref_audio_seconds": spec.RefAudioSeconds,
	}
	if spec.RefImages+spec.RefVideos+spec.RefAudios > 0 {
		patch["reference_mode"] = "asset"
		patch["image_to_image"] = spec.RefImages > 0
	}
	if d := spec.Duration; d != nil {
		switch d.Mode {
		case "options":
			opts := make([]string, 0, len(d.Options))
			for _, s := range d.Options {
				opts = append(opts, fmt.Sprintf("%ds", s))
			}
			patch["durations"] = jsonArray(opts)
			patch["duration_min"], patch["duration_max"] = int64(0), int64(0)
		case "range":
			patch["duration_min"], patch["duration_max"] = d.Min, d.Max
		}
	}
	return patch
}

func applySpec(m *model.ModelConfig, patch map[string]any) {
	if v, ok := patch["resolutions"]; ok {
		m.Resolutions = datatypes.JSON(mustJSON(v))
	}
	if v, ok := patch["durations"]; ok {
		m.Durations = datatypes.JSON(mustJSON(v))
	}
	if v, ok := patch["duration_min"]; ok {
		m.DurationMin = int(v.(int64))
	}
	if v, ok := patch["duration_max"]; ok {
		m.DurationMax = int(v.(int64))
	}
	m.MaxReferenceImages = int(specInt(patch, "max_reference_images"))
	m.MaxReferenceVideos = int(specInt(patch, "max_reference_videos"))
	m.MaxReferenceAudios = int(specInt(patch, "max_reference_audios"))
	m.MaxRefVideoSeconds = int(specInt(patch, "max_ref_video_seconds"))
	m.MaxRefAudioSeconds = int(specInt(patch, "max_ref_audio_seconds"))
	if v, ok := patch["reference_mode"].(string); ok {
		m.ReferenceMode = v
	}
	if v, ok := patch["image_to_image"].(bool); ok {
		m.ImageToImage = v
	}
}

func specInt(patch map[string]any, key string) int64 {
	switch v := patch[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ChannelsForModel returns enabled channels bound to the model with their
// upstream model names — dispatch order is decided by the caller.
func (s *ChannelService) ChannelsForModel(ctx context.Context, modelID string) ([]repo.ChannelWithUpstream, error) {
	return s.channels.ForModel(ctx, modelID)
}

// ChannelsForModelAnyState includes disabled channels (admin pin tests).
func (s *ChannelService) ChannelsForModelAnyState(ctx context.Context, modelID string) ([]repo.ChannelWithUpstream, error) {
	return s.channels.ForModelAnyState(ctx, modelID)
}

// Decrypt exposes the upstream key for dispatch.
// Get loads one channel row by id (resume path needs the owning channel).
func (s *ChannelService) Get(ctx context.Context, id string) (*model.Channel, error) {
	return s.channels.Get(ctx, id)
}

func (s *ChannelService) Decrypt(ch model.Channel) string { return s.decrypt(ch.APIKeyEnc) }

// Upstream returns the shared protocol client (dispatch needs it for
// UploadRef/Generate/Download; admin handlers for balance probes).
func (s *ChannelService) Upstream() *upstream.Client { return s.up }

// BumpSuccess forwards local stats updates to the repo.
func (s *ChannelService) BumpSuccess(ctx context.Context, channelID string, credits float64) {
	_ = s.channels.BumpSuccess(ctx, channelID, credits)
}

// --- AES-GCM key encryption ---

const encPrefix = "enc:v1:"

func (s *ChannelService) encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return encPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *ChannelService) decrypt(stored string) string {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored // plaintext rows (e.g. seeded by hand)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	if len(raw) < gcm.NonceSize() {
		return ""
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}
