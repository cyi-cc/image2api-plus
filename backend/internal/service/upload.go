package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// SaveOwnedImage stores a user-uploaded image under their owner directory so it
// behaves like a generated output: it shows up in the 我的图片 picker, can be
// dropped onto the infinite canvas, and is served by /images/<owner>/<name>.
// format must be a decoder-registered image format ("png", "jpeg", ...).
func (s *AdminReadService) SaveOwnedImage(ctx context.Context, owner, format string, data []byte) (string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || s.store == nil || !s.store.Configured() {
		return "", errors.New("storage not configured")
	}
	ext := strings.ToLower(strings.TrimSpace(format))
	contentType := ""
	switch ext {
	case "jpeg":
		ext = "jpg"
		contentType = "image/jpeg"
	case "png", "gif", "webp":
		contentType = "image/" + ext
	default:
		return "", errors.New("unsupported image format")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", errors.New("invalid image data")
	}
	key := fmt.Sprintf("%s/up_%d%s.%s", owner, time.Now().Unix(), randomUpper(6), ext)
	if err := s.store.Put(ctx, key, data, contentType); err != nil {
		return "", err
	}
	// Best-effort thumbnail, same convention as generated outputs.
	if thumb, terr := makeThumbnail(data); terr == nil {
		_ = s.store.Put(ctx, ThumbKey(key), thumb, "image/jpeg")
	}
	return key, nil
}
