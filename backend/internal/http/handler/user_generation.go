package handler

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"backend/internal/model"
	"backend/internal/repo"
	"backend/internal/service"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

type UserGenerationHandler struct {
	userGen *service.UserGenerationService
	admin   *service.AdminReadService
	idem    *idemStore
}

func NewUserGenerationHandler(userGen *service.UserGenerationService, admin *service.AdminReadService) *UserGenerationHandler {
	return &UserGenerationHandler{
		userGen: userGen,
		admin:   admin,
		idem:    &idemStore{m: map[string]*idemEntry{}},
	}
}

// /generate 是同步长请求(视频要跑好几分钟)。等待期间连接一旦被重置(CDN 回源
// 超时 / HTTP2 GOAWAY),浏览器会把还没拿到响应的 POST 透明重发,后端就会再生成
// 一次、再扣一次积分。前端为每个任务带一个 Idempotency-Key,同一个 key 只真正
// 执行一次:原任务还在跑就直接拒绝,已经跑完就把原结果返回。
const idemTTL = 10 * time.Minute

type idemEntry struct {
	done bool
	resp map[string]any
	at   time.Time
}

type idemStore struct {
	mu sync.Mutex
	m  map[string]*idemEntry
}

// begin 登记一个 key。第二个返回值为 true 表示这个 key 已经在处理或刚处理完,
// 返回的 entry 是原来那次的状态。
func (s *idemStore) begin(key string) (*idemEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, e := range s.m {
		if e.done && now.Sub(e.at) > idemTTL {
			delete(s.m, k)
		}
	}
	if e, ok := s.m[key]; ok {
		return e, true
	}
	e := &idemEntry{at: now}
	s.m[key] = e
	return e, false
}

// finish 记下结果供重发命中;resp 为 nil(本次失败)时直接释放 key,允许用户重试。
func (s *idemStore) finish(key string, resp map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resp == nil {
		delete(s.m, key)
		return
	}
	if e, ok := s.m[key]; ok {
		e.done, e.resp, e.at = true, resp, time.Now()
	}
}

// MyImages returns the current user's own recently generated images (scoped to
// their owner directory) — used by the showcase "选择已生成" picker so an admin
// only sees their own images, not everyone's.
func (h *UserGenerationHandler) MyImages(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	items, err := h.admin.RecentImagesOwned(c.Request.Context(), service.OwnerDir(user), 60)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load images"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// UploadMyImage stores one user-supplied image (multipart field "file") under
// the caller's owner directory, so it can be placed on the infinite canvas and
// shows up in the 我的图片 picker alongside generated outputs. Returns the
// storage key ("owner/name") the frontend feeds to generatedUrl/thumbUrl.
func (h *UserGenerationHandler) UploadMyImage(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 20<<20)
	f, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请选择图片文件"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 15<<20+1))
	if err != nil || len(data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "读取文件失败"})
		return
	}
	if len(data) > 15<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "图片不能超过 15MB"})
		return
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "不是有效的图片文件(支持 png/jpg/webp/gif)"})
		return
	}
	key, err := h.admin.SaveOwnedImage(c.Request.Context(), service.OwnerDir(user), format, data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": key})
}

// DeleteMyFile removes ONE of the caller's own generated files (plus its
// thumbnail) and blanks the log rows referencing it, so the 画图台 grid and
// 创作记录 gallery stop showing it. ?file= is the storage key (owner/name).
func (h *UserGenerationHandler) DeleteMyFile(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	if err := h.admin.DeleteOwnedFile(c.Request.Context(), service.OwnerDir(user), c.Query("file")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *UserGenerationHandler) Generate(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}

	var body struct {
		Model           string   `json:"model"`
		Prompt          string   `json:"prompt"`
		Ratio           string   `json:"ratio"`
		Resolution      string   `json:"resolution"`
		Duration        string   `json:"duration"`
		ReferenceImages []string `json:"reference_images"`
		ReferenceMode   string   `json:"reference_mode"`
		DeAI            bool     `json:"deai"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}

	var generated map[string]any // 成功时的响应,供同 key 的重发直接命中
	if key := strings.TrimSpace(c.GetHeader("Idempotency-Key")); key != "" {
		key = user.ID + "|" + key
		if e, dup := h.idem.begin(key); dup {
			if e.done {
				c.JSON(http.StatusOK, e.resp)
			} else {
				c.JSON(http.StatusConflict, gin.H{"detail": "该任务已在生成中,已忽略重复提交"})
			}
			return
		}
		defer func() { h.idem.finish(key, generated) }()
	}

	resp, err := h.userGen.Generate(c.Request.Context(), user, service.UserGenerateRequest{
		Model:           body.Model,
		Prompt:          body.Prompt,
		Ratio:           body.Ratio,
		Resolution:      body.Resolution,
		Duration:        body.Duration,
		ReferenceImages: body.ReferenceImages,
		ReferenceMode:   body.ReferenceMode,
		DeAI:            body.DeAI,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnknownModel):
			c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrUnsupportedParams), errors.Is(err, service.ErrBannedPrompt):
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrInsufficientFunds):
			c.JSON(http.StatusPaymentRequired, gin.H{"detail": "积分不足"})
		case errors.Is(err, service.ErrNoProviderAccount):
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderAuth), errors.Is(err, service.ErrProviderTemporary):
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderQuota):
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrConcurrencyFull), errors.Is(err, service.ErrUserConcurrencyFull):
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderExecution):
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		default:
			if err.Error() == "已有正在生成的任务,请稍候" {
				c.JSON(http.StatusConflict, gin.H{"detail": err.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		}
		return
	}
	generated = resp
	c.JSON(http.StatusOK, resp)
}

func (h *UserGenerationHandler) Test(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"detail": "需要管理员权限"})
		return
	}

	var body struct {
		Model           string   `json:"model"`
		Prompt          string   `json:"prompt"`
		Ratio           string   `json:"ratio"`
		Resolution      string   `json:"resolution"`
		Duration        string   `json:"duration"`
		ReferenceImages []string `json:"reference_images"`
		ChannelID       string   `json:"channel_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}

	resp, err := h.userGen.AdminTest(c.Request.Context(), user, service.UserGenerateRequest{
		Model:           body.Model,
		Prompt:          body.Prompt,
		Ratio:           body.Ratio,
		Resolution:      body.Resolution,
		Duration:        body.Duration,
		ReferenceImages: body.ReferenceImages,
		ChannelID:       body.ChannelID,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnknownModel):
			c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrUnsupportedParams), errors.Is(err, service.ErrBannedPrompt):
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderQuota):
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrConcurrencyFull), errors.Is(err, service.ErrUserConcurrencyFull):
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrNoProviderAccount):
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderAuth), errors.Is(err, service.ErrProviderTemporary):
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": err.Error()})
		case errors.Is(err, service.ErrProviderExecution):
			c.JSON(http.StatusBadGateway, gin.H{"detail": err.Error()})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *UserGenerationHandler) MyJobs(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusOK, gin.H{"pending": nil, "latest": nil})
		return
	}
	data, err := h.userGen.MyJobs(c.Request.Context(), user, c.Query("source"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load jobs"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *UserGenerationHandler) Logs(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}

	limit := parseInt(c.Query("limit"), 50)
	offset := parseInt(c.Query("offset"), 0)
	kind := c.Query("kind")
	status := c.Query("status")
	// statuses=pending,success → status IN (...). Used by the 画图台 grid so it can
	// fetch exactly the rows it shows (进行中 + 成功) in one query, server-side.
	var statuses []string
	if s := strings.TrimSpace(c.Query("statuses")); s != "" {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				statuses = append(statuses, p)
			}
		}
	}
	// Secure-by-default: always scope to the caller's OWN records. This endpoint
	// serves the front-end 日志 / 创作记录 pages, so an admin viewing their personal
	// records must NOT see other users' work. Only an admin who explicitly opts
	// into the full view (?scope=all — the admin 日志 page) sees everyone's logs.
	// API-key ("v1") usage IS included for the caller's own records so the user
	// can audit their key's calls on /mylogs; the image-only 创作记录 gallery still
	// hides them client-side (they have no stored file).
	userID := user.ID
	excludeSource := ""
	if user.Role == "admin" && c.Query("scope") == "all" {
		userID = ""
	}
	// 来源筛选: "v1" = API key, "user" = 前台画图, "admin" = 测试模型. 始终生效 ——
	// 普通用户已被 userID 限定为本人记录,按来源服务端筛选 + 分页(/mylogs 翻全部历史)。
	source := c.Query("source")
	// 创作记录 gallery passes has_file=1 so server-side pagination counts only
	// rows with real media (success + stored file), not failed/pending events.
	hasFile := c.Query("has_file") == "1" || c.Query("has_file") == "true"

	// Media views hide homepage showcase files — those belong to the public
	// landing page, not to the caller's personal works. Galleries imply it via
	// has_file; the 画图台 grid opts in with exclude_showcase=1.
	excludeShowcase := hasFile || c.Query("exclude_showcase") == "1"
	// media=1 (画图台 grid): only pending rows or rows with a stored file, so a
	// deleted work's blanked row doesn't consume one of the grid's slots.
	mediaOnly := c.Query("media") == "1"
	// ?user= — admin-only 用户搜索 (the 日志管理 page with scope=all). Ignored for
	// normal users, whose rows are already pinned to their own userID.
	var userIDs []string
	if term := strings.TrimSpace(c.Query("user")); term != "" && userID == "" {
		ids, uerr := h.admin.MatchUserIDs(c.Request.Context(), term)
		if uerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load logs"})
			return
		}
		if len(ids) == 0 {
			ids = []string{"__no_match__"}
		}
		userIDs = ids
	}
	items, total, stats, err := h.admin.Logs(c.Request.Context(), limit, offset, kind, status, statuses, nil, userID, userIDs, strings.TrimSpace(c.Query("q")), excludeSource, source, hasFile, excludeShowcase, mediaOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load logs"})
		return
	}
	// Resolve user_id -> display name (mirrors admin.py / AdminReadHandler.Logs).
	// Without this the log table showed every row as "匿名".
	nameByID, err := h.admin.UserNameMap(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load logs"})
		return
	}
	// Resolve account_id -> account label so the log table can show which
	// provider account fulfilled each generation under the user.
	accountByID, err := h.admin.AccountNameMap(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load logs"})
		return
	}
	modelByID, err := h.admin.ModelNameMap(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load logs"})
		return
	}

	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		var userName any
		if item.UserID == "" {
			userName = "匿名"
		} else if name, ok := nameByID[item.UserID]; ok {
			userName = name
		} else {
			userName = item.UserID
		}
		// Provider account identity is admin-only: normal users must not see
		// which upstream account (email) fulfilled their generation.
		var accountName, accountID any
		if userID == "" {
			if item.AccountEmail != "" {
				// Email stamped on the row itself survives account deletion/re-import.
				accountName = item.AccountEmail
			} else if item.AccountID != "" {
				if label, ok := accountByID[item.AccountID]; ok {
					accountName = label
				} else {
					accountName = item.AccountID
				}
			}
			accountID = emptyStringNil(item.AccountID)
		}
		out = append(out, gin.H{
			"id":         item.ID,
			"ts":         item.TS.Unix(),
			"kind":       item.Kind,
			"status":     item.Status,
			"model":      displayModelName(modelByID, item.Model),
			"provider":   item.Provider,
			"prompt":     item.Prompt,
			"ratio":      item.Ratio,
			"resolution": item.Resolution,
			"duration":   item.Duration,
			"refs":       item.Refs,
			"deai":       item.DeAI,
			"source":     emptyStringNil(item.Source),
			"user_id":    emptyStringNil(item.UserID),
			"user_name":  userName,
			"account_id": accountID,
			"account":    accountName,
			"cost":       item.Cost,
			"elapsed_ms": item.ElapsedMS,
			"file":       emptyStringNil(item.File),
			"error":      emptyStringNil(item.Error),
			"created_at": unixSec(item.CreatedAt),
			"updated_at": unixSec(item.UpdatedAt),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"stats":  stats,
	})
}

func (h *UserGenerationHandler) VideoPresets(c *gin.Context) {
	models, err := h.admin.Models(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load video presets"})
		return
	}
	out := make([]gin.H, 0, len(models))
	for _, m := range models {
		if m.Type != "video" || !m.Enabled {
			continue
		}
		out = append(out, modelPreset(m))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *UserGenerationHandler) Catalog(c *gin.Context) {
	items, err := h.catalogEntries(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load catalog"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": items,
	})
}

func (h *UserGenerationHandler) Models(c *gin.Context) {
	items, err := h.publicModels(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load models"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// modelPreset projects one model_configs row into the preset shape the studio
// (画图台) / model pickers consume. Database-driven now: providers are gone and
// every model arrives via a channel import.
func modelPreset(m model.ModelConfig) gin.H {
	durations := repo.JSONStrings(m.Durations)
	if len(durations) == 0 && m.DurationMin > 0 && m.DurationMax >= m.DurationMin {
		// Range spec → expand integer seconds (capped so absurd ranges can't
		// flood a dropdown).
		for d := m.DurationMin; d <= m.DurationMax && len(durations) < 61; d++ {
			durations = append(durations, fmt.Sprintf("%ds", d))
		}
	}
	_, perSecond := m.DurationPrices["per_second"]
	return gin.H{
		"key":                  m.EffectiveName(),
		"id":                   m.ID,
		"label":                m.EffectiveName(),
		"type":                 m.Type,
		"provider":             m.Provider,
		"ratios":               repo.JSONStrings(m.Ratios),
		"resolutions":          repo.JSONStrings(m.Resolutions),
		"durations":            durations,
		"duration_min":         m.DurationMin,
		"duration_max":         m.DurationMax,
		"per_second":           perSecond,
		"max_reference_images": m.MaxReferenceImages,
		"max_images":           m.MaxReferenceImages,
		"max_videos":           m.MaxReferenceVideos,
		"max_audios":           m.MaxReferenceAudios,
		"max_reference_videos": m.MaxReferenceVideos,
		"max_reference_audios": m.MaxReferenceAudios,
		"max_video_seconds":    m.MaxRefVideoSeconds,
		"max_audio_seconds":    m.MaxRefAudioSeconds,
		"reference_mode":       m.ReferenceMode,
		"image_to_image":       m.ImageToImage,
		"description":          m.Name,
		"added":                true,
	}
}

// catalogEntries lists the managed models (previously a static picklist of
// offerable provider models). With channel imports the DB IS the catalog —
// rows are already "added", so the admin picklist degrades gracefully.
func (h *UserGenerationHandler) catalogEntries(c *gin.Context) ([]gin.H, error) {
	models, err := h.admin.Models(c.Request.Context())
	if err != nil {
		return nil, err
	}
	items := make([]gin.H, 0, len(models))
	for _, m := range models {
		row := modelPreset(m)
		row["id"] = m.EffectiveName()
		items = append(items, row)
	}
	return items, nil
}

// publicModels — GET /api/models for the public site (docs/landing pickers).
// Enabled models only, in the same shape as before minus the provider id.
func (h *UserGenerationHandler) publicModels(c *gin.Context) ([]gin.H, error) {
	models, err := h.admin.Models(c.Request.Context())
	if err != nil {
		return nil, err
	}
	items := make([]gin.H, 0, len(models))
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		items = append(items, gin.H{
			"id":          m.EffectiveName(),
			"kind":        m.Type,
			"type":        m.Type,
			"ratios":      repo.JSONStrings(m.Ratios),
			"resolutions": repo.JSONStrings(m.Resolutions),
			"durations":   repo.JSONStrings(m.Durations),
			"description": m.Name,
			"stub":        false,
		})
	}
	return items, nil
}
