// Package custom implements a generic OpenAI-compatible upstream client. A
// "custom" model forwards generation to any OpenAI-compatible API: the upstream
// base_url + api_key live on a custom account (pool="custom"), the upstream model
// name on the model config (UpstreamModel). Calls go DIRECT (no tls-client, no
// proxy) — the upstream is a normal API with no anti-bot.
package custom

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrAuth              = errors.New("custom upstream auth failed")
	ErrQuotaExhausted    = errors.New("custom upstream quota exhausted")
	ErrTemporaryUpstream = errors.New("custom upstream temporary error")
)

type Client struct{}

func NewClient() *Client { return &Client{} }

// sanitizeErr strips the upstream URL/host from a network error so a user's
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

func httpClient() *http.Client { return &http.Client{Timeout: 30 * time.Minute} }

// GenerateImage calls the upstream OpenAI image API. With reference images it
// uses /v1/images/edits (multipart); otherwise /v1/images/generations. Returns
// the raw image bytes (decoded from b64_json, or downloaded from url).
func (c *Client) GenerateImage(ctx context.Context, baseURL, apiKey, model, prompt, size, quality string, refs [][]byte, downloadResult bool) ([]byte, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, "", ErrAuth
	}
	var req *http.Request
	var err error
	if len(refs) > 0 {
		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)
		_ = w.WriteField("model", model)
		_ = w.WriteField("prompt", prompt)
		// Ask the upstream for a URL (not base64) so we can pass it through directly.
		_ = w.WriteField("response_format", "url")
		if size != "" {
			_ = w.WriteField("size", size)
		}
		for i, r := range refs {
			fw, e := w.CreateFormFile("image[]", fmt.Sprintf("ref_%d.png", i+1))
			if e != nil {
				return nil, "", e
			}
			_, _ = fw.Write(r)
		}
		_ = w.Close()
		req, err = http.NewRequest(http.MethodPost, baseURL+"/v1/images/edits", body)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
	} else {
		payload := map[string]any{"model": model, "prompt": prompt, "n": 1, "response_format": "url"}
		if size != "" {
			payload["size"] = size
		}
		if quality != "" {
			payload["quality"] = quality
		}
		raw, _ := json.Marshal(payload)
		req, err = http.NewRequest(http.MethodPost, baseURL+"/v1/images/generations", bytes.NewReader(raw))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if e := mapStatus(resp.StatusCode, body); e != nil {
		return nil, "", e
	}
	return imageFromResponse(ctx, body, downloadResult)
}

// ── Vivid 协议 ──────────────────────────────────────────────────────────────
// Vivid 上游用自家 API:POST /v1/generate 返回 application/x-ndjson 流 —— 首行
// {"jobId":...,"status":"running"},等待期间每 25s 一个空行心跳,末行为终态
// (succeeded 带 result 预签名 URL / failed 带 error)。参考素材走两步:
// POST /v1/uploads {kind,mime} → PUT 直传预签名 URL → 拿 key 放进 refs。
// 断流可携 jobId 重发重连。错误体为 {"error":{"code":"4011|4001|4002|4293|5001"}}。

// GenerateVividImage submits an image job to a Vivid-protocol upstream.
// quality is the tier string the upstream expects ("1K"/"2K"/"4K"), ratio the
// "W:H" aspect. refs are pre-uploaded via /v1/uploads into key strings.
func (c *Client) GenerateVividImage(ctx context.Context, baseURL, apiKey, model, prompt, ratio, quality string, refs [][]byte, downloadResult bool) ([]byte, string, error) {
	payload, err := c.vividPayload(ctx, baseURL, apiKey, model, prompt, ratio, quality, 0, refs)
	if err != nil {
		return nil, "", err
	}
	return c.vividGenerate(ctx, baseURL, apiKey, payload, downloadResult)
}

// GenerateVividVideo submits a video job (duration required by ranged models)
// to a Vivid-protocol upstream.
func (c *Client) GenerateVividVideo(ctx context.Context, baseURL, apiKey, model, prompt, ratio, quality string, durationSeconds int, refs [][]byte, downloadResult bool) ([]byte, string, error) {
	payload, err := c.vividPayload(ctx, baseURL, apiKey, model, prompt, ratio, quality, durationSeconds, refs)
	if err != nil {
		return nil, "", err
	}
	return c.vividGenerate(ctx, baseURL, apiKey, payload, downloadResult)
}

func (c *Client) vividPayload(ctx context.Context, baseURL, apiKey, model, prompt, ratio, quality string, durationSeconds int, refs [][]byte) (map[string]any, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, ErrAuth
	}
	payload := map[string]any{"model": model, "prompt": prompt}
	if quality != "" {
		payload["quality"] = quality
	}
	if ratio != "" {
		payload["ratio"] = ratio
	}
	if durationSeconds > 0 {
		payload["duration"] = durationSeconds
	}
	if len(refs) > 0 {
		keys := make([]string, 0, len(refs))
		for _, ref := range refs {
			key, err := c.vividUploadRef(ctx, baseURL, apiKey, ref)
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		payload["refs"] = keys
	}
	return payload, nil
}

// vividUploadRef runs the two-step upload: POST /v1/uploads {kind:"image",mime}
// → PUT the bytes to the presigned url → returns the key for refs[].
func (c *Client) vividUploadRef(ctx context.Context, baseURL, apiKey string, data []byte) (string, error) {
	mime := http.DetectContentType(data)
	if mime == "" || mime == "application/octet-stream" {
		mime = "image/png"
	}
	raw, _ := json.Marshal(map[string]any{"kind": "image", "mime": mime})
	up, err := c.doJSON(ctx, http.MethodPost, baseURL+"/v1/uploads", apiKey, raw)
	if err != nil {
		return "", err
	}
	putURL := strings.TrimSpace(stringValue(up["url"]))
	key := strings.TrimSpace(stringValue(up["key"]))
	if putURL == "" || key == "" {
		return "", fmt.Errorf("%w: upload session missing url/key", ErrTemporaryUpstream)
	}
	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", mime) // must match the signed mime exactly
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: ref upload %d %s", ErrTemporaryUpstream, resp.StatusCode, clip(body, 120))
	}
	return key, nil
}

// vividGenerate POSTs /v1/generate and reads the NDJSON stream to the terminal
// frame. If the stream drops before terminal, it reconnects once with the
// jobId (same-request resume — no new task, no double charge).
func (c *Client) vividGenerate(ctx context.Context, baseURL, apiKey string, payload map[string]any, downloadResult bool) ([]byte, string, error) {
	var last map[string]any
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if sleepCtx(ctx, 2*time.Second) != nil {
				return nil, "", ctx.Err()
			}
		}
		term, err := c.vividStreamOnce(ctx, baseURL, apiKey, payload)
		if err == nil && term != nil {
			last = term
			break
		}
		if err != nil && !errors.Is(err, ErrTemporaryUpstream) {
			return nil, "", err
		}
		// Temporary error mid-stream: if we hold a jobId, reconnect mode retries
		// without creating a new task.
		if job := strings.TrimSpace(stringValue(payload["jobId"])); job != "" {
			payload = map[string]any{"jobId": job}
			continue
		}
		if attempt == 2 {
			return nil, "", err
		}
	}
	if last == nil {
		return nil, "", fmt.Errorf("%w: no terminal frame", ErrTemporaryUpstream)
	}
	status := strings.ToLower(strings.TrimSpace(stringValue(last["status"])))
	switch status {
	case "succeeded", "completed", "success":
		resultURL := strings.TrimSpace(stringValue(last["result"]))
		if resultURL == "" {
			return nil, "", fmt.Errorf("%w: succeeded frame missing result", ErrTemporaryUpstream)
		}
		if !downloadResult {
			return nil, resultURL, nil
		}
		data, err := c.vividDownload(ctx, resultURL)
		if err != nil {
			return nil, "", err
		}
		return data, resultURL, nil
	default:
		reason := strings.TrimSpace(stringValue(last["error"]))
		if reason == "" {
			reason = "upstream job " + status
		}
		if isCreditError(reason) || strings.Contains(reason, "积分不足") || strings.Contains(reason, "余额") {
			return nil, "", fmt.Errorf("%w: %s", ErrQuotaExhausted, clip([]byte(reason), 160))
		}
		return nil, "", errors.New("custom: " + clip([]byte(reason), 160))
	}
}

// vividStreamOnce performs one /v1/generate request and scans the NDJSON body
// to the last frame. Returns the terminal frame, or a temporary error on
// transport failure (caller's reconnect logic decides what to do with jobId).
func (c *Client) vividStreamOnce(ctx context.Context, baseURL, apiKey string, payload map[string]any) (map[string]any, error) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/generate", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wait-Seconds", "7200")
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
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
		// Remember the jobId for potential reconnect.
		if job := strings.TrimSpace(stringValue(frame["jobId"])); job != "" {
			payload["jobId"] = job
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	return last, nil
}

// vividError maps the {"error":{"code":"NNNN"}} envelope to our error classes.
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
		return fmt.Errorf("%w: %s", ErrQuotaExhausted, msg)
	case "4293", "5001":
		return fmt.Errorf("%w: %s", ErrTemporaryUpstream, msg)
	case "4001":
		return errors.New("custom: " + msg) // 参数/业务拒绝 → 永久错,不换号
	}
	return mapStatus(status, body)
}

// vividDownload fetches the presigned result URL — no auth header (it's signed).
func (c *Client) vividDownload(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: result download %d", ErrTemporaryUpstream, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty result", ErrTemporaryUpstream)
	}
	return data, nil
}

// GenerateVideo drives the upstream Sora-style async video API:
// POST /v1/videos → poll GET /v1/videos/{id} → GET /v1/videos/{id}/content.
// Reference frames (image-to-video / first-last frames) are sent as multipart
// input_reference[] files, matching the OpenAI videos API. When downloadResult
// is false it returns the upstream content URL instead.
func (c *Client) GenerateVideo(ctx context.Context, baseURL, apiKey, model, prompt, size string, seconds int, frames [][]byte, downloadResult bool) ([]byte, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, "", ErrAuth
	}
	var created map[string]any
	var err error
	if len(frames) > 0 {
		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)
		_ = w.WriteField("model", model)
		_ = w.WriteField("prompt", prompt)
		if size != "" {
			_ = w.WriteField("size", size)
		}
		if seconds > 0 {
			_ = w.WriteField("seconds", fmt.Sprintf("%d", seconds))
		}
		for i, f := range frames {
			fw, e := w.CreateFormFile("input_reference[]", fmt.Sprintf("frame_%d.png", i+1))
			if e != nil {
				return nil, "", e
			}
			_, _ = fw.Write(f)
		}
		_ = w.Close()
		created, err = c.doMultipart(ctx, baseURL+"/v1/videos", apiKey, body, w.FormDataContentType())
	} else {
		payload := map[string]any{"model": model, "prompt": prompt}
		if size != "" {
			payload["size"] = size
		}
		if seconds > 0 {
			payload["seconds"] = fmt.Sprintf("%d", seconds)
		}
		raw, _ := json.Marshal(payload)
		created, err = c.doJSON(ctx, http.MethodPost, baseURL+"/v1/videos", apiKey, raw)
	}
	if err != nil {
		return nil, "", err
	}
	jobID := strings.TrimSpace(stringValue(created["id"]))
	if jobID == "" {
		return nil, "", fmt.Errorf("%w: video create missing id", ErrTemporaryUpstream)
	}
	// Poll until terminal.
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		job, err := c.doJSON(ctx, http.MethodGet, baseURL+"/v1/videos/"+jobID, apiKey, nil)
		if err != nil {
			if errors.Is(err, ErrTemporaryUpstream) {
				if sleepCtx(ctx, 5*time.Second) != nil {
					return nil, "", ctx.Err()
				}
				continue
			}
			return nil, "", err
		}
		switch strings.ToLower(strings.TrimSpace(stringValue(job["status"]))) {
		case "completed", "succeeded", "success":
			contentURL := baseURL + "/v1/videos/" + jobID + "/content"
			if !downloadResult {
				return nil, contentURL, nil
			}
			data, err := c.download(ctx, contentURL, apiKey)
			if err != nil {
				return nil, "", err
			}
			return data, contentURL, nil
		case "failed", "error", "canceled", "cancelled":
			reason := stringValue(job["error"])
			if isCreditError(reason) {
				return nil, "", fmt.Errorf("%w: %s", ErrTemporaryUpstream, clip([]byte(reason), 160))
			}
			return nil, "", fmt.Errorf("custom: video %s", clip([]byte(reason), 160))
		}
		if sleepCtx(ctx, 5*time.Second) != nil {
			return nil, "", ctx.Err()
		}
	}
}

func (c *Client) doJSON(ctx context.Context, method, url, apiKey string, body []byte) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if e := mapStatus(resp.StatusCode, raw); e != nil {
		return nil, e
	}
	var out map[string]any
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: non-json: %s", ErrTemporaryUpstream, clip(raw, 120))
	}
	return out, nil
}

func (c *Client) doMultipart(ctx context.Context, url, apiKey string, body io.Reader, contentType string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if e := mapStatus(resp.StatusCode, raw); e != nil {
		return nil, e
	}
	var out map[string]any
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: non-json: %s", ErrTemporaryUpstream, clip(raw, 120))
	}
	return out, nil
}

func (c *Client) download(ctx context.Context, url, apiKey string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: download %d", ErrTemporaryUpstream, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty download", ErrTemporaryUpstream)
	}
	return data, nil
}

// imageBytesFromResponse extracts image bytes from an OpenAI images response:
// data[0].b64_json (preferred) or data[0].url (downloaded).
// imageFromResponse parses an OpenAI image response and returns the upstream URL.
// We always request response_format=url, so the response must carry a URL — a
// base64-only response is treated as an error (no base64 pass-through). With
// downloadResult=false the URL is returned directly (no download).
func imageFromResponse(ctx context.Context, body []byte, downloadResult bool) ([]byte, string, error) {
	var out struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Data) == 0 {
		return nil, "", fmt.Errorf("%w: bad image response: %s", ErrTemporaryUpstream, clip(body, 160))
	}
	url := strings.TrimSpace(out.Data[0].URL)
	if url == "" {
		return nil, "", fmt.Errorf("%w: image response had no url (upstream ignored response_format=url)", ErrTemporaryUpstream)
	}
	if !downloadResult {
		return nil, url, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrTemporaryUpstream, sanitizeErr(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return raw, url, err
}

func mapStatus(status int, body []byte) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == 401 || status == 403:
		return fmt.Errorf("%w: %d %s", ErrAuth, status, clip(body, 160))
	case status == 429:
		// Custom upstreams have NO "quota exhausted" lock — a 429 is just rate
		// limiting, treated as a temporary error (fail over, account stays active).
		return fmt.Errorf("%w: 429 %s", ErrTemporaryUpstream, clip(body, 160))
	case status >= 500:
		return fmt.Errorf("%w: %d %s", ErrTemporaryUpstream, status, clip(body, 160))
	default:
		if isCreditError(string(body)) {
			return fmt.Errorf("%w: %s", ErrTemporaryUpstream, clip(body, 160))
		}
		return fmt.Errorf("custom: %d %s", status, clip(body, 160))
	}
}

func isCreditError(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "insufficient") || strings.Contains(s, "quota") ||
		strings.Contains(s, "credit") || strings.Contains(s, "balance")
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return strings.TrimSpace(string(b))
	}
}

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n]
	}
	return s
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
