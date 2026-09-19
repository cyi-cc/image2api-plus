package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/config"
	"backend/internal/model"
	"backend/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	cfg     *config.Config
	auth    *service.AuthService
	limiter *service.RateLimitService
}

func NewAuthHandler(cfg *config.Config, auth *service.AuthService, limiter *service.RateLimitService) *AuthHandler {
	return &AuthHandler{
		cfg:     cfg,
		auth:    auth,
		limiter: limiter,
	}
}

func (h *AuthHandler) Config(c *gin.Context) {
	data, err := h.auth.AuthConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load auth config"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *AuthHandler) SendCode(c *gin.Context) {
	var body struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	ip := clientIP(c)
	if err := h.enforceRateLimit(c, "auth:send-code:ip:"+ip, 5, time.Hour); err != nil {
		return
	}
	if email, err := service.ValidateEmail(body.Email); err == nil {
		if err := h.enforceRateLimit(c, "auth:send-code:email:"+email, 3, 10*time.Minute); err != nil {
			return
		}
	}
	if err := h.auth.SendCode(c.Request.Context(), body.Email, body.Purpose); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Register(c *gin.Context) {
	var body struct {
		Email      string `json:"email"`
		Username   string `json:"username"`
		Name       string `json:"name"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
		EmailCode  string `json:"email_code"`
		Code       string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		username = strings.TrimSpace(body.Name)
	}
	if err := h.enforceRateLimit(c, "auth:register:ip:"+clientIP(c), 10, time.Hour); err != nil {
		return
	}
	emailCode := strings.TrimSpace(body.EmailCode)
	if emailCode == "" {
		emailCode = strings.TrimSpace(body.Code)
	}
	user, token, session, err := h.auth.Register(
		c.Request.Context(),
		body.Email,
		username,
		body.Password,
		body.InviteCode,
		emailCode,
		clientIP(c),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	h.writeSession(c, token, session, user)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var body struct {
		Identifier string `json:"identifier"`
		Email      string `json:"email"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}

	identifier := strings.TrimSpace(body.Identifier)
	if identifier == "" {
		if strings.TrimSpace(body.Email) != "" {
			identifier = strings.TrimSpace(body.Email)
		} else {
			identifier = strings.TrimSpace(body.Username)
		}
	}
	if identifier == "" || body.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "账号或密码不能为空"})
		return
	}
	ip := clientIP(c)
	if err := h.enforceRateLimit(c, "auth:login:ip:"+ip, 20, 15*time.Minute); err != nil {
		return
	}
	if normalized, err := service.ValidateLoginIdentifier(identifier); err == nil {
		if err := h.enforceRateLimit(c, "auth:login:target:"+ip+":"+strings.ToLower(normalized), 8, 15*time.Minute); err != nil {
			return
		}
	}

	user, token, session, err := h.auth.Login(c.Request.Context(), identifier, body.Password, ip)
	if err != nil {
		if writeLoginLocked(c, err) {
			return
		}
		if err == service.ErrAuthFailed {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	h.writeSession(c, token, session, user)
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var body struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		EmailCode string `json:"email_code"`
		Code      string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	ip := clientIP(c)
	if err := h.enforceRateLimit(c, "auth:reset:ip:"+ip, 5, time.Hour); err != nil {
		return
	}
	if email, err := service.ValidateEmail(body.Email); err == nil {
		if err := h.enforceRateLimit(c, "auth:reset:email:"+email, 5, time.Hour); err != nil {
			return
		}
	}
	emailCode := strings.TrimSpace(body.EmailCode)
	if emailCode == "" {
		emailCode = strings.TrimSpace(body.Code)
	}
	if err := h.auth.ResetPassword(c.Request.Context(), body.Email, body.Password, emailCode, ip); err != nil {
		if writeLoginLocked(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	if err := h.enforceRateLimit(c, "auth:change-password:user:"+user.ID, 10, 30*time.Minute); err != nil {
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		Current         string `json:"current"`
		NewPassword     string `json:"new_password"`
		Password        string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	current := strings.TrimSpace(body.CurrentPassword)
	if current == "" {
		current = strings.TrimSpace(body.Current)
	}
	next := strings.TrimSpace(body.NewPassword)
	if next == "" {
		next = body.Password
	}
	if err := h.auth.ChangePassword(c.Request.Context(), user.ID, current, next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Checkin(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	result, err := h.auth.Checkin(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"already": result.Already,
		"awarded": result.Awarded,
		"streak":  result.Streak,
		"credits": result.Credits,
	})
}

func (h *AuthHandler) Invites(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	items, err := h.auth.InviteList(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load invites"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "reward": h.auth.InviteReward(c.Request.Context())})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token := service.ParseBearer(c.GetHeader("Authorization"))
	if token == "" {
		token = readCookie(c, h.cfg.SessionCookieName)
	}
	_ = h.auth.Logout(c.Request.Context(), token)
	c.SetCookie(h.cfg.SessionCookieName, "", -1, "/", "", h.cfg.CookieSecure, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userValue, ok := c.Get("current_user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	sessionValue, ok := c.Get("current_session")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}

	user, _ := userValue.(*model.User)
	session, _ := sessionValue.(*service.SessionPayload)
	if user == nil || session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "账号或密码错误"})
		return
	}
	publicUser, err := h.auth.PublicUser(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load user profile"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"expires_at": session.ExpiresAt,
		"user":       publicUser,
	})
}

// writeLoginLocked maps a LoginGuard lockout error to HTTP 429 with a
// Retry-After header (mirrors Python api/auth.py:226-237). Returns true when it
// handled the error so the caller stops processing.
func writeLoginLocked(c *gin.Context, err error) bool {
	var locked *service.LoginLockedError
	if errors.As(err, &locked) {
		c.Header("Retry-After", strconv.Itoa(locked.RetryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{"detail": locked.Error()})
		return true
	}
	return false
}

// clientIP resolves the caller for rate limiting. It must NOT read
// client-supplied X-Forwarded-For / X-Real-Ip directly: the first XFF value
// is attacker-controlled and rotating it resets every throttle bucket.
// gin's ClientIP walks the header chain right-to-left and only honors
// forwarding headers from the trusted proxies configured in the router
// (the in-stack traefik/nginx on private docker networks).
func clientIP(c *gin.Context) string {
	return c.ClientIP()
}

func (h *AuthHandler) enforceRateLimit(c *gin.Context, bucket string, limit int64, window time.Duration) error {
	if h.limiter == nil {
		return nil
	}
	if err := h.limiter.Enforce(c.Request.Context(), bucket, limit, window); err != nil {
		if errors.Is(err, service.ErrRateLimited) {
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": err.Error()})
			return err
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "rate limiter unavailable"})
		return err
	}
	return nil
}

func (h *AuthHandler) writeSession(c *gin.Context, token string, session *service.SessionPayload, user *model.User) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.SessionCookieName, token, int(h.cfg.SessionTTL.Seconds()), "/", "", h.cfg.CookieSecure, true)
	publicUser, err := h.auth.PublicUser(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load user profile"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"token":      token,
		"expires_at": session.ExpiresAt,
		"user":       publicUser,
	})
}
