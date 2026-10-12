package service

import (
	"context"
	"errors"
	"strings"

	"backend/internal/repo"
)

type ImageAccessService struct {
	generatedRoot string
	showcase      *repo.ShowcaseRepository
	auth          *AuthService
}

func NewImageAccessService(generatedRoot string, showcase *repo.ShowcaseRepository, auth *AuthService) *ImageAccessService {
	return &ImageAccessService{
		generatedRoot: generatedRoot,
		showcase:      showcase,
		auth:          auth,
	}
}

// Resolve validates the path params and returns the object key (user/name).
// Existence isn't checked here — that's the storage GET's job (404 if missing).
func (s *ImageAccessService) Resolve(user, name string) (string, error) {
	user = strings.TrimSpace(user)
	name = strings.TrimSpace(name)
	if user == "" || name == "" {
		return "", errors.New("missing path params")
	}
	// :user and :name are single path segments (gin won't match "/"); guard
	// against traversal tokens anyway.
	if strings.Contains(user, "..") || strings.Contains(name, "..") ||
		strings.ContainsAny(user, `/\`) || strings.ContainsAny(name, `/\`) {
		return "", errors.New("invalid image path")
	}
	return user + "/" + name, nil
}

func (s *ImageAccessService) IsPublic(ctx context.Context, rel string) (bool, error) {
	// Branding assets (the site logo) are public — they render on the homepage /
	// header for logged-out visitors.
	if strings.HasPrefix(rel, "branding/") {
		return true, nil
	}
	return s.showcase.IsPublicFile(ctx, rel)
}

func (s *ImageAccessService) IsAuthorized(ctx context.Context, authHeader, sessionCookie, owner string) (bool, error) {
	// Private images may be requested by an <img> tag with the session cookie
	// OR by the SPA via a Bearer token. Admins may view anyone's images.
	if authHeader != "" {
		user, _, err := s.auth.CurrentUserFromBearer(ctx, authHeader)
		if err != nil {
			return false, err
		}
		if user == nil {
			return false, nil
		}
		if user.Role == "admin" {
			return true, nil
		}
		return s.auth.OwnsImageDir(user, owner), nil
	}
	return s.auth.IsAuthorizedForPrivateImage(ctx, sessionCookie, owner)
}
