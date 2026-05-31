package httpapi

import (
	"context"
	"encoding/json"

	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/voting"
)

// pollView is the client-facing representation of a poll's full state.
type pollView struct {
	Code              string               `json:"code"`
	Title             string               `json:"title"`
	Status            string               `json:"status"`
	LibraryScope      string               `json:"library_scope"`
	SubmissionRules   poll.SubmissionRules `json:"submission_rules"`
	MinToSubmit       int                  `json:"min_to_submit"`
	VotingMethod      string               `json:"voting_method"`
	VotingMethodLabel string               `json:"voting_method_label"`
	VotingConfig      json.RawMessage      `json:"voting_config"`
	AllowGuests       bool                 `json:"allow_guests"`
	ResultsLive       bool                 `json:"results_live"`
	ParticipantCount  int                  `json:"participant_count"`
	VoterCount        int                  `json:"voter_count"`
	Nominations       []nominationView     `json:"nominations"`
	Me                *meView              `json:"me"`
	Results           *resultsView         `json:"results,omitempty"`
	ShareURL          string               `json:"share_url"`
}

type nominationView struct {
	ID             string `json:"id"`
	ItemID         string `json:"item_id"`
	Title          string `json:"title"`
	Year           int    `json:"year"`
	Type           string `json:"type"`
	Runtime        int    `json:"runtime_minutes"`
	Overview       string `json:"overview"`
	ImageTag       string `json:"image_tag"`
	NominatorCount int    `json:"nominator_count"`
	MineNominated  bool   `json:"mine_nominated"`
}

type meView struct {
	ID              string         `json:"id"`
	DisplayName     string         `json:"display_name"`
	IsHost          bool           `json:"is_host"`
	NominationCount int            `json:"nomination_count"`
	HasVoted        bool           `json:"has_voted"`
	MySelections    map[string]int `json:"my_selections"`
}

type resultsView struct {
	Method  string         `json:"method"`
	Winners []resultEntry  `json:"winners"`
	Ranked  []resultEntry  `json:"ranked"`
	Rounds  []voting.RoundResult `json:"rounds,omitempty"`
}

type resultEntry struct {
	NominationID string  `json:"nomination_id"`
	Title        string  `json:"title"`
	Score        float64 `json:"score"`
}

// buildPollView assembles the full state for poll p as seen by participant me
// (which may be nil for a not-yet-joined visitor).
func (s *Server) buildPollView(ctx context.Context, p *poll.Poll, me *poll.Participant) (pollView, error) {
	participants, err := s.repo.ListParticipants(ctx, p.ID)
	if err != nil {
		return pollView{}, err
	}
	noms, err := s.repo.ListNominations(ctx, p.ID)
	if err != nil {
		return pollView{}, err
	}
	voterCount, err := s.repo.CountVoters(ctx, p.ID)
	if err != nil {
		return pollView{}, err
	}

	methodLabel := p.VotingMethod
	if m, ok := voting.Get(p.VotingMethod); ok {
		methodLabel = m.Label()
	}

	view := pollView{
		Code:              p.Code,
		Title:             p.Title,
		Status:            string(p.Status),
		LibraryScope:      string(p.LibraryScope),
		SubmissionRules:   p.SubmissionRules,
		MinToSubmit:       p.SubmissionRules.MinToSubmit(),
		VotingMethod:      p.VotingMethod,
		VotingMethodLabel: methodLabel,
		VotingConfig:      p.VotingConfig,
		AllowGuests:       p.AllowGuests,
		ResultsLive:       p.ResultsLive,
		ParticipantCount:  len(participants),
		VoterCount:        voterCount,
		ShareURL:          s.cfg.BaseURL + "/p/" + p.Code,
		Nominations:       []nominationView{}, // serialize empty as [] not null
	}

	for _, n := range noms {
		mine := false
		if me != nil {
			for _, nominator := range n.Nominators {
				if nominator == me.ID {
					mine = true
					break
				}
			}
		}
		view.Nominations = append(view.Nominations, nominationView{
			ID:             n.ID,
			ItemID:         n.JellyfinItemID,
			Title:          n.Snapshot.Title,
			Year:           n.Snapshot.Year,
			Type:           n.Snapshot.Type,
			Runtime:        n.Snapshot.Runtime,
			Overview:       n.Snapshot.Overview,
			ImageTag:       n.Snapshot.ImageTag,
			NominatorCount: len(n.Nominators),
			MineNominated:  mine,
		})
	}

	if me != nil {
		count, err := s.repo.CountNominationsByParticipant(ctx, p.ID, me.ID)
		if err != nil {
			return pollView{}, err
		}
		voted, err := s.repo.HasVoted(ctx, p.ID, me.ID)
		if err != nil {
			return pollView{}, err
		}
		view.Me = &meView{
			ID:              me.ID,
			DisplayName:     me.DisplayName,
			IsHost:          me.IsHost(),
			NominationCount: count,
			HasVoted:        voted,
		}
		if p.Status == poll.StatusRound2 || p.Status == poll.StatusClosed {
			votes, err := s.repo.ListVotes(ctx, p.ID)
			if err != nil {
				return pollView{}, err
			}
			sel := map[string]int{}
			for _, v := range votes {
				if v.ParticipantID == me.ID {
					sel[v.NominationID] = v.Value
				}
			}
			view.Me.MySelections = sel
		}
	}

	if resultsVisible(p) {
		rv, err := s.computeResults(ctx, p, noms)
		if err != nil {
			return pollView{}, err
		}
		view.Results = rv
	}

	return view, nil
}

// resultsVisible reports whether tallies may be shown: always once closed, and
// during round 2 when the host enabled live results.
func resultsVisible(p *poll.Poll) bool {
	return p.Status == poll.StatusClosed || (p.Status == poll.StatusRound2 && p.ResultsLive)
}

func (s *Server) computeResults(ctx context.Context, p *poll.Poll, noms []poll.Nomination) (*resultsView, error) {
	res, _, err := s.svc.Results(ctx, p)
	if err != nil {
		return nil, err
	}
	title := make(map[string]string, len(noms))
	for _, n := range noms {
		title[n.ID] = n.Snapshot.Title
	}
	// Initialize slices so an empty tally serializes as [] not null — the
	// frontend dereferences r.ranked / r.winners without a per-field guard.
	rv := &resultsView{
		Method:  res.Method,
		Rounds:  res.Rounds,
		Winners: []resultEntry{},
		Ranked:  []resultEntry{},
	}
	for _, r := range res.Ranked {
		rv.Ranked = append(rv.Ranked, resultEntry{NominationID: r.NominationID, Title: title[r.NominationID], Score: r.Score})
	}
	for _, id := range res.WinnerIDs {
		rv.Winners = append(rv.Winners, resultEntry{NominationID: id, Title: title[id]})
	}
	return rv, nil
}
