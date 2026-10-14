package custom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// vividServer serves /v1/uploads, /v1/generate (ndjson) and a fake presigned
// result URL, all under one httptest server.
func vividServer(t *testing.T, frames []string, httpStatus int, errCode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/uploads":
			var req struct {
				Kind string `json:"kind"`
				Mime string `json:"mime"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Kind != "image" || req.Mime == "" {
				t.Errorf("bad upload request: %+v", req)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"uuid":"u1","key":"u/refs/image/u1.png","url":"%s","expiresAt":9999999999}`,
				// presigned URL points back at this server's /put endpoint
				"http://"+r.Host+"/put")
		case "/put":
			if r.Method != http.MethodPut {
				t.Errorf("expected PUT, got %s", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
				t.Errorf("bad content-type %q", ct)
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case "/v1/generate":
			if httpStatus != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(httpStatus)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":"boom"}}`, errCode)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			for _, f := range frames {
				fmt.Fprintln(w, f)
				fmt.Fprintln(w) // heartbeat line
			}
		case "/result.png":
			w.Write([]byte("PNGDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestVividGenerateImageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/generate":
			w.Header().Set("Content-Type", "application/x-ndjson")
			fmt.Fprintln(w, `{"jobId":"j1","status":"running"}`)
			fmt.Fprintln(w) // heartbeat
			fmt.Fprintf(w, `{"jobId":"j1","status":"succeeded","result":"http://%s/result.png","creditsCharged":10}`+"\n", r.Host)
		case "/result.png":
			w.Write([]byte("PNGDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	data, url, err := NewClient().GenerateVividImage(context.Background(), srv.URL, "vk_test", "m1", "hi", "1:1", "1K", nil, true)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if string(data) != "PNGDATA" {
		t.Fatalf("data=%q", data)
	}
	if url == "" {
		t.Fatal("empty url")
	}
}

func TestVividGenerateImageURLPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"jobId":"j1","status":"running"}`)
		fmt.Fprintln(w, `{"jobId":"j1","status":"succeeded","result":"https://fs.example.com/x.png"}`)
	}))
	defer srv.Close()
	data, url, err := NewClient().GenerateVividImage(context.Background(), srv.URL, "vk_test", "m1", "hi", "1:1", "1K", nil, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if data != nil {
		t.Fatalf("expected no bytes, got %d", len(data))
	}
	if url != "https://fs.example.com/x.png" {
		t.Fatalf("url=%q", url)
	}
}

func TestVividGenerateFailedFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"jobId":"j1","status":"running"}`)
		fmt.Fprintln(w, `{"jobId":"j1","status":"failed","error":"积分不足","refunded":true}`)
	}))
	defer srv.Close()
	_, _, err := NewClient().GenerateVividImage(context.Background(), srv.URL, "vk_test", "m1", "hi", "1:1", "1K", nil, false)
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("want quota, got %v", err)
	}
}

func TestVividErrorCodes(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{401, "4011", ErrAuth},
		{402, "4002", ErrQuotaExhausted},
		{429, "4293", ErrTemporaryUpstream},
		{500, "5001", ErrTemporaryUpstream},
	}
	for _, tc := range cases {
		srv := vividServer(t, nil, tc.status, tc.code)
		_, _, err := NewClient().GenerateVividImage(context.Background(), srv.URL, "vk_test", "m1", "hi", "1:1", "1K", nil, false)
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("code %s: want %v, got %v", tc.code, tc.want, err)
		}
	}
}

func TestVividRefKindMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		kind string
	}{
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}, "image"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4, 5, 6}, "image"},
		{"mp4", []byte{0, 0, 0, 0, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, "video"},
		{"mov", []byte{0, 0, 0, 0, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '}, "video"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 1, 2, 3, 4}, "video"},
		{"mp3-id3", []byte{'I', 'D', '3', 4, 0, 0, 1, 2}, "audio"},
		{"mp3-sync", []byte{0xFF, 0xFB, 1, 2, 3, 4}, "audio"},
		{"wav", []byte{'R', 'I', 'F', 'F', 1, 2, 3, 4, 'W', 'A', 'V', 'E'}, "audio"},
		{"ogg", []byte{'O', 'g', 'g', 'S', 1, 2, 3, 4}, "audio"},
		{"m4a", []byte{0, 0, 0, 0, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, "audio"},
		{"empty", []byte{}, "image"},
	}
	for _, tc := range cases {
		kind, mime := vividRefKindMime(tc.data)
		if kind != tc.kind {
			t.Errorf("%s: kind=%s mime=%s", tc.name, kind, mime)
		}
	}
}

func TestVividUploadRefKindVideo(t *testing.T) {
	var gotKind, gotMime string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/uploads" {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotKind, _ = req["kind"].(string)
			gotMime, _ = req["mime"].(string)
			fmt.Fprintf(w, `{"key":"k1","url":"http://%s/up"}`, r.Host)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	webm := []byte{0x1A, 0x45, 0xDF, 0xA3, 1, 2, 3, 4, 5, 6, 7, 8}
	key, err := NewClient().vividUploadRef(context.Background(), srv.URL, "vk", webm)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if key != "k1" || gotKind != "video" || gotMime != "video/webm" {
		t.Fatalf("key=%q kind=%q mime=%q", key, gotKind, gotMime)
	}
}

func TestVividUploadRefFlow(t *testing.T) {
	var putOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/uploads":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"key":"k123","url":"http://%s/up"}`, r.Host)
		case "/up":
			if r.Method == http.MethodPut {
				putOK = true
			}
			w.WriteHeader(http.StatusOK)
		case "/v1/generate":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			refs, _ := req["refs"].([]any)
			if len(refs) != 1 || refs[0] != "k123" {
				t.Errorf("refs=%v", req["refs"])
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			fmt.Fprintln(w, `{"jobId":"j1","status":"succeeded","result":"https://x/1.png"}`)
		}
	}))
	defer srv.Close()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // fake png magic
	_, url, err := NewClient().GenerateVividImage(context.Background(), srv.URL, "vk_test", "m1", "hi", "1:1", "1K", [][]byte{png}, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !putOK {
		t.Fatal("PUT never happened")
	}
	if url == "" {
		t.Fatal("no url")
	}
}
