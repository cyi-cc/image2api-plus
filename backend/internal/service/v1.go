package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"backend/internal/config"
	"backend/internal/model"
	"backend/internal/repo"
	"backend/internal/storage"
	"backend/internal/upstream"
	"backend/internal/util"

	"gorm.io/gorm"
)

var (
	ErrMissingAPIKey     = errors.New("missing api key")
	ErrInvalidAPIKey     = errors.New("invalid api key")
	ErrUnknownModel      = errors.New("unknown model")
	ErrUnsupportedParams = errors.New("unsupported or unpriced parameters for this model")
	ErrBannedPrompt      = errors.New("prompt contains banned content")
	ErrInsufficientFunds = errors.New("insufficient credits")
	ErrGenerationPending = errors.New("generation executor not implemented yet")
	ErrProviderAuth      = errors.New("upstream channel credentials rejected")
	// ErrNoProviderAccount = no enabled channel is bound to the model.
	ErrNoProviderAccount = errors.New("no upstream channel available, please ask an admin to configure one")
	// ErrProviderQuota — kept for handler compatibility; channel failover no
	// longer produces it (quota errors just rotate to the next channel).
	ErrProviderQuota       = errors.New("provider quota exhausted")
	ErrProviderTemporary   = errors.New("upstream temporary unavailable")
	ErrProviderExecution   = errors.New("upstream request failed")
	ErrProviderUnsupported = errors.New("provider not implemented")
	ErrReferenceTooLarge   = errors.New("reference image too large")
	// ErrConcurrencyFull — every eligible channel is busy right now.
	ErrConcurrencyFull = errors.New("all upstream channels are busy, please try again shortly")
	// ErrUserConcurrencyFull — the caller already has their concurrency-group's
	// max generations in flight (画图台 + API key combined). 0 = unlimited.
	ErrUserConcurrencyFull = errors.New("too many generations in progress, please wait for one to finish")
	// ErrVideoJobNotFound / ErrVideoNotReady — /v1 job lookups (kept names).
	ErrVideoJobNotFound = errors.New("job not found")
	ErrVideoNotReady    = errors.New("job result is not ready yet")
)

// maxReferenceImageBytes bounds a single decoded reference blob. 20 MB
// comfortably covers real photos/screenshots/clips; anything larger is almost
// certainly abuse or a mistake.
const maxReferenceImageBytes = 20 * 1024 * 1024

// videoGenBudget caps one video render end-to-end (submit + stream + download).
const videoGenBudget = 30 * time.Minute

// imageGenBudget caps one image generation end-to-end.
const imageGenBudget = 8 * time.Minute

// Channel concurrency semantics: 0 = unlimited (the Lua slot script passes
// max=0 straight through), matching vivid's channel design.

// maxChannelFailover caps how many channels one request may burn on
// retryable/temporary errors before giving up, so an upstream-wide blip can't
// fan a single request out across every channel.
const maxChannelFailover = 6

// PresignTTL is the lifetime of /v1/uploads PUT urls and result GET urls.
const PresignTTL = 30 * time.Minute

type V1Service struct {
	cfg      *config.Config
	models   *repo.ModelRepository
	users    *repo.UserRepository
	events   *repo.EventRepository
	channels *ChannelService
	settings *repo.SiteSettingRepository
	cgroups  *repo.ConcurrencyGroupRepository
	store    *storage.Client
	up       *upstream.Client
	// banned is the admin-managed prompt blocklist (set via SetBannedWords).
	// nil disables the check.
	banned *repo.BannedWordRepository

	// inflight maps an in-progress event ID → the cancel func of its generation
	// work context, so the maintenance sweep can stop a stuck generation the
	// moment it abandons the row.
	inflight *InflightRegistry

	// conc is the Redis-backed concurrency limiter for BOTH the per-channel
	// upstream gate and the per-user gate (画图台 + API key, capped by the
	// user's concurrency group). Self-healing + fail-open.
	conc *ConcurrencyService
}

// chanAcquire takes one per-channel slot (capped at max; 0 = unlimited), tagged
// with the generation's eventID.
func (s *V1Service) chanAcquire(ctx context.Context, channelID, eventID string, max int) bool {
	if max < 0 {
		max = 0
	}
	return s.conc.Acquire(ctx, "conc:c:"+channelID, max, eventID)
}

func (s *V1Service) chanRelease(ctx context.Context, channelID, eventID string) {
	s.conc.Release(ctx, "conc:c:"+channelID, eventID)
}

// userAcquire takes one per-user generation slot, capped by the user's
// concurrency group (0 = unlimited). Returns false when the user is already at
// their limit. `token` is a unique per-generation tag passed back to userRelease.
func (s *V1Service) userAcquire(ctx context.Context, user *model.User, token string) bool {
	if user == nil {
		return true
	}
	return s.conc.Acquire(ctx, "conc:u:"+user.ID, s.userConcurrencyLimit(ctx, user), token)
}

func (s *V1Service) userRelease(ctx context.Context, userID, token string) {
	s.conc.Release(ctx, "conc:u:"+userID, token)
}

// userConcurrencyLimit resolves the user's concurrency-group cap (0 = unlimited),
// falling back to the default group when unset/missing.
func (s *V1Service) userConcurrencyLimit(ctx context.Context, user *model.User) int {
	if s.cgroups == nil || user == nil {
		return 0
	}
	var g *model.ConcurrencyGroup
	if user.ConcurrencyGroupID != "" {
		g, _ = s.cgroups.Get(ctx, user.ConcurrencyGroupID)
	}
	if g == nil {
		g, _ = s.cgroups.GetDefault(ctx)
	}
	if g == nil {
		return 0
	}
	return g.MaxConcurrency
}

// InflightRegistry tracks the cancel func of every in-progress generation by
// event ID. The generation registers on start and removes on finish; the
// maintenance sweep calls Cancel when it gives up on (abandons) an event.
type InflightRegistry struct {
	m sync.Map // eventID -> context.CancelFunc
}

func (r *InflightRegistry) Add(eventID string, cancel context.CancelFunc) {
	if eventID != "" {
		r.m.Store(eventID, cancel)
	}
}

// TryAdd registers an event only when nothing is tracked for it — the resume
// sweep uses it so two ticks can't double-attach to the same upstream job.
func (r *InflightRegistry) TryAdd(eventID string, cancel context.CancelFunc) bool {
	if eventID == "" {
		return false
	}
	_, loaded := r.m.LoadOrStore(eventID, cancel)
	return !loaded
}

// Done deregisters an event (called on normal completion).
func (r *InflightRegistry) Done(eventID string) { r.m.Delete(eventID) }

// Cancel stops an in-flight generation by event ID. Returns true if one was
// running and got cancelled. No-op (false) if it already finished.
func (r *InflightRegistry) Cancel(eventID string) bool {
	if v, ok := r.m.LoadAndDelete(eventID); ok {
		v.(context.CancelFunc)()
		return true
	}
	return false
}

type APIPrincipal struct {
	User      *model.User
	TokenType string
}

type V1ImageRequest struct {
	Model  string
	Prompt string
	Size   string
	// Quality is the vivid-protocol quality label (= resolution tier verbatim,
	// e.g. "1K"/"2K"/"4K"). Only used when Resolution is blank.
	Quality         string
	AspectRatio     string
	Resolution      string
	N               int
	ReferenceImages []string
	// DeAI applies 去AI特征 post-processing (crop / noise / tone jitter +
	// re-encode) to the output and charges the per-tier surcharge on top of
	// the model price. Playground-only; the /v1 path never sets it.
	DeAI bool
	// ChannelID pins an admin test to one specific channel (渠道测试). Ignored
	// on the normal user paths.
	ChannelID string
	// BaseURL is the scheme+host of the inbound request, used to build absolute
	// /images URLs. Empty falls back to a relative path.
	BaseURL string
}

type V1VideoRequest struct {
	Model           string
	Prompt          string
	Duration        string
	AspectRatio     string
	Resolution      string
	ReferenceImages []string
	ReferenceMode   string // "frame", "asset" or "media", overrides model default
	// ChannelID — see V1ImageRequest.ChannelID.
	ChannelID string
	// BaseURL — see V1ImageRequest.BaseURL.
	BaseURL string
}

func NewV1Service(cfg *config.Config, models *repo.ModelRepository, users *repo.UserRepository, events *repo.EventRepository, channels *ChannelService, settings *repo.SiteSettingRepository, cgroups *repo.ConcurrencyGroupRepository, conc *ConcurrencyService, store *storage.Client) *V1Service {
	return &V1Service{
		cfg:      cfg,
		models:   models,
		users:    users,
		events:   events,
		channels: channels,
		settings: settings,
		cgroups:  cgroups,
		conc:     conc,
		store:    store,
		up:       channels.Upstream(),
		inflight: &InflightRegistry{},
	}
}

// Inflight exposes the registry so the maintenance sweep can cancel a stuck
// generation when it abandons that event.
func (s *V1Service) Inflight() *InflightRegistry { return s.inflight }

// SetBannedWords wires the prompt blocklist in after construction.
func (s *V1Service) SetBannedWords(r *repo.BannedWordRepository) { s.banned = r }

// checkBannedPrompt rejects the request when the prompt contains any banned
// word (case-insensitive substring). A hit bumps the word's counter and the
// user's 违禁词触发次数 before rejecting.
func (s *V1Service) checkBannedPrompt(ctx context.Context, principal *APIPrincipal, prompt string) error {
	if s.banned == nil || strings.TrimSpace(prompt) == "" {
		return nil
	}
	words, err := s.banned.List(ctx)
	if err != nil || len(words) == 0 {
		return nil
	}
	lower := strings.ToLower(prompt)
	for _, w := range words {
		term := strings.ToLower(strings.TrimSpace(w.Word))
		if term == "" || !strings.Contains(lower, term) {
			continue
		}
		userID, userName := "", ""
		if principal != nil && principal.User != nil {
			userID = principal.User.ID
			userName = principal.User.Name
			if userName == "" {
				userName = principal.User.Email
			}
		}
		s.banned.RecordHit(ctx, w.ID, w.Word, userID, userName, prompt)
		return fmt.Errorf("%w: banned word \"%s\"", ErrBannedPrompt, w.Word)
	}
	return nil
}

// logRejectedEvent records a request rejected BEFORE the pending event exists
// (banned word, concurrency full, unknown model, insufficient credits…) as a
// failed event, so every attempt shows up in the logs.
func (s *V1Service) logRejectedEvent(ctx context.Context, kind, modelID string, principal *APIPrincipal, prompt, source, reason string) {
	event := &model.EventLog{
		ID:        "evt-" + randomUpper(12),
		TS:        time.Now(),
		Kind:      kind,
		Status:    "failed",
		Model:     strings.TrimSpace(modelID),
		Prompt:    prompt,
		Source:    source,
		Error:     reason,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if m, err := s.models.Get(ctx, event.Model); err == nil {
		event.Model = m.ID
		event.Provider = m.Provider
	}
	if principal != nil && principal.User != nil {
		event.UserID = principal.User.ID
	}
	_ = s.events.Create(ctx, event)
}

func (s *V1Service) Authenticate(ctx context.Context, authHeader string) (*APIPrincipal, error) {
	token := ParseBearer(authHeader)
	if token == "" {
		return nil, ErrMissingAPIKey
	}
	user, err := s.users.GetByAPIKeyHash(ctx, HashAPIKey(token))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}
	if user.Status != "active" {
		return nil, ErrInvalidAPIKey
	}
	_ = s.users.TouchAPIKeyUsage(ctx, HashAPIKey(token))
	return &APIPrincipal{
		User:      user,
		TokenType: "user",
	}, nil
}

// ===== /v1 public protocol (vivid-compatible) =====

// V1ModelItem is one GET /v1/models entry — identical shape to a vivid
// upstream's, which is what makes channel model-import work symmetrically.
type V1ModelItem struct {
	ID              string      `json:"id"`
	Kind            string      `json:"kind"`
	Qualities       []string    `json:"qualities"`
	Duration        *V1ModelDur `json:"duration,omitempty"`
	RefImages       int64       `json:"refImages"`
	RefVideos       int64       `json:"refVideos"`
	RefAudios       int64       `json:"refAudios"`
	RefVideoSeconds int64       `json:"refVideoSeconds,omitempty"`
	RefAudioSeconds int64       `json:"refAudioSeconds,omitempty"`
}

// V1ModelDur exposes a video model's duration capability: options=discrete
// list (single-choice models too — "fixed" is never emitted), range=continuous
// interval.
type V1ModelDur struct {
	Mode    string  `json:"mode"`
	Seconds int64   `json:"seconds,omitempty"`
	Min     int64   `json:"min,omitempty"`
	Max     int64   `json:"max,omitempty"`
	Options []int64 `json:"options,omitempty"`
}

// SellableModels — GET /v1/models. 配价即上架（无启停开关）：绑定到至少一个
// enabled 渠道 AND 全部档位有价才是可售模型（缺价的档位提交时会扣费拒绝，
// 直接不下发）。kind 不限——image/video/text 同构透出，下游才能连导。
func (s *V1Service) SellableModels(ctx context.Context) ([]V1ModelItem, error) {
	items, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]V1ModelItem, 0, len(items))
	for _, item := range items {
		if !s.modelSellable(ctx, &item) {
			continue
		}
		qualities := repo.JSONStrings(item.Resolutions)
		if len(qualities) == 0 {
			// Resolution list unset → advertise the priced tiers.
			for k := range item.Prices {
				qualities = append(qualities, k)
			}
		}
		entry := V1ModelItem{
			ID:              item.EffectiveName(),
			Kind:            item.Type,
			Qualities:       qualities,
			RefImages:       int64(item.MaxReferenceImages),
			RefVideos:       int64(item.MaxReferenceVideos),
			RefAudios:       int64(item.MaxReferenceAudios),
			RefVideoSeconds: int64(item.MaxRefVideoSeconds),
			RefAudioSeconds: int64(item.MaxRefAudioSeconds),
		}
		if entry.Qualities == nil {
			entry.Qualities = []string{}
		}
		if item.Type == "video" {
			entry.Duration = modelDurationSpec(&item)
		}
		out = append(out, entry)
	}
	return out, nil
}

// modelSellable checks binding + pricing so /v1/models only advertises what a
// generate call can actually fulfil.
func (s *V1Service) modelSellable(ctx context.Context, item *model.ModelConfig) bool {
	channels, err := s.channels.ChannelsForModel(ctx, item.ID)
	if err != nil || len(channels) == 0 {
		return false
	}
	return ModelFullyPriced(item)
}

// ModelFullyPriced — 上架门槛（无启停开关，配价即上架）：每个清晰度档位在
// Prices 里有价；视频模型还要求时长有价（离散档逐档，或区间档按秒价）。
func ModelFullyPriced(item *model.ModelConfig) bool {
	for _, res := range repo.JSONStrings(item.Resolutions) {
		if _, ok := jsonMapFloat(item.Prices, res); !ok {
			return false
		}
	}
	if item.Type == "video" {
		durs := repo.JSONStrings(item.Durations)
		if len(durs) > 0 {
			for _, d := range durs {
				if _, ok := jsonMapFloat(item.DurationPrices, d); !ok {
					return false
				}
			}
		} else if item.DurationMin > 0 || item.DurationMax > 0 {
			if _, ok := jsonMapFloat(item.DurationPrices, "per_second"); !ok {
				return false
			}
		}
	}
	return len(repo.JSONStrings(item.Resolutions)) > 0 || len(item.Prices) > 0
}

func modelDurationSpec(item *model.ModelConfig) *V1ModelDur {
	if item.DurationMin > 0 || item.DurationMax > 0 {
		return &V1ModelDur{Mode: "range", Min: int64(item.DurationMin), Max: int64(item.DurationMax)}
	}
	opts := repo.JSONStrings(item.Durations)
	if len(opts) == 0 {
		return nil
	}
	vals := make([]int64, 0, len(opts))
	for _, d := range opts {
		if n := parseDurationSeconds(d); n > 0 {
			vals = append(vals, int64(n))
		}
	}
	if len(vals) == 0 {
		return nil
	}
	return &V1ModelDur{Mode: "options", Options: vals}
}

// UserBalance — GET /v1/balance. Re-reads the user row so the number is live.
func (s *V1Service) UserBalance(ctx context.Context, principal *APIPrincipal) (float64, error) {
	user := principal.User
	if fresh, err := s.users.GetByID(ctx, user.ID); err == nil && fresh != nil {
		user = fresh
	}
	return user.Credits, nil
}

// ===== channel dispatch =====

// generationParams bundles the wire fields one generate call carries.
type generationParams struct {
	Kind         string // "image" | "video"
	Prompt       string
	Ratio        string
	Quality      string // resolution tier verbatim
	DurationSecs int64
	Refs         [][]byte
	// PinChannelID forces this one channel (admin test) — bypasses the enabled
	// filter, weighted order and failover.
	PinChannelID string
}

// genResult is what a successful dispatch produces.
type genResult struct {
	Data        []byte
	ContentType string
	ChannelID   string
	ChannelName string
	UpstreamJob string
	Credits     float64 // upstream-reported spend
}

// dispatchGenerate routes one generation through the channel pool bound to the
// model: weighted-random order, per-channel concurrency slot, refs re-uploaded
// per attempt (upload sessions are channel-local), failover on auth/temporary
// errors. Deterministic errors (invalid params / failed job) are terminal.
func (s *V1Service) dispatchGenerate(ctx context.Context, eventID string, modelItem *model.ModelConfig, p generationParams) (*genResult, error) {
	var channels []repo.ChannelWithUpstream
	var err error
	if pin := strings.TrimSpace(p.PinChannelID); pin != "" {
		// Admin test pin: single channel, disabled included, no failover.
		channels, err = s.channels.ChannelsForModelAnyState(ctx, modelItem.ID)
		if err != nil {
			return nil, err
		}
		kept := channels[:0]
		for _, c := range channels {
			if c.Channel.ID == pin {
				kept = append(kept, c)
			}
		}
		channels = kept
	} else {
		channels, err = s.channels.ChannelsForModel(ctx, modelItem.ID)
		if err != nil {
			return nil, err
		}
	}
	if len(channels) == 0 {
		return nil, ErrNoProviderAccount
	}
	var lastErr error
	failovers := 0
	for _, cand := range weightedChannelOrder(channels) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		ch := cand.Channel
		if !s.chanAcquire(ctx, ch.ID, eventID, ch.Concurrency) {
			continue // channel at capacity — try the next one
		}
		res, attemptErr := s.generateOnChannel(ctx, eventID, ch, cand.UpstreamModel, p)
		s.chanRelease(ctx, ch.ID, eventID)
		if attemptErr == nil {
			return res, nil
		}
		lastErr = attemptErr
		if !upstream.Failoverable(attemptErr) {
			return nil, upstreamUserError(attemptErr)
		}
		if errors.Is(attemptErr, upstream.ErrAuth) {
			continue // bad credentials don't burn the failover budget
		}
		failovers++
		if failovers >= maxChannelFailover {
			break
		}
	}
	if lastErr == nil {
		return nil, ErrConcurrencyFull
	}
	return nil, fmt.Errorf("%w: %v", ErrProviderTemporary, upstreamUserError(lastErr))
}

// generateOnChannel performs one full attempt on a single channel: upload refs
// → POST /v1/generate (NDJSON, jobId reconnect) → download result bytes.
func (s *V1Service) generateOnChannel(ctx context.Context, eventID string, ch model.Channel, upstreamModel string, p generationParams) (*genResult, error) {
	apiKey := s.channels.Decrypt(ch)
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: empty key", upstream.ErrAuth)
	}
	base := strings.TrimRight(ch.BaseURL, "/")
	refKeys := make([]string, 0, len(p.Refs))
	for _, blob := range p.Refs {
		key, err := s.up.UploadRef(ctx, base, apiKey, blob)
		if err != nil {
			return nil, err
		}
		refKeys = append(refKeys, key)
	}
	wait := int64(imageGenBudget / time.Second)
	if p.Kind == "video" {
		wait = int64(videoGenBudget / time.Second)
	}
	var upstreamJob string
	resultURL, credits, err := s.up.Generate(ctx, base, apiKey, upstream.GenParams{
		Model:    upstreamModel,
		Prompt:   p.Prompt,
		Quality:  p.Quality,
		Ratio:    p.Ratio,
		Duration: p.DurationSecs,
		Refs:     refKeys,
	}, wait, func(job string) {
		if upstreamJob == "" {
			upstreamJob = job
			_ = s.events.SetUpstreamJob(ctx, eventID, job)
			// Stamp the owning channel the moment the upstream accepts the job —
			// a restart between submit and finish would otherwise leave a
			// pending row that can't tell which channel to resume on.
			_ = s.events.SetAccount(ctx, eventID, ch.ID, ch.Name)
		}
	})
	if err != nil {
		return nil, err
	}
	data, ct, err := s.up.Download(ctx, resultURL)
	if err != nil {
		return nil, err
	}
	s.channels.BumpSuccess(ctx, ch.ID, float64(credits))
	_ = s.events.SetAccount(ctx, eventID, ch.ID, ch.Name)
	return &genResult{
		Data:        data,
		ContentType: ct,
		ChannelID:   ch.ID,
		ChannelName: ch.Name,
		UpstreamJob: upstreamJob,
		Credits:     float64(credits),
	}, nil
}

// weightedChannelOrder builds a weighted-random permutation: each pick draws a
// channel with probability weight/remainingTotal (weight 2 → ~2x first-pick
// rate of weight 1). Ported from vivid's job executor.
func weightedChannelOrder(channels []repo.ChannelWithUpstream) []repo.ChannelWithUpstream {
	if len(channels) <= 1 {
		return channels
	}
	rest := append([]repo.ChannelWithUpstream(nil), channels...)
	out := make([]repo.ChannelWithUpstream, 0, len(rest))
	for len(rest) > 0 {
		total := 0
		for _, c := range rest {
			w := c.Channel.Weight
			if w < 1 {
				w = 1
			}
			total += w
		}
		r := rand.Intn(total)
		for i, c := range rest {
			w := c.Channel.Weight
			if w < 1 {
				w = 1
			}
			if r < w {
				out = append(out, c)
				rest = append(rest[:i], rest[i+1:]...)
				break
			}
			r -= w
		}
	}
	return out
}

// upstreamUserError maps an upstream error to what the caller sees. failed 帧
// 文案是上游给用户看的中文，透传；4001 参数错同样透传；其余归一为通用文案。
func upstreamUserError(err error) error {
	if err == nil {
		return ErrProviderTemporary
	}
	msg := err.Error()
	for _, prefix := range []string{
		upstream.ErrJobFailed.Error() + ": ",
		upstream.ErrInvalid.Error() + ": ",
		upstream.ErrAuth.Error() + ": ",
		upstream.ErrRetryable.Error() + ": ",
		upstream.ErrAmbiguous.Error() + ": ",
	} {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, prefix))
	}
	switch upstream.KindOf(err) {
	case upstream.KindJobFailed, upstream.KindInvalid:
		if msg == "" {
			return ErrProviderExecution
		}
		return fmt.Errorf("%w: %s", ErrProviderExecution, msg)
	default:
		if msg == "" {
			return ErrProviderTemporary
		}
		return fmt.Errorf("%w: %s", ErrProviderTemporary, msg)
	}
}

// ===== session / admin-test generation (unchanged response shapes) =====

func (s *V1Service) prepareSessionImage(ctx context.Context, principal *APIPrincipal, in V1ImageRequest) (map[string]any, error) {
	return s.prepareImageExecution(ctx, principal, in, "user", true)
}

func (s *V1Service) prepareAdminTestImage(ctx context.Context, principal *APIPrincipal, in V1ImageRequest) (map[string]any, error) {
	return s.prepareImageExecution(ctx, principal, in, "admin", false)
}

func (s *V1Service) prepareImageExecution(ctx context.Context, principal *APIPrincipal, in V1ImageRequest, source string, charge bool) (map[string]any, error) {
	// Detach the whole execution from the request lifecycle. The frontend tracks
	// progress by polling /jobs/mine, so a client disconnect must NOT cancel an
	// in-flight generation or drop the refund/final-status bookkeeping.
	ctx = context.WithoutCancel(ctx)
	if source != "admin" {
		if err := s.checkBannedPrompt(ctx, principal, in.Prompt); err != nil {
			s.logRejectedEvent(ctx, "image", in.Model, principal, in.Prompt, source, err.Error())
			return nil, err
		}
	}
	if in.DeAI && !s.deaiEnabled(ctx) {
		in.DeAI = false
	}
	genCtx, cancel := context.WithTimeout(ctx, imageGenBudget)
	defer cancel()

	if source != "admin" && principal != nil && principal.User != nil {
		slot := randomUpper(12)
		if !s.userAcquire(ctx, principal.User, slot) {
			s.logRejectedEvent(ctx, "image", in.Model, principal, in.Prompt, source, ErrUserConcurrencyFull.Error())
			return nil, ErrUserConcurrencyFull
		}
		defer s.userRelease(ctx, principal.User.ID, slot)
	}

	modelItem, resolution, aspectRatio, price, err := s.prepareImage(ctx, principal, in, charge)
	if err != nil {
		s.logRejectedEvent(ctx, "image", in.Model, principal, in.Prompt, source, err.Error())
		return nil, err
	}
	refCount := len(in.ReferenceImages)
	fileURL, relativePath := s.allocateOutput(principal, "png", in.BaseURL)
	eventID, err := s.logPendingEvent(ctx, "image", modelItem, principal, in.Prompt, aspectRatio, resolution, "", refCount, price, relativePath, source, nil, in.DeAI)
	if err != nil {
		return nil, err
	}
	s.inflight.Add(eventID, cancel)
	defer s.inflight.Done(eventID)
	startedAt := time.Now()

	refs, err := decodeReferenceImages(in.ReferenceImages, max(totalRefLimit(modelItem), 1))
	if err != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", err.Error(), 0)
		return nil, err
	}
	res, execErr := s.dispatchGenerate(genCtx, eventID, modelItem, generationParams{
		Kind: "image", Prompt: in.Prompt, Ratio: aspectRatio, Quality: resolution, Refs: refs,
		PinChannelID: in.ChannelID,
	})
	if execErr != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", execErr.Error(), 0)
		return nil, execErr
	}
	imageBytes := res.Data
	if in.DeAI {
		if processed, derr := applyDeAI(imageBytes); derr == nil {
			imageBytes = processed
		}
	}
	if err := s.store.Put(genCtx, relativePath, imageBytes, "image/png"); err != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", "storage upload failed: "+err.Error(), 0)
		return nil, fmt.Errorf("%w: %v", ErrProviderExecution, err)
	}
	if thumb, terr := makeThumbnail(imageBytes); terr == nil {
		_ = s.store.Put(genCtx, ThumbKey(relativePath), thumb, "image/jpeg")
	}
	elapsedMS := int(time.Since(startedAt).Milliseconds())
	if err := s.events.UpdateStatus(ctx, eventID, "success", "", elapsedMS); err != nil {
		return nil, err
	}
	_ = s.models.IncrementGenerationCount(ctx, modelItem.ID)
	if principal != nil && principal.User != nil {
		_ = s.users.IncrementGenerationCount(ctx, principal.User.ID)
	}
	if charge {
		_ = s.maybeGrantInviteReward(ctx, principal)
	}
	return map[string]any{
		"created":    time.Now().Unix(),
		"data":       []map[string]any{{"url": fileURL, "b64_json": nil}},
		"model":      modelItem.EffectiveName(),
		"provider":   res.ChannelName,
		"kind":       "image",
		"url":        fileURL,
		"elapsed_ms": elapsedMS,
		"charged":    price,
		"credits":    principalCredits(principal),
	}, nil
}

func (s *V1Service) prepareSessionVideo(ctx context.Context, principal *APIPrincipal, in V1VideoRequest) (map[string]any, error) {
	return s.prepareVideoExecution(ctx, principal, in, "user", true)
}

func (s *V1Service) prepareAdminTestVideo(ctx context.Context, principal *APIPrincipal, in V1VideoRequest) (map[string]any, error) {
	return s.prepareVideoExecution(ctx, principal, in, "admin", false)
}

func (s *V1Service) prepareVideoExecution(ctx context.Context, principal *APIPrincipal, in V1VideoRequest, source string, charge bool) (map[string]any, error) {
	ctx = context.WithoutCancel(ctx)
	if source != "admin" {
		if err := s.checkBannedPrompt(ctx, principal, in.Prompt); err != nil {
			s.logRejectedEvent(ctx, "video", in.Model, principal, in.Prompt, source, err.Error())
			return nil, err
		}
	}
	genCtx, cancel := context.WithTimeout(ctx, videoGenBudget)
	defer cancel()

	if source != "admin" && principal != nil && principal.User != nil {
		slot := randomUpper(12)
		if !s.userAcquire(ctx, principal.User, slot) {
			s.logRejectedEvent(ctx, "video", in.Model, principal, in.Prompt, source, ErrUserConcurrencyFull.Error())
			return nil, ErrUserConcurrencyFull
		}
		defer s.userRelease(ctx, principal.User.ID, slot)
	}

	modelItem, resolution, aspectRatio, duration, price, err := s.prepareVideo(ctx, principal, in, charge)
	if err != nil {
		s.logRejectedEvent(ctx, "video", in.Model, principal, in.Prompt, source, err.Error())
		return nil, err
	}
	refCount := len(in.ReferenceImages)
	fileURL, relativePath := s.allocateOutput(principal, "mp4", in.BaseURL)
	eventID, err := s.logPendingEvent(ctx, "video", modelItem, principal, in.Prompt, aspectRatio, resolution, duration, refCount, price, relativePath, source, nil, false)
	if err != nil {
		return nil, err
	}
	s.inflight.Add(eventID, cancel)
	defer s.inflight.Done(eventID)
	startedAt := time.Now()

	refs, err := decodeReferenceImages(in.ReferenceImages, max(totalRefLimit(modelItem), 1))
	if err != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", err.Error(), 0)
		return nil, err
	}
	res, execErr := s.dispatchGenerate(genCtx, eventID, modelItem, generationParams{
		Kind: "video", Prompt: in.Prompt, Ratio: aspectRatio, Quality: resolution,
		DurationSecs: int64(parseDurationSeconds(duration)), Refs: refs,
		PinChannelID: in.ChannelID,
	})
	if execErr != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", execErr.Error(), 0)
		return nil, execErr
	}
	videoBytes := res.Data
	if err := s.store.Put(genCtx, relativePath, videoBytes, "video/mp4"); err != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", "storage upload failed: "+err.Error(), 0)
		return nil, fmt.Errorf("%w: %v", ErrProviderExecution, err)
	}
	if thumb, last, terr := extractVideoFrames(genCtx, videoBytes); terr == nil {
		if len(thumb) > 0 {
			_ = s.store.Put(genCtx, ThumbKey(relativePath), thumb, "image/jpeg")
		}
		if len(last) > 0 {
			_ = s.store.Put(genCtx, LastFrameKey(relativePath), last, "image/jpeg")
		}
	}
	elapsedMS := int(time.Since(startedAt).Milliseconds())
	if err := s.events.UpdateStatus(ctx, eventID, "success", "", elapsedMS); err != nil {
		return nil, err
	}
	_ = s.models.IncrementGenerationCount(ctx, modelItem.ID)
	if principal != nil && principal.User != nil {
		_ = s.users.IncrementGenerationCount(ctx, principal.User.ID)
	}
	if charge {
		_ = s.maybeGrantInviteReward(ctx, principal)
	}
	return map[string]any{
		"created":    time.Now().Unix(),
		"data":       []map[string]any{{"url": fileURL}},
		"model":      modelItem.EffectiveName(),
		"provider":   res.ChannelName,
		"kind":       "video",
		"url":        fileURL,
		"elapsed_ms": elapsedMS,
		"charged":    price,
		"credits":    principalCredits(principal),
	}, nil
}

// ===== /v1/generate jobs (vivid protocol) =====

// V1JobRequest is the POST /v1/generate body.
type V1JobRequest struct {
	Model    string  `json:"model"`
	Prompt   string  `json:"prompt"`
	Quality  string  `json:"quality"`
	Ratio    *string `json:"ratio"`
	Duration *int64  `json:"duration"`
	// Refs are object keys issued by /v1/uploads.
	Refs []string `json:"refs"`
	// JobId reconnect mode: re-attach to an existing job, never creates one.
	JobID *string `json:"jobId"`
}

// CreateJob validates + charges + creates the pending event and kicks the
// background render; returns the event row (its ID is the jobId).
func (s *V1Service) CreateJob(ctx context.Context, principal *APIPrincipal, in V1JobRequest) (*model.EventLog, error) {
	ctx = context.WithoutCancel(ctx)
	if err := s.checkBannedPrompt(ctx, principal, in.Prompt); err != nil {
		s.logRejectedEvent(ctx, "", in.Model, principal, in.Prompt, "v1", err.Error())
		return nil, err
	}
	modelItem, err := s.models.Get(ctx, strings.TrimSpace(in.Model))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUnknownModel
		}
		return nil, err
	}
	kind := modelItem.Type
	if kind != "image" && kind != "video" {
		return nil, ErrUnknownModel
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, fmt.Errorf("%w: prompt required", ErrUnsupportedParams)
	}
	quality := strings.TrimSpace(in.Quality)
	if quality == "" {
		return nil, fmt.Errorf("%w: quality required", ErrUnsupportedParams)
	}
	if res := repo.JSONStrings(modelItem.Resolutions); len(res) > 0 && !strSliceContains(res, quality) {
		return nil, fmt.Errorf("%w: quality %q not in %v", ErrUnsupportedParams, quality, res)
	}
	ratio := ""
	if in.Ratio != nil {
		ratio = snapRatio(strings.TrimSpace(strings.ReplaceAll(*in.Ratio, "x", ":")), repo.JSONStrings(modelItem.Ratios))
	}
	var durSecs int64
	duration := ""
	if kind == "video" {
		if in.Duration == nil || *in.Duration <= 0 {
			return nil, fmt.Errorf("%w: duration required", ErrUnsupportedParams)
		}
		durSecs = *in.Duration
		if err := validateModelDuration(modelItem, durSecs); err != nil {
			return nil, err
		}
		duration = fmt.Sprintf("%ds", durSecs)
	}
	// Refs: keys must live under the caller's refs/ dir; bytes are fetched now
	// (existence + caps + duration checks) and re-uploaded to whatever channel
	// wins the dispatch.
	refBlobs, refKeys, err := s.resolveJobRefs(ctx, principal, in.Refs, modelItem)
	if err != nil {
		s.logRejectedEvent(ctx, kind, modelItem.ID, principal, in.Prompt, "v1", err.Error())
		return nil, err
	}
	// The job runs async — acquire the per-user slot now and release it when the
	// job settles, so the slot survives past this HTTP request.
	slot := randomUpper(12)
	if principal != nil && principal.User != nil && !s.userAcquire(ctx, principal.User, slot) {
		s.logRejectedEvent(ctx, kind, modelItem.ID, principal, in.Prompt, "v1", ErrUserConcurrencyFull.Error())
		return nil, ErrUserConcurrencyFull
	}
	price, err := s.chargeForModel(ctx, principal, modelItem, kind, quality, duration, 0, true)
	if err != nil {
		s.userRelease(ctx, principal.User.ID, slot)
		s.logRejectedEvent(ctx, kind, modelItem.ID, principal, in.Prompt, "v1", err.Error())
		return nil, err
	}
	eventID, err := s.logPendingEvent(ctx, kind, modelItem, principal, in.Prompt, ratio, quality, duration, len(refBlobs), price, "", "v1", nil, false)
	if err != nil {
		s.userRelease(ctx, principal.User.ID, slot)
		_, _ = s.users.RefundCredits(ctx, principal.User.ID, price)
		return nil, err
	}
	go s.runV1Job(ctx, principal, eventID, modelItem, generationParams{
		Kind: kind, Prompt: in.Prompt, Ratio: ratio, Quality: quality,
		DurationSecs: durSecs, Refs: refBlobs,
	}, refKeys, slot, price)
	return s.events.GetByID(ctx, eventID)
}

// JobForUser reloads a job scoped to the caller (reconnect mode + wait loop).
func (s *V1Service) JobForUser(ctx context.Context, principal *APIPrincipal, jobID string) (*model.EventLog, error) {
	ev, err := s.events.GetByID(ctx, strings.TrimSpace(jobID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoJobNotFound
		}
		return nil, err
	}
	if ev == nil || (principal != nil && principal.User != nil && ev.UserID != principal.User.ID) {
		return nil, ErrVideoJobNotFound
	}
	return ev, nil
}

// JobStatus maps an event to the vivid wire status.
func JobStatus(ev *model.EventLog) string {
	switch ev.Status {
	case "success":
		return "succeeded"
	case "failed":
		return "failed"
	default:
		if strings.TrimSpace(ev.AccountID) != "" {
			return "running"
		}
		return "queued"
	}
}

// JobResultURL presigns the stored result for GET (30 min). API-key results
// expire: the URL is only minted within 30 minutes of completion.
func (s *V1Service) JobResultURL(ev *model.EventLog) string {
	if ev == nil || ev.Status != "success" || strings.TrimSpace(ev.File) == "" {
		return ""
	}
	if s.store == nil || !s.store.Configured() {
		return ""
	}
	if time.Since(ev.UpdatedAt) > PresignTTL {
		return ""
	}
	url, err := s.store.PresignGet(ev.File, PresignTTL)
	if err != nil {
		return ""
	}
	return url
}

// runV1Job executes a /v1 job in the background: dispatch → store under tmp/ →
// final event state. Caller polls the row; the NDJSON stream just re-reads it.
// `slot` is the per-user concurrency token acquired in CreateJob.
func (s *V1Service) runV1Job(ctx context.Context, principal *APIPrincipal, eventID string, modelItem *model.ModelConfig, p generationParams, refKeys []string, slot string, price float64) {
	budget := imageGenBudget
	if p.Kind == "video" {
		budget = videoGenBudget
	}
	genCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	s.inflight.Add(eventID, cancel)
	defer s.inflight.Done(eventID)
	if principal != nil && principal.User != nil {
		defer s.userRelease(ctx, principal.User.ID, slot)
	}
	// Reference uploads are session-scoped on OUR side too — drop them once the
	// job reaches a terminal state.
	defer func() {
		for _, k := range refKeys {
			_ = s.store.Delete(context.Background(), k)
		}
	}()
	startedAt := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[v1] panic in runV1Job event=%s model=%s: %v\n%s", eventID, modelItem.Name, r, debug.Stack())
			_ = s.refundIfNeeded(ctx, principal, eventID, price)
			_ = s.events.UpdateStatus(ctx, eventID, "failed", fmt.Sprintf("internal error: %v", r), 0)
		}
	}()

	res, execErr := s.dispatchGenerate(genCtx, eventID, modelItem, p)
	if execErr != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", execErr.Error(), 0)
		return
	}
	ext := resultExt(res.ContentType, p.Kind)
	rel := filepath.ToSlash(filepath.Join("tmp", eventID+"."+ext))
	if err := s.store.Put(genCtx, rel, res.Data, res.ContentType); err != nil {
		_ = s.refundIfNeeded(ctx, principal, eventID, price)
		_ = s.events.UpdateStatus(ctx, eventID, "failed", "storage upload failed: "+err.Error(), 0)
		return
	}
	_ = s.events.SetFile(ctx, eventID, rel)
	if err := s.events.UpdateStatus(ctx, eventID, "success", "", int(time.Since(startedAt).Milliseconds())); err != nil {
		return
	}
	_ = s.models.IncrementGenerationCount(ctx, modelItem.ID)
	if principal != nil && principal.User != nil {
		_ = s.users.IncrementGenerationCount(ctx, principal.User.ID)
	}
	_ = s.maybeGrantInviteReward(ctx, principal)
}

// ResumeStaleJob re-attaches to an upstream job whose local goroutine died with
// a process restart. Called by the maintenance sweep; TryAdd guards against
// double-attach across ticks. Terminal failure (upstream job lost/failed,
// channel gone or key broken) fails the event and refunds; transient failure
// leaves it pending for the next sweep (events older than maxResumeAge get
// purged instead of resumed).
func (s *V1Service) ResumeStaleJob(ctx context.Context, ev repo.StaleEvent) {
	if ev.UpstreamJob == "" || ev.AccountID == "" {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), videoGenBudget)
	if !s.inflight.TryAdd(ev.ID, cancel) {
		cancel()
		return
	}
	go s.resumeUpstreamJob(rctx, ev)
}

func (s *V1Service) resumeUpstreamJob(ctx context.Context, ev repo.StaleEvent) {
	defer s.inflight.Done(ev.ID)
	fail := func(msg string) {
		s.refundStale(ctx, ev)
		elapsed := int(time.Since(ev.TS).Milliseconds())
		_ = s.events.UpdateStatus(ctx, ev.ID, "failed", msg, elapsed)
	}
	ch, err := s.channels.Get(ctx, ev.AccountID)
	if err != nil || ch == nil {
		fail("上游渠道已删除，任务无法续等")
		return
	}
	apiKey := s.channels.Decrypt(*ch)
	if apiKey == "" {
		fail("上游渠道密钥不可用，任务无法续等")
		return
	}
	base := strings.TrimRight(ch.BaseURL, "/")
	resultURL, credits, err := s.up.Generate(ctx, base, apiKey, upstream.GenParams{JobID: ev.UpstreamJob}, int64(videoGenBudget/time.Second), nil)
	if err != nil {
		switch upstream.KindOf(err) {
		case upstream.KindJobFailed, upstream.KindInvalid, upstream.KindAuth:
			fail(upstreamUserError(err).Error())
		default:
			// Retryable/transport/timeout — the upstream job may still run;
			// leave the event pending for the next sweep.
			log.Printf("[v1] resume upstream job %s (event %s) retry later: %v", ev.UpstreamJob, ev.ID, err)
		}
		return
	}
	data, ct, err := s.up.Download(ctx, resultURL)
	if err != nil {
		log.Printf("[v1] resume download %s (event %s) retry later: %v", ev.UpstreamJob, ev.ID, err)
		return
	}
	rel := strings.TrimSpace(ev.File)
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join("tmp", ev.ID+"."+resultExt(ct, ev.Kind)))
	}
	if err := s.store.Put(ctx, rel, data, ct); err != nil {
		fail("storage upload failed: " + err.Error())
		return
	}
	_ = s.events.SetFile(ctx, ev.ID, rel)
	if err := s.events.UpdateStatus(ctx, ev.ID, "success", "", int(time.Since(ev.TS).Milliseconds())); err != nil {
		return
	}
	s.channels.BumpSuccess(ctx, ch.ID, float64(credits))
	if ev.Model != "" {
		_ = s.models.IncrementGenerationCount(ctx, ev.Model)
	}
	if ev.UserID != "" {
		_ = s.users.IncrementGenerationCount(ctx, ev.UserID)
	}
	log.Printf("[v1] resumed upstream job %s → event %s succeeded", ev.UpstreamJob, ev.ID)
}

// refundStale returns the up-front charge for an abandoned/resume-dead event,
// claimed exactly once via the refunded flag.
func (s *V1Service) refundStale(ctx context.Context, ev repo.StaleEvent) {
	if ev.UserID == "" || ev.Cost <= 0 {
		return
	}
	claimed, err := s.events.MarkRefunded(ctx, ev.ID)
	if err != nil || !claimed {
		return
	}
	if _, err := s.users.RefundCredits(ctx, ev.UserID, ev.Cost); err != nil {
		log.Printf("[v1] refund stale event %s (user %s, %.0f): %v", ev.ID, ev.UserID, ev.Cost, err)
	}
}

func resultExt(contentType, kind string) string {
	switch {
	case strings.Contains(contentType, "webp"):
		return "webp"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return "jpg"
	case strings.Contains(contentType, "gif"):
		return "gif"
	case strings.Contains(contentType, "webm"):
		return "webm"
	case strings.Contains(contentType, "mp4"):
		return "mp4"
	case strings.Contains(contentType, "quicktime"):
		return "mov"
	}
	if kind == "video" {
		return "mp4"
	}
	return "png"
}

// validateModelDuration enforces the model's duration spec (options list or
// min/max range) on a /v1 request.
func validateModelDuration(item *model.ModelConfig, secs int64) error {
	if item.DurationMin > 0 || item.DurationMax > 0 {
		if item.DurationMin > 0 && secs < int64(item.DurationMin) ||
			item.DurationMax > 0 && secs > int64(item.DurationMax) {
			return fmt.Errorf("%w: duration %ds out of range [%d-%d]", ErrUnsupportedParams, secs, item.DurationMin, item.DurationMax)
		}
		return nil
	}
	if durs := repo.JSONStrings(item.Durations); len(durs) > 0 {
		for _, d := range durs {
			if int64(parseDurationSeconds(d)) == secs {
				return nil
			}
		}
		return fmt.Errorf("%w: duration %ds not in %v", ErrUnsupportedParams, secs, durs)
	}
	return nil
}

// resolveJobRefs fetches + validates /v1 ref keys. Returns blobs (for channel
// upload) and the raw keys (for cleanup on job completion).
func (s *V1Service) resolveJobRefs(ctx context.Context, principal *APIPrincipal, keys []string, modelItem *model.ModelConfig) ([][]byte, []string, error) {
	if len(keys) == 0 {
		return nil, nil, nil
	}
	if principal == nil || principal.User == nil {
		return nil, nil, ErrInvalidAPIKey
	}
	uid := principal.User.ID
	total := len(keys)
	if cap := totalRefLimit(modelItem); cap > 0 && total > cap {
		return nil, nil, fmt.Errorf("%w: too many reference files", ErrUnsupportedParams)
	}
	blobs := make([][]byte, 0, total)
	counts := map[string]int{}
	secSum := map[string]float64{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if !validRefKey(uid, key) {
			return nil, nil, fmt.Errorf("%w: 参考素材无效", ErrUnsupportedParams)
		}
		data, err := s.getObjectBytes(ctx, key)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: 参考素材已过期或不存在", ErrUnsupportedParams)
		}
		kind, mime := upstream.RefKindMime(data)
		if kind == "" {
			return nil, nil, fmt.Errorf("%w: 参考素材格式不支持", ErrUnsupportedParams)
		}
		counts[kind]++
		limit := refLimitFor(modelItem, kind)
		if counts[kind] > limit {
			return nil, nil, fmt.Errorf("%w: 参考%s最多 %d 个", ErrUnsupportedParams, refKindLabel(kind), limit)
		}
		if kind == "video" || kind == "audio" {
			if sec, ok := util.MediaDurationSeconds(mime, data[:minInt(len(data), 256<<10)]); ok {
				secSum[kind] += sec
			}
		}
		blobs = append(blobs, data)
	}
	if lim := modelItem.MaxRefVideoSeconds; lim > 0 && secSum["video"] > float64(lim) {
		return nil, nil, fmt.Errorf("%w: 参考视频总时长 %.0f 秒超过上限 %d 秒", ErrUnsupportedParams, secSum["video"], lim)
	}
	if lim := modelItem.MaxRefAudioSeconds; lim > 0 && secSum["audio"] > float64(lim) {
		return nil, nil, fmt.Errorf("%w: 参考音频总时长 %.0f 秒超过上限 %d 秒", ErrUnsupportedParams, secSum["audio"], lim)
	}
	return blobs, keys, nil
}

func refLimitFor(item *model.ModelConfig, kind string) int {
	switch kind {
	case "video":
		return item.MaxReferenceVideos
	case "audio":
		return item.MaxReferenceAudios
	default:
		return item.MaxReferenceImages
	}
}

func refKindLabel(kind string) string {
	switch kind {
	case "video":
		return "视频"
	case "audio":
		return "音频"
	default:
		return "图"
	}
}

// validRefKey checks ownership + shape: <uid>/refs/<kind>/<name>.<ext>.
func validRefKey(uid, key string) bool {
	if uid == "" || !strings.HasPrefix(key, uid+"/refs/") {
		return false
	}
	rest := strings.TrimPrefix(key, uid+"/refs/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return false
	}
	switch parts[0] {
	case "image", "video", "audio":
	default:
		return false
	}
	name := parts[1]
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return false
	}
	switch name[dot+1:] {
	case "png", "jpg", "webp", "mp4", "webm", "mov", "mp3", "wav", "m4a", "ogg":
		return true
	}
	return false
}

func (s *V1Service) getObjectBytes(ctx context.Context, key string) ([]byte, error) {
	resp, err := s.store.Get(ctx, key, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxReferenceImageBytes+1))
}

// PresignRefUpload — POST /v1/uploads. Returns the object key + presigned PUT
// URL the caller uploads bytes to within PresignTTL.
type V1UploadView struct {
	UUID      string `json:"uuid"`
	Key       string `json:"key"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (s *V1Service) PresignRefUpload(ctx context.Context, principal *APIPrincipal, kind, mime string) (*V1UploadView, error) {
	if principal == nil || principal.User == nil {
		return nil, ErrInvalidAPIKey
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	mime = strings.ToLower(strings.TrimSpace(mime))
	if !presignRefMimeOK(kind, mime) {
		return nil, fmt.Errorf("%w: 参考素材类型或格式不支持", ErrUnsupportedParams)
	}
	if s.store == nil || !s.store.Configured() {
		return nil, fmt.Errorf("%w: 对象存储不可用", ErrProviderTemporary)
	}
	id := "r" + strings.ToLower(randomUpper(16))
	key := fmt.Sprintf("%s/refs/%s/%s.%s", principal.User.ID, kind, id, refExtOfMime(mime))
	url, err := s.store.PresignPut(key, mime, PresignTTL)
	if err != nil {
		return nil, fmt.Errorf("%w: 预签名签发失败", ErrProviderTemporary)
	}
	return &V1UploadView{UUID: id, Key: key, URL: url, ExpiresAt: time.Now().Add(PresignTTL).Unix()}, nil
}

func presignRefMimeOK(kind, mime string) bool {
	if !strings.HasPrefix(mime, kind+"/") {
		return false
	}
	switch mime {
	case "image/png", "image/jpeg", "image/webp",
		"video/mp4", "video/webm", "video/quicktime",
		"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/mp4", "audio/m4a", "audio/ogg":
		return true
	}
	return false
}

func refExtOfMime(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav":
		return "wav"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/ogg":
		return "ogg"
	default:
		return "bin"
	}
}

func strSliceContains(list []string, v string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == v {
			return true
		}
	}
	return false
}

// ===== pricing / charge / refund (unchanged semantics) =====

func (s *V1Service) prepareImage(ctx context.Context, principal *APIPrincipal, in V1ImageRequest, charge bool) (*model.ModelConfig, string, string, float64, error) {
	modelID := strings.TrimSpace(in.Model)
	prompt := strings.TrimSpace(in.Prompt)
	if modelID == "" || prompt == "" {
		return nil, "", "", 0, errors.New("model and prompt required")
	}
	modelItem, err := s.models.Get(ctx, modelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", "", 0, ErrUnknownModel
		}
		return nil, "", "", 0, err
	}
	if modelItem.Type != "image" {
		return nil, "", "", 0, ErrUnknownModel
	}
	// Fail fast before charging if no channel serves this model.
	if ok, err := s.hasChannelForModel(ctx, modelItem.ID); err != nil {
		return nil, "", "", 0, err
	} else if !ok {
		return nil, "", "", 0, ErrNoProviderAccount
	}
	refLimit := 0
	if modelItem.ImageToImage {
		refLimit = totalRefLimit(modelItem)
		if refLimit <= 0 {
			refLimit = 1
		}
	}
	if len(in.ReferenceImages) > refLimit {
		return nil, "", "", 0, errors.New("too many reference images")
	}
	if err := ensureReferenceSizes(in.ReferenceImages); err != nil {
		return nil, "", "", 0, err
	}
	aspectRatio, resolution := parseImageSize(in.Size, in.AspectRatio, in.Resolution)
	// An explicit quality label (vivid callers) wins over a blank resolution.
	if resolution == "" && strings.TrimSpace(in.Quality) != "" {
		resolution = strings.TrimSpace(in.Quality)
	}
	aspectRatio = snapRatio(aspectRatio, repo.JSONStrings(modelItem.Ratios))
	if _, ok := modelPrice(modelItem, "image", resolution, "", false); !ok {
		if fb := firstPricedResolution(modelItem); fb != "" {
			resolution = fb
		}
	}
	var surcharge float64
	if in.DeAI {
		surcharge = s.deaiSurcharge(ctx, resolution)
	}
	price, err := s.chargeForModel(ctx, principal, modelItem, "image", resolution, "", surcharge, charge)
	if err != nil {
		return nil, "", "", 0, err
	}
	return modelItem, resolution, aspectRatio, price, nil
}

func (s *V1Service) prepareVideo(ctx context.Context, principal *APIPrincipal, in V1VideoRequest, charge bool) (*model.ModelConfig, string, string, string, float64, error) {
	modelID := strings.TrimSpace(in.Model)
	prompt := strings.TrimSpace(in.Prompt)
	duration := strings.TrimSpace(in.Duration)
	if modelID == "" || prompt == "" {
		return nil, "", "", "", 0, errors.New("model and prompt required")
	}
	if duration == "" {
		return nil, "", "", "", 0, errors.New("duration required")
	}
	modelItem, err := s.models.Get(ctx, modelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", "", "", 0, ErrUnknownModel
		}
		return nil, "", "", "", 0, err
	}
	if modelItem.Type != "video" {
		return nil, "", "", "", 0, ErrUnknownModel
	}
	// Validate duration against the model's spec (options list or min/max range).
	if secs := int64(parseDurationSeconds(duration)); secs > 0 {
		if err := validateModelDuration(modelItem, secs); err != nil {
			return nil, "", "", "", 0, err
		}
	}
	if ok, err := s.hasChannelForModel(ctx, modelItem.ID); err != nil {
		return nil, "", "", "", 0, err
	} else if !ok {
		return nil, "", "", "", 0, ErrNoProviderAccount
	}
	refLimit := totalRefLimit(modelItem)
	if refLimit <= 0 && len(in.ReferenceImages) > 0 {
		return nil, "", "", "", 0, errors.New("this model does not accept references")
	}
	if refLimit <= 0 {
		refLimit = 10
	}
	if len(in.ReferenceImages) > refLimit {
		return nil, "", "", "", 0, errors.New("too many reference images")
	}
	if err := ensureReferenceSizes(in.ReferenceImages); err != nil {
		return nil, "", "", "", 0, err
	}
	aspectRatio := strings.TrimSpace(strings.ReplaceAll(in.AspectRatio, "x", ":"))
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	resolution := strings.TrimSpace(in.Resolution)
	if resolution == "" {
		if resList := repo.JSONStrings(modelItem.Resolutions); len(resList) > 0 {
			resolution = strings.TrimSpace(resList[0])
		}
		if resolution == "" {
			resolution = "720p"
		}
	}
	price, err := s.chargeForModel(ctx, principal, modelItem, "video", resolution, duration, 0, charge)
	if err != nil {
		return nil, "", "", "", 0, err
	}
	if n := parseDurationSeconds(duration); n > 0 {
		duration = fmt.Sprintf("%ds", n)
	}
	return modelItem, resolution, aspectRatio, duration, price, nil
}

// hasChannelForModel reports whether at least one enabled channel serves the
// model — the channel-era replacement for the token-pool precheck.
func (s *V1Service) hasChannelForModel(ctx context.Context, modelID string) (bool, error) {
	channels, err := s.channels.ChannelsForModel(ctx, modelID)
	if err != nil {
		return false, err
	}
	return len(channels) > 0, nil
}

func (s *V1Service) chargeForModel(ctx context.Context, principal *APIPrincipal, modelItem *model.ModelConfig, kind, resolution, duration string, surcharge float64, charge bool) (float64, error) {
	// 代理用户走代理价(某档未设代理价则回退普通价)。
	agent := principal != nil && principal.User != nil && principal.User.Role == "agent"
	price, ok := modelPrice(modelItem, kind, resolution, duration, agent)
	if !ok {
		return 0, ErrUnsupportedParams
	}
	price += surcharge
	if !charge || principal == nil || principal.User == nil {
		return 0, nil
	}
	updated, debited, err := s.users.TryDebitCredits(ctx, principal.User.ID, price)
	if err != nil {
		return 0, err
	}
	if !debited {
		if updated != nil {
			principal.User = updated
		}
		return 0, ErrInsufficientFunds
	}
	principal.User = updated
	return price, nil
}

func (s *V1Service) refundIfNeeded(ctx context.Context, principal *APIPrincipal, eventID string, price float64) error {
	if principal == nil || principal.User == nil || price <= 0 {
		return nil
	}
	// Exactly-once: claim the refund via the event's `refunded` flag. If another
	// path (e.g. the abandoned-purge sweep) already refunded, MarkRefunded
	// returns false and we skip — no double refund.
	claimed, err := s.events.MarkRefunded(ctx, eventID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	updated, err := s.users.RefundCredits(ctx, principal.User.ID, price)
	if err == nil {
		principal.User = updated
	}
	return err
}

func (s *V1Service) maybeGrantInviteReward(ctx context.Context, principal *APIPrincipal) error {
	if principal == nil || principal.User == nil || s.settings == nil {
		return nil
	}
	enabledRaw, err := s.settings.GetValue(ctx, "credits.invite_enabled")
	if err != nil {
		return err
	}
	if !parseBoolSetting(enabledRaw, true) {
		return nil
	}
	rewardRaw, err := s.settings.GetValue(ctx, "credits.invite_reward")
	if err != nil {
		return err
	}
	_, err = s.users.GrantInviteReward(ctx, principal.User.ID, parseIntSetting(rewardRaw, 3))
	return err
}

func (s *V1Service) userDir(principal *APIPrincipal) string {
	if principal == nil {
		return "anon"
	}
	return OwnerDir(principal.User)
}

// OwnerDir is the storage directory (= /images/<owner>/ segment) a user's outputs
// live under: sanitized name → sanitized email-local → id → "anon".
func OwnerDir(user *model.User) string {
	if user != nil {
		if d := sanitizeOwnerName(user.Name); d != "" {
			return d
		}
		if d := sanitizeOwnerName(strings.Split(user.Email, "@")[0]); d != "" {
			return d
		}
		if user.ID != "" {
			return user.ID
		}
	}
	return "anon"
}

// allocateOutput builds the object key (= relative path, user-scoped) and the
// /images proxy URL. Nothing is written here — the caller uploads the bytes.
func (s *V1Service) allocateOutput(principal *APIPrincipal, ext, baseURL string) (string, string) {
	userDir := s.userDir(principal)
	filename := time.Now().Format("20060102-150405") + "-" + randomUpper(8) + "." + strings.TrimPrefix(ext, ".")
	relativePath := filepath.ToSlash(filepath.Join(userDir, filename))
	if base := strings.TrimRight(strings.TrimSpace(baseURL), "/"); base != "" {
		return base + "/images/" + relativePath, relativePath
	}
	return "/images/" + relativePath, relativePath
}

func (s *V1Service) logPendingEvent(ctx context.Context, kind string, modelItem *model.ModelConfig, principal *APIPrincipal, prompt, ratio, resolution, duration string, refs int, cost float64, file, source string, refFiles []string, deai bool) (string, error) {
	event := &model.EventLog{
		ID:         "evt-" + randomUpper(12),
		TS:         time.Now(),
		Kind:       kind,
		Status:     "pending",
		Model:      modelItem.ID,
		Provider:   modelItem.Provider,
		Prompt:     prompt,
		Ratio:      ratio,
		Resolution: resolution,
		Duration:   duration,
		Refs:       refs,
		DeAI:       deai,
		Source:     source,
		Cost:       cost,
		File:       file,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if len(refFiles) > 0 {
		event.RefFiles = jsonArray(refFiles)
	}
	if principal != nil && principal.User != nil {
		event.UserID = principal.User.ID
	}
	if err := s.events.Create(ctx, event); err != nil {
		return "", err
	}
	return event.ID, nil
}

// ===== small helpers =====

func ensureReferenceSizes(inputs []string) error {
	for _, raw := range inputs {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if (len(v)*3)/4 > maxReferenceImageBytes {
			return ErrReferenceTooLarge
		}
	}
	return nil
}

func decodeReferenceImages(inputs []string, limit int) ([][]byte, error) {
	if limit <= 0 {
		limit = 1
	}
	if len(inputs) > limit {
		return nil, errors.New("too many reference images")
	}
	out := make([][]byte, 0, len(inputs))
	for _, raw := range inputs {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		// Only raw base64 is accepted (no "data:...;base64," URL prefix).
		if (len(v)*3)/4 > maxReferenceImageBytes {
			return nil, ErrReferenceTooLarge
		}
		data, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(v)
			if err != nil {
				return nil, errors.New("invalid reference image encoding")
			}
		}
		if len(data) == 0 {
			return nil, errors.New("empty reference image")
		}
		if len(data) > maxReferenceImageBytes {
			return nil, ErrReferenceTooLarge
		}
		out = append(out, data)
	}
	return out, nil
}

// totalRefLimit is the combined per-kind cap for a model.
func totalRefLimit(item *model.ModelConfig) int {
	if item == nil {
		return 0
	}
	n := item.MaxReferenceImages + item.MaxReferenceVideos + item.MaxReferenceAudios
	if item.ImageToImage && n == 0 {
		n = 1
	}
	return n
}

func parseImageSize(size, aspectRatio, resolution string) (string, string) {
	ar := strings.TrimSpace(strings.ReplaceAll(aspectRatio, "x", ":"))
	rs := strings.TrimSpace(resolution)
	if size != "" && strings.Contains(strings.ToLower(size), "x") {
		var w, h int
		_, _ = fmt.Sscanf(strings.ToLower(size), "%dx%d", &w, &h)
		if w > 0 && h > 0 {
			if ar == "" {
				ar = guessRatio(w, h)
			}
			if rs == "" {
				maxEdge := w
				if h > maxEdge {
					maxEdge = h
				}
				switch {
				case maxEdge >= 3500:
					rs = "4K"
				case maxEdge >= 1800:
					rs = "2K"
				default:
					rs = "1K"
				}
			}
		}
	}
	if ar == "" {
		ar = "1:1"
	}
	if rs == "" {
		rs = "2K"
	}
	return ar, rs
}

// snapRatio returns the entry in supported closest in value to ar ("W:H").
func snapRatio(ar string, supported []string) string {
	parse := func(s string) (float64, bool) {
		var w, h int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &w, &h); err != nil || w <= 0 || h <= 0 {
			return 0, false
		}
		return float64(w) / float64(h), true
	}
	v, ok := parse(ar)
	if !ok || len(supported) == 0 {
		return ar
	}
	best, bestDelta := "", 0.0
	for _, s := range supported {
		if strings.TrimSpace(strings.ReplaceAll(s, "x", ":")) == ar {
			return ar
		}
		sv, sok := parse(strings.ReplaceAll(s, "x", ":"))
		if !sok {
			continue
		}
		if d := absFloat(v - sv); best == "" || d < bestDelta {
			best, bestDelta = strings.TrimSpace(strings.ReplaceAll(s, "x", ":")), d
		}
	}
	if best == "" {
		return ar
	}
	return best
}

func guessRatio(w, h int) string {
	type candidate struct {
		W int
		H int
	}
	candidates := []candidate{
		{1, 1},
		{5, 4}, {4, 3}, {3, 2}, {16, 9}, {2, 1}, {21, 9}, {3, 1}, {4, 1}, {8, 1}, // 横
		{4, 5}, {3, 4}, {2, 3}, {9, 16}, {1, 3}, {1, 4}, {1, 8}, // 竖
	}
	best := candidates[0]
	bestDelta := absFloat(float64(w)/float64(h) - float64(best.W)/float64(best.H))
	for _, item := range candidates[1:] {
		delta := absFloat(float64(w)/float64(h) - float64(item.W)/float64(item.H))
		if delta < bestDelta {
			best = item
			bestDelta = delta
		}
	}
	return fmt.Sprintf("%d:%d", best.W, best.H)
}

// deaiEnabled reports whether the 去AI特征 feature is switched on in system
// settings (default off).
func (s *V1Service) deaiEnabled(ctx context.Context) bool {
	if s.settings == nil {
		return false
	}
	raw, err := s.settings.GetValue(ctx, "deai.enabled")
	if err != nil {
		return false
	}
	return parseBoolSetting(raw, false)
}

// deaiSurcharge returns the 去AI特征 surcharge (积分) for an image resolution
// tier, from site settings (defaults: 1K=1, 2K=2, 4K=3).
func (s *V1Service) deaiSurcharge(ctx context.Context, resolution string) float64 {
	key, def := "deai.price_1k", 1
	switch strings.ToUpper(strings.TrimSpace(resolution)) {
	case "2K":
		key, def = "deai.price_2k", 2
	case "4K":
		key, def = "deai.price_4k", 3
	}
	if s.settings == nil {
		return float64(def)
	}
	raw, err := s.settings.GetValue(ctx, key)
	if err != nil {
		return float64(def)
	}
	n := parseIntSetting(raw, def)
	if n < 0 {
		n = 0
	}
	return float64(n)
}

func firstPricedResolution(item *model.ModelConfig) string {
	if item == nil {
		return ""
	}
	for _, r := range []string{"1K", "2K", "4K"} {
		if _, ok := jsonMapFloat(item.Prices, r); ok {
			return r
		}
	}
	// Fall back to whatever priced tier exists (video-style labels like 720p).
	for _, r := range repo.JSONStrings(item.Resolutions) {
		if _, ok := jsonMapFloat(item.Prices, r); ok {
			return r
		}
	}
	return ""
}

// modelPrice returns the charge for (kind, resolution, duration). The set of
// supported tiers is driven by the NORMAL prices; `agent` only overrides the
// amount with the agent price when one is set for that tier.
func modelPrice(item *model.ModelConfig, kind, resolution, duration string, agent bool) (float64, bool) {
	if item == nil {
		return 0, false
	}
	tierPrice := func(normal, agentMap map[string]any, key string) (float64, bool) {
		nv, ok := jsonMapFloat(normal, key)
		if !ok {
			return 0, false
		}
		if agent {
			if av, aok := jsonMapFloat(agentMap, key); aok {
				return av, true
			}
		}
		return nv, true
	}
	if kind == "video" {
		rv, rok := tierPrice(item.Prices, item.PricesAgent, resolution)
		var dv float64
		var dok bool
		if pps, hasPerSec := jsonMapFloat(item.DurationPrices, "per_second"); hasPerSec {
			secs := parseDurationSeconds(duration)
			dv = pps * float64(secs)
			dok = true
			if agent {
				if av, aok := jsonMapFloat(item.DurationPricesAgent, "per_second"); aok {
					dv = av * float64(secs)
				}
			}
		} else {
			dv, dok = tierPrice(item.DurationPrices, item.DurationPricesAgent, duration)
		}
		if !rok || !dok {
			return 0, false
		}
		return rv + dv, true
	}
	return tierPrice(item.Prices, item.PricesAgent, resolution)
}

func jsonMapFloat(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		// datatypes.JSONMap.Scan decodes with UseNumber(), so values loaded from
		// the DB arrive as json.Number — NOT float64.
		if f, err := x.Float64(); err == nil {
			return f, true
		}
	case string:
		var out float64
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%f", &out); err == nil {
			return out, true
		}
	}
	return 0, false
}

func sanitizeOwnerName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseDurationSeconds(raw string) int {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.TrimSuffix(raw, "s")
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return 5
	}
	return n
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func principalCredits(principal *APIPrincipal) float64 {
	if principal == nil || principal.User == nil {
		return 0
	}
	return principal.User.Credits
}
