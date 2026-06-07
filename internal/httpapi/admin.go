package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/voting"
)

const adminCookieName = "seeurchin_admin"

// adminFingerprint derives a stable, non-reversible fingerprint of the admin
// token. The admin cookie stores this (signed); a request is authenticated only
// when its cookie's fingerprint matches the *current* token, so rotating
// SEEURCHIN_ADMIN_TOKEN invalidates every outstanding admin session.
func adminFingerprint(token string) string {
	sum := sha256.Sum256([]byte("seeurchin-admin:" + token))
	return hex.EncodeToString(sum[:])
}

// requireAdmin gates the admin data endpoints: 404 when the dashboard isn't
// configured (so its existence isn't even advertised), 401 when the caller has
// no valid admin cookie.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.AdminEnabled() {
			s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
			return
		}
		if !s.adminAuthed(r) {
			s.writeJSON(w, http.StatusUnauthorized, errResp{"admin login required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminAuthed reports whether the request carries a valid admin-session cookie
// for the current token.
func (s *Server) adminAuthed(r *http.Request) bool {
	c, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	fp, ok := s.sessions.VerifyValue(c.Value)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(fp), []byte(adminFingerprint(s.cfg.AdminToken))) == 1
}

func (s *Server) setAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    s.sessions.SignValue(adminFingerprint(s.cfg.AdminToken)),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.BaseURL, "https"),
		MaxAge:   60 * 60 * 24 * 7, // a week
	})
}

func (s *Server) clearAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.BaseURL, "https"),
		MaxAge:   -1,
	})
}

// handleAdminSession lets the SPA decide what to render: 404 when the dashboard
// is disabled, otherwise whether the caller is already logged in.
func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AdminEnabled() {
		s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"authenticated": s.adminAuthed(r)})
}

type adminLoginReq struct {
	Token string `json:"token"`
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AdminEnabled() {
		s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
		return
	}
	var req adminLoginReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Token)), []byte(s.cfg.AdminToken)) != 1 {
		s.writeJSON(w, http.StatusUnauthorized, errResp{"incorrect admin token"})
		return
	}
	s.setAdminCookie(w)
	s.writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AdminEnabled() {
		s.writeJSON(w, http.StatusNotFound, errResp{"not found"})
		return
	}
	s.clearAdminCookie(w)
	s.writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

// adminPollView is one row of the admin history list.
type adminPollView struct {
	Code              string     `json:"code"`
	Title             string     `json:"title"`
	Status            string     `json:"status"`
	VotingMethod      string     `json:"voting_method"`
	VotingMethodLabel string     `json:"voting_method_label"`
	DeadlineMode      string     `json:"deadline_mode,omitempty"`
	ParticipantCount  int        `json:"participant_count"`
	NominationCount   int        `json:"nomination_count"`
	VoterCount        int        `json:"voter_count"`
	WinnerTitle       string     `json:"winner_title,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ClosedAt          *time.Time `json:"closed_at,omitempty"`
}

func (s *Server) buildAdminPollView(ctx context.Context, sum poll.PollSummary) adminPollView {
	p := sum.Poll
	label := p.VotingMethod
	if m, ok := voting.Get(p.VotingMethod); ok {
		label = m.Label()
	}
	v := adminPollView{
		Code:              p.Code,
		Title:             p.Title,
		Status:            string(p.Status),
		VotingMethod:      p.VotingMethod,
		VotingMethodLabel: label,
		DeadlineMode:      string(p.DeadlineMode),
		ParticipantCount:  sum.Counts.Participants,
		NominationCount:   sum.Counts.Nominations,
		VoterCount:        sum.Counts.Voters,
		CreatedAt:         p.CreatedAt.UTC(),
		ClosedAt:          p.ClosedAt,
	}
	// Resolve the winning title for finished polls so the list reads as history.
	if p.Status == poll.StatusClosed {
		if res, noms, err := s.svc.Results(ctx, p); err == nil && len(res.WinnerIDs) > 0 {
			for _, n := range noms {
				if n.ID == res.WinnerIDs[0] {
					v.WinnerTitle = n.Snapshot.Title
					break
				}
			}
		}
	}
	return v
}

func (s *Server) handleAdminPolls(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.svc.ListHistory(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	out := make([]adminPollView, 0, len(summaries))
	for _, sum := range summaries {
		out = append(out, s.buildAdminPollView(r.Context(), sum))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleAdminPoll returns the full state of one poll (nominations and, when
// available, results) for the dashboard's drill-down. It reuses the standard
// poll view with no participant identity.
func (s *Server) handleAdminPoll(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	view, err := s.buildPollView(r.Context(), p, nil)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleAdminDeletePoll(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if err := s.svc.DeletePoll(r.Context(), p.ID); err != nil {
		s.writeErr(w, err)
		return
	}
	// Nudge any connected clients; their refetch will 404 on the gone poll.
	s.broadcast(p.ID, "status")
	s.writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleAdminAdvance(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if _, err := s.svc.ForceAdvance(r.Context(), p); err != nil {
		s.writeErr(w, err)
		return
	}
	s.broadcast(p.ID, "status")
	if p.Status == poll.StatusClosed {
		s.autoRequestWinners(r.Context(), p)
	}
	summaries, err := s.svc.ListHistory(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	for _, sum := range summaries {
		if sum.Poll.ID == p.ID {
			s.writeJSON(w, http.StatusOK, s.buildAdminPollView(r.Context(), sum))
			return
		}
	}
	s.writeJSON(w, http.StatusOK, errResp{}) // shouldn't happen
}

// RunRetentionSweeper periodically purges closed polls older than the configured
// retention window. It runs once immediately, then every interval until ctx is
// cancelled. Started from main only when retention is enabled.
func (s *Server) RunRetentionSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		n, err := s.svc.PurgeExpired(ctx, time.Now(), s.cfg.PollRetentionDays)
		if err != nil {
			log.Printf("retention sweep: %v", err)
		} else if n > 0 {
			log.Printf("retention: purged %d poll(s) older than %d days", n, s.cfg.PollRetentionDays)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
