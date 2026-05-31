// Package poll defines seeurchin's core domain: polls, their two-round
// lifecycle, participants, nominations, and votes, plus the service logic that
// enforces the rules. Persistence is delegated to a Repository implementation.
package poll

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned by Repository methods when an entity does not exist.
var ErrNotFound = errors.New("poll: not found")

// Status is a point in a poll's lifecycle.
type Status string

const (
	StatusDraft  Status = "draft"  // created, not yet opened
	StatusRound1 Status = "round1" // accepting nominations
	StatusRound2 Status = "round2" // accepting votes
	StatusClosed Status = "closed" // finished; results final
)

// LibraryScope restricts which Jellyfin item types may be nominated.
type LibraryScope string

const (
	ScopeMovies LibraryScope = "movie"
	ScopeSeries LibraryScope = "series"
	ScopeBoth   LibraryScope = "both"
)

// Role distinguishes the poll host from ordinary participants.
const (
	RoleHost        = "host"
	RoleParticipant = "participant"
)

// SubmissionRules bounds how many nominations a participant may make in round 1.
// If Required > 0 it pins the exact count; otherwise Min/Max apply (0 = no bound).
type SubmissionRules struct {
	Min      int `json:"min"`
	Max      int `json:"max"`
	Required int `json:"required"`
}

// Poll is a single movie-night vote.
type Poll struct {
	ID                string          `json:"id"`
	Code              string          `json:"code"`
	Title             string          `json:"title"`
	HostParticipantID string          `json:"host_participant_id"`
	LibraryScope      LibraryScope    `json:"library_scope"`
	Status            Status          `json:"status"`
	SubmissionRules   SubmissionRules `json:"submission_rules"`
	VotingMethod      string          `json:"voting_method"`
	VotingConfig      json.RawMessage `json:"voting_config"`
	AllowGuests       bool            `json:"allow_guests"`
	ResultsLive       bool            `json:"results_live"` // reveal tallies during round 2
	RevealNominators  bool            `json:"reveal_nominators"`
	RevealScope       string          `json:"reveal_scope"` // "winner" | "all" (when RevealNominators)
	Genres            []string        `json:"genres"`       // nomination pool restricted to these genres (empty = all)
	CreatedAt         time.Time       `json:"created_at"`
	Round1ClosesAt    *time.Time      `json:"round1_closes_at,omitempty"`
	Round2ClosesAt    *time.Time      `json:"round2_closes_at,omitempty"`
	// WinnerNominationID freezes the decided winner for non-deterministic or
	// multi-round methods (e.g. random) so the outcome is stable across reads.
	WinnerNominationID string     `json:"winner_nomination_id,omitempty"`
	DecidedAt          *time.Time `json:"decided_at,omitempty"`
}

// RevealScope values for showing who nominated titles on the results screen.
const (
	RevealWinner = "winner" // reveal nominators of the winner(s) only
	RevealAll    = "all"    // reveal nominators of every title
)

// Participant is someone taking part in a poll. Guests have an empty
// JellyfinUserID; Phase 2 Jellyfin login populates it.
type Participant struct {
	ID             string    `json:"id"`
	PollID         string    `json:"poll_id"`
	DisplayName    string    `json:"display_name"`
	SessionToken   string    `json:"-"`
	JellyfinUserID string    `json:"-"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

// IsHost reports whether the participant is the poll host.
func (p Participant) IsHost() bool { return p.Role == RoleHost }

// ItemSnapshot captures the Jellyfin item details at nomination time so the
// ballot renders even if the library changes later.
type ItemSnapshot struct {
	Title    string `json:"title"`
	Year     int    `json:"year"`
	Type     string `json:"type"`
	Runtime  int    `json:"runtime_minutes"`
	Overview string `json:"overview"`
	ImageTag string `json:"image_tag"`
}

// Nomination is a candidate title in a poll. Nominators lists the participants
// who nominated it (populated on read).
type Nomination struct {
	ID             string       `json:"id"`
	PollID         string       `json:"poll_id"`
	JellyfinItemID string       `json:"jellyfin_item_id"`
	Snapshot       ItemSnapshot `json:"snapshot"`
	CreatedAt      time.Time    `json:"created_at"`
	Nominators     []string     `json:"nominators"`
}

// Vote is one allocation by a participant to a nomination. Value's meaning
// depends on the poll's voting method (votes, rank, or score).
type Vote struct {
	ID            string    `json:"id"`
	PollID        string    `json:"poll_id"`
	ParticipantID string    `json:"participant_id"`
	NominationID  string    `json:"nomination_id"`
	Value         int       `json:"value"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewID returns a random 128-bit identifier as a 32-char hex string.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand should never fail
	}
	return hex.EncodeToString(b)
}
