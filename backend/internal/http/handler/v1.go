// Package handler — /v1/* 消费者 HTTP 接口（vivid 协议同款）。
// 这套接口同时是渠道上游契约：同构实例经相同的四个端点互相对接。
//
//	POST /v1/generate   创建生成任务（NDJSON 流：首帧 jobId → 终态帧；断流携 jobId 重发续等）
//	POST /v1/uploads    参考素材预签名直传（返回 key 随 generate 的 refs 提交）
//	GET  /v1/models     可售模型裸数组（启用 + 有渠道绑定 + 全部档位已配价）
//	GET  /v1/balance    {balance}：余额积分
//
// 鉴权：Authorization: Bearer vk_...（用户 API Key，哈希落库）。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/model"
	"backend/internal/service"
	"github.com/gin-gonic/gin"
)

// v1MaxWait 同步等待终态的上限。视频单渠道预算 30 分钟、最多换几个渠道，
// 留足余量取 2 小时（与 vivid 一致）。
const v1MaxWait = 2 * time.Hour

const v1PollInterval = time.Second

type V1Handler struct {
	v1 *service.V1Service
}

func NewV1Handler(v1 *service.V1Service) *V1Handler {
	return &V1Handler{v1: v1}
}

func (h *V1Handler) auth(c *gin.Context) (*service.APIPrincipal, bool) {
	principal, err := h.v1.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
	if err != nil {
		v1Err(c, http.StatusUnauthorized, "4011", "无效的 API Key")
		return nil, false
	}
	return principal, true
}

// v1Err writes the {"error":{"code","message"}} envelope — the wire shape every
// vivid-protocol caller parses.
func v1Err(c *gin.Context, httpStatus int, code, msg string) {
	c.JSON(httpStatus, gin.H{
		"error": gin.H{"code": code, "message": msg},
	})
}

// v1Fail maps a service error to the vivid code/HTTP status pair.
func v1Fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMissingAPIKey), errors.Is(err, service.ErrInvalidAPIKey):
		v1Err(c, http.StatusUnauthorized, "4011", "无效的 API Key")
	case errors.Is(err, service.ErrInsufficientFunds):
		v1Err(c, http.StatusBadRequest, "4002", "积分不足，请先充值")
	case errors.Is(err, service.ErrConcurrencyFull), errors.Is(err, service.ErrUserConcurrencyFull):
		v1Err(c, http.StatusTooManyRequests, "4293", "当前并发已满，请稍后重试")
	case errors.Is(err, service.ErrVideoJobNotFound):
		v1Err(c, http.StatusBadRequest, "4001", "任务不存在")
	case errors.Is(err, service.ErrReferenceTooLarge):
		v1Err(c, http.StatusRequestEntityTooLarge, "4001", "参考素材超过大小上限")
	case errors.Is(err, service.ErrUnknownModel):
		v1Err(c, http.StatusBadRequest, "4001", "模型不存在或未启用")
	case errors.Is(err, service.ErrNoProviderAccount):
		v1Err(c, http.StatusBadRequest, "4001", "模型无可用渠道")
	case errors.Is(err, service.ErrBannedPrompt):
		v1Err(c, http.StatusBadRequest, "4001", "提示词包含违禁内容")
	case errors.Is(err, service.ErrUnsupportedParams):
		v1Err(c, http.StatusBadRequest, "4001", err.Error())
	case errors.Is(err, service.ErrProviderTemporary), errors.Is(err, service.ErrProviderExecution),
		errors.Is(err, service.ErrProviderAuth):
		v1Err(c, http.StatusInternalServerError, "5001", "生成暂时不可用，积分已退还")
	default:
		v1Err(c, http.StatusBadRequest, "4001", err.Error())
	}
}

// ---- POST /v1/generate ----

func (h *V1Handler) Generate(c *gin.Context) {
	principal, ok := h.auth(c)
	if !ok {
		return
	}
	var body service.V1JobRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		v1Err(c, http.StatusBadRequest, "4001", "请求体不是合法 JSON")
		return
	}
	var ev *model.EventLog
	var err error
	// 重连模式：只带 jobId → 不建新任务，回读该任务并等待终态。
	if body.JobID != nil && strings.TrimSpace(*body.JobID) != "" {
		ev, err = h.v1.JobForUser(c.Request.Context(), principal, *body.JobID)
	} else {
		ev, err = h.v1.CreateJob(c.Request.Context(), principal, body)
	}
	if err != nil {
		v1Fail(c, err)
		return
	}

	wait := 300 * time.Second
	if h := strings.TrimSpace(c.GetHeader("X-Wait-Seconds")); h != "" {
		if sec, serr := strconv.Atoi(h); serr == nil && sec > 0 && time.Duration(sec)*time.Second <= v1MaxWait {
			wait = time.Duration(sec) * time.Second
		}
	}

	c.Writer.Header().Set("Content-Type", "application/x-ndjson")
	c.Writer.Header().Set("Cache-Control", "no-cache, no-transform")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	writeFrame := func(v *model.EventLog) bool {
		frame, _ := json.Marshal(v1JobBody(h.v1, v))
		if _, werr := c.Writer.Write(append(frame, '\n')); werr != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}

	// 已终态：直接回终态帧关流。
	if st := service.JobStatus(ev); st == "succeeded" || st == "failed" {
		writeFrame(ev)
		return
	}
	// 连上即回执 jobId（断流后携它重发的凭据）。
	if !writeFrame(ev) {
		return
	}

	deadline := time.Now().Add(wait)
	poll := time.NewTicker(v1PollInterval)
	heartbeat := time.NewTicker(25 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return // client gone — the job keeps running; caller may reconnect with jobId
		case <-poll.C:
			latest, lerr := h.v1.JobForUser(c.Request.Context(), principal, ev.ID)
			if lerr != nil || latest == nil {
				continue
			}
			ev = latest
			if st := service.JobStatus(ev); st == "succeeded" || st == "failed" {
				writeFrame(ev)
				return
			}
		case <-heartbeat.C:
			if _, werr := c.Writer.WriteString("\n"); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-time.After(time.Until(deadline)):
			writeFrame(ev) // snapshot; client reconnects with jobId
			return
		}
	}
}

// v1JobBody assembles one NDJSON frame (running receipt and terminal frame
// share the shape; succeeded adds result+creditsCharged, failed adds error).
func v1JobBody(v1 *service.V1Service, ev *model.EventLog) map[string]any {
	body := map[string]any{"jobId": ev.ID, "status": service.JobStatus(ev)}
	switch service.JobStatus(ev) {
	case "succeeded":
		body["result"] = v1.JobResultURL(ev)
		body["creditsCharged"] = ev.Cost
	case "failed":
		body["error"] = ev.Error
		body["refunded"] = true
	}
	return body
}

// ---- POST /v1/uploads ----

func (h *V1Handler) Uploads(c *gin.Context) {
	principal, ok := h.auth(c)
	if !ok {
		return
	}
	var body struct {
		Kind string `json:"kind"`
		Mime string `json:"mime"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		v1Err(c, http.StatusBadRequest, "4001", "请求体不是合法 JSON")
		return
	}
	view, err := h.v1.PresignRefUpload(c.Request.Context(), principal, body.Kind, body.Mime)
	if err != nil {
		v1Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// ---- GET /v1/models ----

func (h *V1Handler) Models(c *gin.Context) {
	if _, ok := h.auth(c); !ok {
		return
	}
	items, err := h.v1.SellableModels(c.Request.Context())
	if err != nil {
		v1Err(c, http.StatusInternalServerError, "5001", "模型加载失败")
		return
	}
	c.JSON(http.StatusOK, items)
}

// ---- GET /v1/balance ----

func (h *V1Handler) UserBalance(c *gin.Context) {
	principal, ok := h.auth(c)
	if !ok {
		return
	}
	bal, err := h.v1.UserBalance(c.Request.Context(), principal)
	if err != nil {
		v1Err(c, http.StatusInternalServerError, "5001", "余额查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"balance": bal})
}

// requestBaseURL derives the scheme+host of the inbound request so the service
// layer can build absolute /images URLs for the session path.
func requestBaseURL(c *gin.Context) string {
	host := c.Request.Host
	if host == "" {
		return ""
	}
	scheme := "http"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = strings.ToLower(strings.Split(proto, ",")[0])
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host
}
