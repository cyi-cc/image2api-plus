package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"backend/internal/model"
	"backend/internal/repo"
)

type APIKeyService struct {
	keys *repo.APIKeyRepository
}

func NewAPIKeyService(keys *repo.APIKeyRepository) *APIKeyService {
	return &APIKeyService{keys: keys}
}

func (s *APIKeyService) Current(ctx context.Context, userID string) (map[string]any, error) {
	keys, err := s.keys.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return map[string]any{"key": nil}, nil
	}
	key := keys[0]
	return map[string]any{
		"key": map[string]any{
			"id":           key.ID,
			"name":         key.Name,
			"key_preview":  key.KeyPreview,
			"created_at":   key.CreatedAt,
			"last_used_at": key.LastUsedAt,
		},
	}, nil
}

func (s *APIKeyService) Mint(ctx context.Context, userID string) (map[string]any, error) {
	plain, err := generatePlainAPIKey()
	if err != nil {
		return nil, err
	}
	key := &model.APIKey{
		ID:         "k-" + time.Now().Format("150405") + randomSuffix(2),
		UserID:     userID,
		Name:       "default",
		KeyPreview: previewAPIKey(plain),
		KeyHash:    hashAPIKey(plain),
		CreatedAt:  time.Now(),
	}
	if err := s.keys.ReplaceForUser(ctx, userID, key); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":      true,
		"key":     plain,
		"preview": key.KeyPreview,
	}, nil
}

func (s *APIKeyService) Revoke(ctx context.Context, userID string) error {
	return s.keys.DeleteByUserID(ctx, userID)
}

func (s *APIKeyService) MintNamed(ctx context.Context, userID, name string, replace bool) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}
	plain, err := generatePlainAPIKey()
	if err != nil {
		return nil, err
	}
	key := &model.APIKey{
		ID:         "k-" + time.Now().Format("150405") + randomSuffix(2),
		UserID:     userID,
		Name:       name,
		KeyPreview: previewAPIKey(plain),
		KeyHash:    hashAPIKey(plain),
		CreatedAt:  time.Now(),
	}
	if replace {
		if err := s.keys.ReplaceForUser(ctx, userID, key); err != nil {
			return nil, err
		}
	} else {
		if err := s.keys.Create(ctx, key); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"ok":      true,
		"key":     plain,
		"preview": key.KeyPreview,
		"id":      key.ID,
		"name":    key.Name,
	}, nil
}

func (s *APIKeyService) DeleteOne(ctx context.Context, userID, keyID string) error {
	if strings.TrimSpace(keyID) == "" {
		return errors.New("key id required")
	}
	return s.keys.DeleteByID(ctx, userID, keyID)
}

// generatePlainAPIKey mints a vivid-shaped key: vk_ + 48 lowercase hex chars
// (upstream channel api_keys use the same shape, so one key format rules
// everything — no per-direction prefixes).
func generatePlainAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "vk_" + hex.EncodeToString(b), nil
}

func previewAPIKey(plain string) string {
	if len(plain) <= 4 {
		return strings.Repeat("•", len(plain))
	}
	return "…" + plain[len(plain)-4:]
}

func hashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func randomSuffix(n int) string {
	return randomUpper(n)
}
