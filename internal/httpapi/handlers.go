package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/enderu/seeurchin/internal/jellyfin"
	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/voting"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type methodView struct {
	Key           string          `json:"key"`
	Label         string          `json:"label"`
	DefaultConfig json.RawMessage `json:"default_config"`
}

func (s *Server) handleMethods(w http.ResponseWriter, _ *http.Request) {
	var out []methodView
	for _, m := range voting.All() {
		out = append(out, methodView{Key: m.Key(), Label: m.Label(), DefaultConfig: m.DefaultConfig()})
	}
	s.writeJSON(w, http.StatusOK, out)
}

type createPollReq struct {
	Title           string               `json:"title"`
	HostName        string               `json:"host_name"`
	LibraryScope    string               `json:"library_scope"`
	SubmissionRules poll.SubmissionRules `json:"submission_rules"`
	VotingMethod    string               `json:"voting_method"`
	VotingConfig    json.RawMessage      `json:"voting_config"`
	AllowGuests     bool                 `json:"allow_guests"`
	ResultsLive     bool                 `json:"results_live"`
}

func (s *Server) handleCreatePoll(w http.ResponseWriter, r *http.Request) {
	var req createPollReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	p, host, err := s.svc.CreatePoll(r.Context(), poll.CreatePollInput{
		Title:           req.Title,
		HostName:        req.HostName,
		LibraryScope:    poll.LibraryScope(req.LibraryScope),
		SubmissionRules: req.SubmissionRules,
		VotingMethod:    req.VotingMethod,
		VotingConfig:    req.VotingConfig,
		AllowGuests:     req.AllowGuests,
		ResultsLive:     req.ResultsLive,
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.setToken(w, r, p.ID, host.SessionToken)
	view, err := s.buildPollView(r.Context(), p, host)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleGetPoll(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.respondView(w, r, p, s.currentParticipant(r, p))
}

type joinReq struct {
	DisplayName string `json:"display_name"`
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if me := s.currentParticipant(r, p); me != nil {
		s.respondView(w, r, p, me) // already joined; idempotent
		return
	}
	var req joinReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	me, err := s.svc.JoinAsGuest(r.Context(), p, req.DisplayName)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.setToken(w, r, p.ID, me.SessionToken)
	s.respondView(w, r, p, me)
}

type libraryItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Type      string `json:"type"`
	Runtime   int    `json:"runtime_minutes"`
	ImageTag  string `json:"image_tag"`
	Nominated bool   `json:"nominated"`
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if _, ok := s.requireParticipant(w, r, p); !ok {
		return
	}

	types := scopeTypes(p.LibraryScope)
	switch strings.ToLower(r.URL.Query().Get("type")) {
	case "movie":
		types = []string{"Movie"}
	case "series":
		types = []string{"Series"}
	}
	// Load the whole library by default — Jellyfin returns all matching items
	// when no Limit is sent, and the frontend lazy-loads posters as you scroll.
	// An explicit ?limit= still caps it if ever needed.
	limit := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))

	items, total, err := s.jf.Search(r.Context(), jellyfin.SearchParams{
		Query:      strings.TrimSpace(r.URL.Query().Get("q")),
		Types:      types,
		Limit:      limit,
		StartIndex: start,
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}

	nominated := map[string]bool{}
	if noms, err := s.repo.ListNominations(r.Context(), p.ID); err == nil {
		for _, n := range noms {
			nominated[n.JellyfinItemID] = true
		}
	}

	// Collapse duplicate library entries for the same title — e.g. the same
	// media surfaced under two Jellyfin libraries shows up as distinct items
	// with different IDs. Key on type+title+year; prefer the copy that has a
	// poster, and keep the nominated flag sticky across the merge.
	out := make([]libraryItem, 0, len(items))
	seen := make(map[string]int, len(items))
	for _, it := range items {
		li := libraryItem{
			ID:        it.ID,
			Title:     it.Name,
			Year:      it.ProductionYear,
			Type:      it.Type,
			Runtime:   it.RuntimeMinutes(),
			ImageTag:  it.PrimaryImageTag(),
			Nominated: nominated[it.ID],
		}
		key := it.Type + "\x00" + strings.ToLower(strings.TrimSpace(it.Name)) + "\x00" + strconv.Itoa(it.ProductionYear)
		if idx, ok := seen[key]; ok {
			if li.Nominated {
				out[idx].Nominated = true
			}
			if out[idx].ImageTag == "" && li.ImageTag != "" {
				wasNominated := out[idx].Nominated
				out[idx] = li
				out[idx].Nominated = wasNominated
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, li)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

type nominateReq struct {
	ItemID string `json:"item_id"`
}

func (s *Server) handleNominate(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	me, ok := s.requireParticipant(w, r, p)
	if !ok {
		return
	}
	var req nominateReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	if _, err := s.svc.SubmitNomination(r.Context(), p, me, req.ItemID); err != nil {
		s.writeErr(w, err)
		return
	}
	s.broadcast(p.ID, "nominations")
	s.respondView(w, r, p, me)
}

func (s *Server) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	me, ok := s.requireParticipant(w, r, p)
	if !ok {
		return
	}
	if err := s.svc.WithdrawNomination(r.Context(), p, me, chi.URLParam(r, "id")); err != nil {
		s.writeErr(w, err)
		return
	}
	s.broadcast(p.ID, "nominations")
	s.respondView(w, r, p, me)
}

func (s *Server) handleAdvance(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	me, ok := s.requireParticipant(w, r, p)
	if !ok {
		return
	}
	if _, err := s.svc.Advance(r.Context(), p, me); err != nil {
		s.writeErr(w, err)
		return
	}
	s.broadcast(p.ID, "status")
	s.respondView(w, r, p, me)
}

type voteReq struct {
	Selections map[string]int `json:"selections"`
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	me, ok := s.requireParticipant(w, r, p)
	if !ok {
		return
	}
	var req voteReq
	if err := decodeJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	if err := s.svc.CastVotes(r.Context(), p, me, req.Selections); err != nil {
		s.writeErr(w, err)
		return
	}
	s.broadcast(p.ID, "votes")
	s.respondView(w, r, p, me)
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if !resultsVisible(p) {
		s.writeJSON(w, http.StatusForbidden, errResp{"results are not available yet"})
		return
	}
	noms, err := s.repo.ListNominations(r.Context(), p.ID)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	rv, err := s.computeResults(r.Context(), p, noms)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rv)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := s.jf.FetchImage(r.Context(), id, "Primary", r.URL.Query())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, errResp{"image unavailable"})
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// --- shared helpers ---

func (s *Server) requireParticipant(w http.ResponseWriter, r *http.Request, p *poll.Poll) (*poll.Participant, bool) {
	me := s.currentParticipant(r, p)
	if me == nil {
		s.writeJSON(w, http.StatusUnauthorized, errResp{"join the poll first"})
		return nil, false
	}
	return me, true
}

func (s *Server) respondView(w http.ResponseWriter, r *http.Request, p *poll.Poll, me *poll.Participant) {
	view, err := s.buildPollView(r.Context(), p, me)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, view)
}

func scopeTypes(scope poll.LibraryScope) []string {
	switch scope {
	case poll.ScopeMovies:
		return []string{"Movie"}
	case poll.ScopeSeries:
		return []string{"Series"}
	default:
		return []string{"Movie", "Series"}
	}
}
