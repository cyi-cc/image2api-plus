// Package upstream is the vivid-protocol client used to talk to channel
// upstreams (other same-protocol instances). It covers the full public
// surface: POST /v1/generate (NDJSON stream with jobId reconnect), POST
// /v1/uploads (presigned reference upload), GET /v1/models, GET /v1/balance,
// plus presigned-URL result download.
package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Error classes. Dispatch decisions (retry / fail over / fail the job) key off
// these sentinels via errors.Is — KindOf maps them for log messages.
var (
	// ErrAuth = channel credentials rejected (4011 / HTTP 401 / 403). The
	// channel is skipped without consuming a retry budget.
	ErrAuth = errors.New("upstream auth rejected")
	// ErrInvalid = upstream rejected the request itself (4001 / HTTP 4xx).
	// Retrying on another channel would fail identically — terminal.
	ErrInvalid = errors.New("upstream rejected request")
	// ErrJobFailed = the upstream ran the job and returned a failed frame.
	// The frame's error text is user-safe and surfaced as-is.
	ErrJobFailed = errors.New("upstream job failed")
	// ErrRetryable = transient upstream failure (4293 / 5001 / 5xx / quota
	// / timeout / transport). Fail over to the next channel.
	ErrRetryable = errors.New("upstream temporary failure")
	// ErrAmbiguous = the generate request was sent but the stream dropped
	// before any jobId arrived — the job may or may not exist upstream.
	// Treated as retryable for dispatch purposes.
	ErrAmbiguous = errors.New("upstream submission ambiguous")
)

// Kind is the coarse dispatch classification of an upstream error.
type Kind int

const (
	KindRetryable Kind = iota
	KindAuth
	KindInvalid
	KindJobFailed
)

// KindOf classifies an error returned by this package.
func KindOf(err error) Kind {
	switch {
	case errors.Is(err, ErrAuth):
		return KindAuth
	case errors.Is(err, ErrInvalid):
		return KindInvalid
	case errors.Is(err, ErrJobFailed):
		return KindJobFailed
	default:
		return KindRetryable
	}
}

// Failoverable reports whether the request should be re-tried on another
// channel. ErrInvalid and ErrJobFailed are deterministic — same request, same
// result — so they are terminal.
func Failoverable(err error) bool {
	return !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrJobFailed)
}

// GenParams is the POST /v1/generate request body.
type GenParams struct {
	Model    string   `json:"model"`
	Prompt   string   `json:"prompt"`
	Quality  string   `json:"quality,omitempty"`
	Ratio    string   `json:"ratio,omitempty"`
	Duration int64    `json:"duration,omitempty"`
	Refs     []string `json:"refs,omitempty"`
	JobID    string   `json:"jobId,omitempty"`
}

// RemoteModel mirrors one item of a vivid GET /v1/models response.
type RemoteModel struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Qualities []string        `json:"qualities"`
	Duration  *RemoteDuration `json:"duration,omitempty"`
	// Per-kind reference caps; 0 = that kind is not accepted.
	RefImages int64 `json:"refImages"`
	RefVideos int64 `json:"refVideos"`
	RefAudios int64 `json:"refAudios"`
	// Combined reference duration caps in seconds; 0 = unlimited.
	RefVideoSeconds int64 `json:"refVideoSeconds,omitempty"`
	RefAudioSeconds int64 `json:"refAudioSeconds,omitempty"`
	// Bound is set by the admin fetch-models endpoint only: true when this
	// upstream id is already bound to that channel locally.
	Bound bool `json:"bound,omitempty"`
}

// RemoteDuration mirrors the duration spec of a vivid model item.
type RemoteDuration struct {
	Mode    string  `json:"mode"` // options | range
	Seconds int64   `json:"seconds,omitempty"`
	Min     int64   `json:"min,omitempty"`
	Max     int64   `json:"max,omitempty"`
	Options []int64 `json:"options,omitempty"`
}

// Client is stateless; per-call baseURL/key come from the channel row.
type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Minute}}
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return NewClient().HTTP
}

// maxStreamFailures caps reconnect attempts after a jobId is known — each
// reconnect re-opens /v1/generate with {jobId} and never double-charges.
const maxStreamFailures = 3

// Generate submits a vivid job and reads the NDJSON stream to a terminal
// frame, reconnecting with the reported jobId when the stream drops mid-run
// (upstream heartbeat keeps us attached; transport blips don't lose the job).
// onJobID, when non-nil, fires once the upstream assigns a job id. Returns the
// presigned result URL and the credits the upstream reports charging us.
func (c *Client) Generate(ctx context.Context, baseURL, apiKey string, p GenParams, waitSeconds int64, onJobID func(string)) (resultURL string, credits int64, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return "", 0, ErrAuth
	}
	var last map[string]any
	failures := 0
	for {
		term, ferr := c.streamOnce(ctx, baseURL, apiKey, &p, waitSeconds, onJobID)
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		if ferr != nil && !errors.Is(ferr, ErrRetryable) && !errors.Is(ferr, ErrAmbiguous) {
			return "", 0, ferr
		}
		if ferr != nil {
			failures++
		} else {
			failures = 0
		}
		if ferr == nil && term != nil {
			status := strings.ToLower(strings.TrimSpace(str(term["status"])))
			if status != "running" && status != "" {
				last = term
				break
			}
		}
		if p.JobID == "" {
			if ferr != nil {
				return "", 0, ferr
			}
			return "", 0, ErrAmbiguous
		}
		if failures >= maxStreamFailures {
			if ferr != nil {
				return "", 0, ferr
			}
			return "", 0, fmt.Errorf("%w: stream dropped repeatedly", ErrRetryable)
		}
		if sleepCtx(ctx, 2*time.Second) != nil {
			return "", 0, ctx.Err()
		}
	}
	status := strings.ToLower(strings.TrimSpace(str(last["status"])))
	switch status {
	case "succeeded", "completed", "success":
		resultURL = strings.TrimSpace(str(last["result"]))
		if resultURL == "" {
			return "", 0, fmt.Errorf("%w: succeeded frame missing result", ErrRetryable)
		}
		return resultURL, int64(num(last["creditsCharged"])), nil
	default:
		reason := strings.TrimSpace(str(last["error"]))
		if reason == "" {
			reason = "upstream job " + status
		}
		if isCreditError(reason) {
			return "", 0, fmt.Errorf("%w: %s", ErrRetryable, clip([]byte(reason), 160))
		}
		// failed 帧的 error 是上游给用户看的中文文案 — 透传
		return "", 0, fmt.Errorf("%w: %s", ErrJobFailed, clip([]byte(reason), 160))
	}
}

// streamOnce performs one /v1/generate call and scans the NDJSON body. It
// records the upstream jobId into p.JobID as soon as a frame carries it.
func (c *Client) streamOnce(ctx context.Context, baseURL, apiKey string, p *GenParams, waitSeconds int64, onJobID func(string)) (map[string]any, error) {
	raw, _ := json.Marshal(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/generate", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if waitSeconds <= 0 {
		waitSeconds = 300
	}
	req.Header.Set("X-Wait-Seconds", fmt.Sprintf("%d", waitSeconds))
	resp, err := c.http().Do(req)
	if err != nil {
		if p.JobID == "" {
			return nil, fmt.Errorf("%w: %s", ErrAmbiguous, sanitizeErr(err))
		}
		return nil, fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, vividError(resp.StatusCode, body)
	}
	var last map[string]any
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue // 25s heartbeat empty line
		}
		var frame map[string]any
		if json.Unmarshal(line, &frame) != nil {
			continue
		}
		last = frame
		if job := strings.TrimSpace(str(frame["jobId"])); job != "" && job != p.JobID {
			p.JobID = job
			if onJobID != nil {
				onJobID(job)
			}
		}
		if status := strings.ToLower(strings.TrimSpace(str(frame["status"]))); status == "succeeded" || status == "failed" {
			return frame, nil
		}
	}
	if err := sc.Err(); err != nil {
		if p.JobID == "" {
			return nil, fmt.Errorf("%w: %s", ErrAmbiguous, sanitizeErr(err))
		}
		return nil, fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	return last, nil
}

// UploadRef runs the two-step reference upload: POST /v1/uploads {kind,mime}
// then PUT the bytes to the presigned url with the exact signed Content-Type.
// Returns the object key for refs[].
func (c *Client) UploadRef(ctx context.Context, baseURL, apiKey string, data []byte) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return "", ErrAuth
	}
	kind, mime := RefKindMime(data)
	if kind == "" {
		return "", fmt.Errorf("%w: 参考素材格式不支持", ErrInvalid)
	}
	raw, _ := json.Marshal(map[string]any{"kind": kind, "mime": mime})
	up, err := c.postJSON(ctx, baseURL+"/v1/uploads", apiKey, raw)
	if err != nil {
		return "", err
	}
	putURL, key := UploadSession(up)
	if putURL == "" || key == "" {
		rawUp, _ := json.Marshal(up)
		return "", fmt.Errorf("%w: upload session missing url/key (got %s)", ErrRetryable, clip(rawUp, 200))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mime) // must match the signed mime exactly
	resp, err := c.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: ref upload %d %s", ErrRetryable, resp.StatusCode, clip(body, 120))
	}
	return key, nil
}

// UploadSession extracts the presigned PUT url + object key from a
// /v1/uploads response. Implementations vary: some return the pair at top
// level, others wrap it in {"data":{...}} / {"result":{...}}, and field
// spellings differ (upload_url / presigned_url / put_url; key / file_key /
// object_key). Search top level first, then one layer deep.
func UploadSession(up map[string]any) (putURL, key string) {
	// Go map 查找区分大小写，而上游实现可能返回 PascalCase 字段；按去分隔符+
	// 小写归一化匹配。
	urlKeys := map[string]bool{"url": true, "uploadurl": true, "presignedurl": true, "puturl": true, "signedurl": true}
	keyKeys := map[string]bool{"key": true, "filekey": true, "objectkey": true, "uploadkey": true}
	norm := func(s string) string { return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s)) }
	pick := func(m map[string]any) (string, string) {
		var u, k string
		for name, v := range m {
			s := strings.TrimSpace(str(v))
			if s == "" {
				continue
			}
			switch {
			case u == "" && urlKeys[norm(name)]:
				u = s
			case k == "" && keyKeys[norm(name)]:
				k = s
			}
		}
		return u, k
	}
	levels := []map[string]any{up}
	for _, nest := range []string{"data", "result", "upload", "session", "upload_session"} {
		if sub, ok := up[nest].(map[string]any); ok {
			levels = append(levels, sub)
		}
	}
	for _, m := range levels {
		u, k := pick(m)
		if putURL == "" {
			putURL = u
		}
		if key == "" {
			key = k
		}
	}
	return putURL, key
}

// Models fetches the upstream's sellable model list (GET /v1/models → bare
// array of RemoteModel).
func (c *Client) Models(ctx context.Context, baseURL, apiKey string) ([]RemoteModel, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, ErrAuth
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, vividError(resp.StatusCode, body)
	}
	var items []RemoteModel
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("%w: bad /v1/models body", ErrRetryable)
	}
	return items, nil
}

// Balance fetches the upstream balance (GET /v1/balance → {"balance":N}).
// Returns nil when the upstream does not implement the endpoint.
func (c *Client) Balance(ctx context.Context, baseURL, apiKey string) (*int64, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, ErrAuth
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, vividError(resp.StatusCode, body)
	}
	var env struct {
		Balance *int64 `json:"balance"`
	}
	if json.Unmarshal(body, &env) != nil || env.Balance == nil {
		return nil, nil
	}
	return env.Balance, nil
}

// Download fetches a presigned result URL — no Authorization header.
func (c *Client) Download(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%w: result download %d", ErrRetryable, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("%w: empty result", ErrRetryable)
	}
	// Content-Type fallback chain: only trust image/* video/* headers (upstreams
	// often mislabel results text/plain or octet-stream), else sniff magic bytes.
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if !strings.HasPrefix(ct, "image/") && !strings.HasPrefix(ct, "video/") {
		ct = http.DetectContentType(data)
	}
	return data, ct, nil
}

func (c *Client) postJSON(ctx context.Context, url, apiKey string, body []byte) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRetryable, sanitizeErr(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, vividError(resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: bad response %s", ErrRetryable, clip(raw, 120))
	}
	return out, nil
}

// vividError maps the {"error":{"code":"NNNN","message":...}} envelope to our
// error classes; falls back to plain HTTP status mapping.
func vividError(status int, body []byte) error {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	code := ""
	msg := clip(body, 160)
	if json.Unmarshal(body, &e) == nil {
		code = strings.TrimSpace(e.Error.Code)
		if m := strings.TrimSpace(e.Error.Message); m != "" {
			msg = clip([]byte(m), 160)
		}
	}
	switch code {
	case "4011":
		return fmt.Errorf("%w: %s", ErrAuth, msg)
	case "4002":
		return fmt.Errorf("%w: 积分不足 %s", ErrRetryable, msg)
	case "4293", "5001":
		return fmt.Errorf("%w: %s", ErrRetryable, msg)
	case "4001":
		return fmt.Errorf("%w: %s", ErrInvalid, msg)
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: http %d", ErrAuth, status)
	case status == http.StatusTooManyRequests || status >= 500:
		return fmt.Errorf("%w: http %d %s", ErrRetryable, status, msg)
	default:
		return fmt.Errorf("%w: http %d %s", ErrInvalid, status, msg)
	}
}

// RefKindMime classifies a reference blob into the vivid "kind" (image /
// video / audio) plus a concrete mime. Magic bytes first (DetectContentType
// misses mov/m4a/ogg-audio), then sniffing, then a png default.
func RefKindMime(data []byte) (kind, mime string) {
	n := len(data)
	// ISOBMFF box: ftyp — mp4/mov/m4a share the container; the brand decides.
	if n >= 12 && string(data[4:8]) == "ftyp" {
		brand := string(data[8:12])
		switch {
		case strings.HasPrefix(brand, "M4A"), strings.HasPrefix(brand, "M4B"), strings.HasPrefix(brand, "F4A"), hasBMFFHandler(data, "soun") && !hasBMFFHandler(data, "vide"):
			return "audio", "audio/mp4"
		case brand == "qt  ":
			return "video", "video/quicktime"
		default:
			return "video", "video/mp4"
		}
	}
	// WebM/MKV EBML
	if n >= 4 && data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 && bytes.Contains(bytes.ToLower(data[:minInt(n, 4096)]), []byte("webm")) {
		return "video", "video/webm"
	}
	// MP3: ID3 tag or MPEG sync
	if n >= 3 && string(data[:3]) == "ID3" {
		return "audio", "audio/mpeg"
	}
	if n >= 4 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 &&
		(data[1]>>3)&3 != 1 && (data[1]>>1)&3 != 0 && data[2]>>4 != 0 && data[2]>>4 != 15 && (data[2]>>2)&3 != 3 {
		return "audio", "audio/mpeg"
	}
	// WAV / OGG
	if n >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "audio", "audio/wav"
	}
	if n >= 27 && string(data[:4]) == "OggS" && (bytes.Contains(data[:minInt(n, 4096)], []byte("OpusHead")) ||
		bytes.Contains(data[:minInt(n, 4096)], []byte("vorbis")) || bytes.Contains(data[:minInt(n, 4096)], []byte("Speex   ")) ||
		bytes.Contains(data[:minInt(n, 4096)], []byte("fLaC"))) {
		return "audio", "audio/ogg"
	}
	if m := http.DetectContentType(data); m == "image/png" || m == "image/jpeg" || m == "image/webp" {
		return "image", m
	}
	return "", ""
}

func hasBMFFHandler(data []byte, handler string) bool {
	data = data[:minInt(len(data), 4096)]
	for offset := 0; offset < len(data); {
		i := bytes.Index(data[offset:], []byte("hdlr"))
		if i < 0 {
			return false
		}
		i += offset
		if i+16 <= len(data) && string(data[i+12:i+16]) == handler {
			return true
		}
		offset = i + 4
	}
	return false
}

func isCreditError(s string) bool {
	m := strings.ToLower(s)
	return strings.Contains(m, "credit") || strings.Contains(m, "quota") ||
		strings.Contains(m, "insufficient") || strings.Contains(s, "积分不足") ||
		strings.Contains(s, "余额")
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitizeErr strips the upstream URL/host from a network error so a channel's
// private upstream URL never leaks into the event log / API response.
func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Client.Timeout"), strings.Contains(s, "timeout"):
		return "request timeout"
	case strings.Contains(s, "connection refused"):
		return "connection refused"
	case strings.Contains(s, "no such host"), strings.Contains(s, "dial tcp"), strings.Contains(s, "lookup "):
		return "cannot reach upstream"
	case strings.Contains(s, "tls"), strings.Contains(s, "TLS"), strings.Contains(s, "certificate"):
		return "TLS error"
	case strings.Contains(s, "EOF"), strings.Contains(s, "reset by peer"), strings.Contains(s, "broken pipe"):
		return "connection reset"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return strings.ToLower(ue.Op) + " upstream failed"
	}
	return "upstream request failed"
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
