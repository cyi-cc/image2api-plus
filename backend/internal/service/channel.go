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
	up       *upstream.Client
	secret   []byte
}

func NewChannelService(channels *repo.ChannelRepository, models *repo.ModelRepository, up *upstream.Client, secret string) *ChannelService {
	sum := sha256.Sum256([]byte("image2api-channel-keys|" + secret))
	return &ChannelService{channels: channels, models: models, up: up, secret: sum[:]}
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

func (s *ChannelService) Save(ctx context.Context, d SaveChannelDto) (*model.Channel, error) {
	name := strings.TrimSpace(d.Name)
	base := strings.TrimRight(strings.TrimSpace(d.BaseURL), "/")
	if base == "" {
		return nil, errors.New("base_url required")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, errors.New("base_url must start with http:// or https://")
	}
	weight := d.Weight
	if weight < 1 {
		weight = 1
	}
	if d.ID == "" {
		if strings.TrimSpace(d.APIKey) == "" {
			return nil, errors.New("api_key required")
		}
		enc, err := s.encrypt(d.APIKey)
		if err != nil {
			return nil, err
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
		return ch, s.channels.Create(ctx, ch)
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
			return nil, err
		}
		patch["api_key_enc"] = enc
	}
	return s.channels.Update(ctx, d.ID, patch)
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

// UnbindModel detaches one upstream model from a channel; when the local model
// row ends up bound to no channel at all it is deleted too (same orphan
// semantics as deleting a whole channel).
func (s *ChannelService) UnbindModel(ctx context.Context, channelID, upstreamModelID string) error {
	upID := strings.TrimSpace(upstreamModelID)
	if upID == "" {
		return errors.New("model id required")
	}
	m, err := s.channels.UpstreamModelMap(ctx, channelID)
	if err != nil {
		return err
	}
	var localID string
	for mid, up := range m {
		if up == upID {
			localID = mid
			break
		}
	}
	if localID == "" {
		return nil // not bound — idempotent
	}
	if err := s.channels.Unbind(ctx, channelID, localID); err != nil {
		return err
	}
	if rest, _ := s.channels.ChannelIDsByModel(ctx, localID); len(rest) == 0 {
		_, _ = s.models.Delete(ctx, localID)
	}
	return nil
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
		m, gerr := s.models.Get(ctx, id)
		if gerr != nil || m == nil || m.Provider == "channel" {
			continue
		}
		if _, derr := s.models.Delete(ctx, id); derr == nil {
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
	for _, spec := range specs {
		upID := strings.TrimSpace(spec.ID)
		if upID == "" {
			continue
		}
		localID := strings.TrimSpace(spec.Alias)
		if localID == "" {
			localID = upID
		}
		existing, gerr := s.models.Get(ctx, localID)
		if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
			existing = nil
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
				Enabled:   true,
				Ratios:    jsonArray([]string{"16:9"}),
				Prices:    datatypes.JSONMap{},
				CreatedAt: time.Now(),
			}
			applySpec(local, patch)
			if err := s.models.Create(ctx, local); err != nil {
				return fmt.Errorf("model %s: %w", localID, err)
			}
		} else {
			if _, err := s.models.Update(ctx, localID, patch); err != nil {
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
// Prices/enabled/alias are intentionally untouched on update so admin pricing
// survives re-import.
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
		case "fixed":
			patch["durations"] = jsonArray([]string{fmt.Sprintf("%ds", d.Seconds)})
			patch["duration_min"], patch["duration_max"] = int64(0), int64(0)
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
