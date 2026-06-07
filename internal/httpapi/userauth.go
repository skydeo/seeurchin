package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const userCookieName = "seeurchin_user"

// userIdentity is the authenticated Jellyfin user carried (signed) in the
// seeurchin_user cookie. It is the single identity used both for gating the
// NAS-touching actions (poll creation, write-in request) and for authorizing
// admin access.
type userIdentity struct {
	UserID   string `json:"uid"`
	Name     string `json:"name"`
	JFAdmin  bool   `json:"jfAdmin"`
	IssuedAt int64  `json:"iat"`
}

// currentUser returns the authenticated Jellyfin identity from the request's
// signed cookie, or (nil, false) when absent or invalid.
func (s *Server) currentUser(r *http.Request) (*userIdentity, bool) {
	c, err := r.Cookie(userCookieName)
	if err != nil {
		return nil, false
	}
	raw, ok := s.sessions.VerifyValue(c.Value)
	if !ok {
		return nil, false
	}
	var id userIdentity
	if err := json.Unmarshal([]byte(raw), &id); err != nil || id.UserID == "" {
		return nil, false
	}
	return &id, true
}

func (s *Server) setUserCookie(w http.ResponseWriter, id userIdentity) error {
	id.IssuedAt = time.Now().Unix()
	payload, err := json.Marshal(id)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     userCookieName,
		Value:    s.sessions.SignValue(string(payload)),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.BaseURL, "https"),
		MaxAge:   60 * 60 * 24 * 7, // a week
	})
	return nil
}

func (s *Server) clearUserCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     userCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.BaseURL, "https"),
		MaxAge:   -1,
	})
}

// requireUser gates the NAS-touching actions. When Jellyfin login is disabled
// (the default), it passes through so the local guest flow keeps working; when
// enabled, it requires a valid user cookie.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.EnableUserLogin {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := s.currentUser(r); !ok {
			s.writeJSON(w, http.StatusUnauthorized, errResp{"sign in with Jellyfin first"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleUserSession lets the SPA decide what to render: whether login is enabled
// at all, and if so whether this browser is already authenticated.
func (s *Server) handleUserSession(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"login_enabled": s.cfg.EnableUserLogin,
		"authenticated": false,
		"display_name":  "",
		"is_admin":      false,
	}
	if id, ok := s.currentUser(r); ok {
		resp["authenticated"] = true
		resp["display_name"] = id.Name
		resp["is_admin"] = s.isAdminUser(id)
	}
	s.writeJSON(w, http.StatusOK, resp)
}

type userLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleUserLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.EnableUserLogin {
		s.writeJSON(w, http.StatusNotFound, errResp{"login is not enabled"})
		return
	}
	if !s.loginLimiter.allow(realIP(r)) {
		s.writeJSON(w, http.StatusTooManyRequests, errResp{"too many attempts; try again shortly"})
		return
	}
	var req userLoginReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		s.writeJSON(w, http.StatusBadRequest, errResp{"username and password are required"})
		return
	}
	res, err := s.jf.AuthenticateByName(r.Context(), strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		s.writeJSON(w, http.StatusUnauthorized, errResp{"incorrect username or password"})
		return
	}
	id := userIdentity{UserID: res.UserID, Name: res.Username, JFAdmin: res.IsAdmin}
	if err := s.setUserCookie(w, id); err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"display_name":  id.Name,
		"is_admin":      s.isAdminUser(&id),
	})
}

func (s *Server) handleUserLogout(w http.ResponseWriter, _ *http.Request) {
	s.clearUserCookie(w)
	s.writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

// --- per-poll passcode hashing ---

// passcodeHash returns a stable, non-reversible hash of a per-poll passcode,
// keyed by the server session secret (HMAC-SHA256, hex). An empty passcode
// hashes to "" so polls without one carry no gate. Stored in polls.passcode_hash.
func (s *Server) passcodeHash(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte("seeurchin-passcode:" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

// passcodeMatches reports whether a supplied passcode hashes to the stored hash,
// using a constant-time compare.
func (s *Server) passcodeMatches(stored, supplied string) bool {
	got := s.passcodeHash(supplied)
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}

// --- login rate limiting ---

// loginLimiter is a tiny fixed-window per-key limiter guarding the public login
// endpoint against brute force. It is best-effort defense-in-depth; Cloudflare
// rate-limiting is the real backstop in production.
type loginLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{hits: map[string][]time.Time{}, window: time.Minute, max: 10}
}

func (l *loginLimiter) allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// realIP returns the client IP recorded by the RealIP middleware (falling back
// to RemoteAddr), used as the login limiter key.
func realIP(r *http.Request) string {
	if ip := r.RemoteAddr; ip != "" {
		if i := strings.LastIndexByte(ip, ':'); i > 0 {
			return ip[:i]
		}
		return ip
	}
	return ""
}
