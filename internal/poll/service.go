package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/enderu/seeurchin/internal/codes"
	"github.com/enderu/seeurchin/internal/voting"
)

// ItemResolver resolves a Jellyfin item ID into the details seeurchin snapshots
// at nomination time. It decouples the domain from the Jellyfin client.
type ItemResolver interface {
	GetItem(ctx context.Context, id string) (*ResolvedItem, error)
}

// ResolvedItem is the minimal library item the service needs.
type ResolvedItem struct {
	ID       string
	Title    string
	Year     int
	Type     string // "Movie" | "Series"
	Runtime  int
	Overview string
	ImageTag string
	Genres   []string
}

// Error is a service error carrying an HTTP-friendly status code.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errBad(format string, a ...any) *Error { return &Error{Code: 400, Msg: fmt.Sprintf(format, a...)} }
func errForbid(format string, a ...any) *Error {
	return &Error{Code: 403, Msg: fmt.Sprintf(format, a...)}
}
func errConflict(format string, a ...any) *Error {
	return &Error{Code: 409, Msg: fmt.Sprintf(format, a...)}
}

// Service holds the poll business logic.
type Service struct {
	repo    Repository
	items   ItemResolver
	codeLen int
}

// NewService constructs a Service. codeLen <= 0 uses the default code length.
func NewService(repo Repository, items ItemResolver, codeLen int) *Service {
	if codeLen <= 0 {
		codeLen = codes.DefaultLength
	}
	return &Service{repo: repo, items: items, codeLen: codeLen}
}

// CreatePollInput describes a new poll.
type CreatePollInput struct {
	Title            string
	HostName         string
	LibraryScope     LibraryScope
	SubmissionRules  SubmissionRules
	VotingMethod     string
	VotingConfig     json.RawMessage
	AllowGuests      bool
	ResultsLive      bool
	RevealNominators bool
	RevealScope      string
	Genres           []string // restrict nominations to these genres (empty = any)
}

// CreatePoll validates input, opens a poll directly into round 1, and creates
// the host participant. The returned host carries its session token.
func (s *Service) CreatePoll(ctx context.Context, in CreatePollInput) (*Poll, *Participant, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, nil, errBad("title is required")
	}
	hostName := strings.TrimSpace(in.HostName)
	if hostName == "" {
		return nil, nil, errBad("your name is required")
	}
	if in.LibraryScope == "" {
		in.LibraryScope = ScopeBoth
	}
	switch in.LibraryScope {
	case ScopeMovies, ScopeSeries, ScopeBoth:
	default:
		return nil, nil, errBad("invalid library scope %q", in.LibraryScope)
	}
	if err := validateRules(in.SubmissionRules); err != nil {
		return nil, nil, err
	}
	if in.RevealScope == "" {
		in.RevealScope = RevealWinner
	}
	if in.RevealScope != RevealWinner && in.RevealScope != RevealAll {
		return nil, nil, errBad("invalid reveal scope %q", in.RevealScope)
	}
	genres := cleanGenres(in.Genres)

	method, ok := voting.Get(in.VotingMethod)
	if !ok {
		return nil, nil, errBad("unknown voting method %q", in.VotingMethod)
	}
	cfg := in.VotingConfig
	if len(cfg) == 0 {
		cfg = method.DefaultConfig()
	}
	if err := method.ValidateConfig(cfg); err != nil {
		return nil, nil, errBad("voting config: %v", err)
	}

	code, err := codes.GenerateUnique(s.codeLen, 10, func(c string) (bool, error) {
		return s.repo.CodeExists(ctx, c)
	})
	if err != nil {
		return nil, nil, err
	}

	p := &Poll{
		ID:               NewID(),
		Code:             code,
		Title:            title,
		LibraryScope:     in.LibraryScope,
		Status:           StatusRound1,
		SubmissionRules:  in.SubmissionRules,
		VotingMethod:     in.VotingMethod,
		VotingConfig:     cfg,
		AllowGuests:      in.AllowGuests,
		ResultsLive:      in.ResultsLive,
		RevealNominators: in.RevealNominators,
		RevealScope:      in.RevealScope,
		Genres:           genres,
	}
	host := &Participant{
		ID:           NewID(),
		DisplayName:  hostName,
		SessionToken: NewID(),
		Role:         RoleHost,
	}
	if err := s.repo.CreatePoll(ctx, p, host); err != nil {
		return nil, nil, err
	}
	return p, host, nil
}

// cleanGenres trims, de-dupes (case-insensitively), and drops empty genre names,
// preserving the host's order.
func cleanGenres(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		key := strings.ToLower(g)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, g)
	}
	return out
}

// allowsGenres reports whether an item with the given genres may be nominated in
// a poll restricted to allowed (empty allowed = no restriction).
func allowsGenres(allowed, itemGenres []string) bool {
	if len(allowed) == 0 {
		return true
	}
	have := make(map[string]bool, len(itemGenres))
	for _, g := range itemGenres {
		have[strings.ToLower(strings.TrimSpace(g))] = true
	}
	for _, g := range allowed {
		if have[strings.ToLower(strings.TrimSpace(g))] {
			return true
		}
	}
	return false
}

func validateRules(r SubmissionRules) error {
	if r.Min < 0 || r.Max < 0 || r.Required < 0 {
		return errBad("submission rules cannot be negative")
	}
	if r.Required > 0 && (r.Min > 0 || r.Max > 0) {
		return errBad("set either a required count or a min/max, not both")
	}
	if r.Max > 0 && r.Min > r.Max {
		return errBad("min cannot exceed max")
	}
	return nil
}

// effectiveMax returns the per-participant nomination cap (0 = unlimited).
func (r SubmissionRules) effectiveMax() int {
	if r.Required > 0 {
		return r.Required
	}
	return r.Max
}

// MinToSubmit returns how many nominations a participant must make to be
// considered complete (for UI guidance).
func (r SubmissionRules) MinToSubmit() int {
	if r.Required > 0 {
		return r.Required
	}
	return r.Min
}

// JoinAsGuest creates a guest participant for the poll and returns it with a
// fresh session token.
func (s *Service) JoinAsGuest(ctx context.Context, p *Poll, displayName string) (*Participant, error) {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return nil, errBad("a display name is required")
	}
	if len(name) > 40 {
		name = name[:40]
	}
	if !p.AllowGuests {
		return nil, errForbid("this poll does not allow guests")
	}
	if p.Status == StatusClosed {
		return nil, errConflict("this poll has ended")
	}
	part := &Participant{
		ID:           NewID(),
		PollID:       p.ID,
		DisplayName:  name,
		SessionToken: NewID(),
		Role:         RoleParticipant,
	}
	if err := s.repo.CreateParticipant(ctx, part); err != nil {
		return nil, err
	}
	return part, nil
}

// SubmitNomination resolves the item, enforces scope and the per-participant
// max, and records the nomination.
func (s *Service) SubmitNomination(ctx context.Context, p *Poll, participant *Participant, itemID string) (*Nomination, error) {
	if p.Status != StatusRound1 {
		return nil, errConflict("nominations are closed")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, errBad("item id is required")
	}
	item, err := s.items.GetItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errBad("that title is not in the library")
	}
	if !scopeAllows(p.LibraryScope, item.Type) {
		return nil, errBad("%ss can't be nominated in this poll", strings.ToLower(item.Type))
	}
	if !allowsGenres(p.Genres, item.Genres) {
		return nil, errBad("this poll only allows: %s", strings.Join(p.Genres, ", "))
	}

	if max := p.SubmissionRules.effectiveMax(); max > 0 {
		count, err := s.repo.CountNominationsByParticipant(ctx, p.ID, participant.ID)
		if err != nil {
			return nil, err
		}
		// Allow re-affirming an existing nomination; only block brand-new ones
		// once at the cap.
		if count >= max {
			already, err := s.alreadyNominated(ctx, p.ID, itemID)
			if err != nil {
				return nil, err
			}
			if !already {
				return nil, errBad("you can nominate at most %d", max)
			}
		}
	}

	n := &Nomination{
		PollID:         p.ID,
		JellyfinItemID: itemID,
		Snapshot: ItemSnapshot{
			Title:    item.Title,
			Year:     item.Year,
			Type:     item.Type,
			Runtime:  item.Runtime,
			Overview: item.Overview,
			ImageTag: item.ImageTag,
		},
	}
	return s.repo.AddNomination(ctx, n, participant.ID)
}

func (s *Service) alreadyNominated(ctx context.Context, pollID, itemID string) (bool, error) {
	noms, err := s.repo.ListNominations(ctx, pollID)
	if err != nil {
		return false, err
	}
	for _, n := range noms {
		if n.JellyfinItemID == itemID {
			return true, nil
		}
	}
	return false, nil
}

// WithdrawNomination removes the participant as a nominator (deleting the
// nomination if nobody else nominated it).
func (s *Service) WithdrawNomination(ctx context.Context, p *Poll, participant *Participant, nominationID string) error {
	if p.Status != StatusRound1 {
		return errConflict("nominations are closed")
	}
	return s.repo.WithdrawNomination(ctx, p.ID, nominationID, participant.ID)
}

// Advance moves the poll to the next stage. Only the host may advance.
func (s *Service) Advance(ctx context.Context, p *Poll, participant *Participant) (*Poll, error) {
	if !participant.IsHost() {
		return nil, errForbid("only the host can advance the poll")
	}
	switch p.Status {
	case StatusRound1:
		noms, err := s.repo.ListNominations(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if len(noms) < 2 {
			return nil, errConflict("need at least 2 nominations to start voting")
		}
		// Methods that decide without a voting round (e.g. random) close
		// immediately: draw the winner once and freeze it.
		if dec, ok := voting.Decider(p.VotingMethod); ok {
			ids := make([]string, len(noms))
			for i, n := range noms {
				ids[i] = n.ID
			}
			winner, err := dec.Decide(p.VotingConfig, ids)
			if err != nil {
				return nil, errBad("%v", err)
			}
			if err := s.repo.SetPollWinner(ctx, p.ID, winner); err != nil {
				return nil, err
			}
			if err := s.repo.UpdatePollStatus(ctx, p.ID, StatusClosed); err != nil {
				return nil, err
			}
			p.WinnerNominationID = winner
			p.Status = StatusClosed
			return p, nil
		}
		if err := s.repo.UpdatePollStatus(ctx, p.ID, StatusRound2); err != nil {
			return nil, err
		}
		p.Status = StatusRound2
	case StatusRound2:
		if err := s.repo.UpdatePollStatus(ctx, p.ID, StatusClosed); err != nil {
			return nil, err
		}
		p.Status = StatusClosed
	default:
		return nil, errConflict("the poll cannot be advanced from %q", p.Status)
	}
	return p, nil
}

// CastVotes validates and records a participant's ballot for round 2.
func (s *Service) CastVotes(ctx context.Context, p *Poll, participant *Participant, selections map[string]int) error {
	if p.Status != StatusRound2 {
		return errConflict("voting is not open")
	}
	method, ok := voting.Get(p.VotingMethod)
	if !ok {
		return errBad("unknown voting method %q", p.VotingMethod)
	}
	noms, err := s.repo.ListNominations(ctx, p.ID)
	if err != nil {
		return err
	}
	allIDs := make([]string, 0, len(noms))
	var ownIDs []string
	for _, n := range noms {
		allIDs = append(allIDs, n.ID)
		for _, nominator := range n.Nominators {
			if nominator == participant.ID {
				ownIDs = append(ownIDs, n.ID)
				break
			}
		}
	}
	ballot := voting.Ballot{ParticipantID: participant.ID, Selections: selections}
	if err := method.ValidateBallot(p.VotingConfig, ballot, allIDs, ownIDs); err != nil {
		return errBad("%v", err)
	}
	votes := make([]Vote, 0, len(selections))
	for nomID, value := range selections {
		if value == 0 {
			continue
		}
		votes = append(votes, Vote{NominationID: nomID, Value: value})
	}
	return s.repo.ReplaceVotes(ctx, p.ID, participant.ID, votes)
}

// Results tallies the poll. nominations is returned alongside so callers can
// map IDs to titles.
func (s *Service) Results(ctx context.Context, p *Poll) (voting.Results, []Nomination, error) {
	method, ok := voting.Get(p.VotingMethod)
	if !ok {
		return voting.Results{}, nil, errBad("unknown voting method %q", p.VotingMethod)
	}
	noms, err := s.repo.ListNominations(ctx, p.ID)
	if err != nil {
		return voting.Results{}, nil, err
	}
	allIDs := make([]string, 0, len(noms))
	for _, n := range noms {
		allIDs = append(allIDs, n.ID)
	}
	votes, err := s.repo.ListVotes(ctx, p.ID)
	if err != nil {
		return voting.Results{}, nil, err
	}
	byParticipant := map[string]map[string]int{}
	for _, v := range votes {
		if byParticipant[v.ParticipantID] == nil {
			byParticipant[v.ParticipantID] = map[string]int{}
		}
		byParticipant[v.ParticipantID][v.NominationID] = v.Value
	}
	ballots := make([]voting.Ballot, 0, len(byParticipant))
	for pid, sel := range byParticipant {
		ballots = append(ballots, voting.Ballot{ParticipantID: pid, Selections: sel})
	}
	res, err := method.Tally(p.VotingConfig, allIDs, ballots)
	if err != nil {
		return voting.Results{}, nil, err
	}
	// A frozen winner (random pick, or any decided-once method) is authoritative
	// over the tally.
	if p.WinnerNominationID != "" {
		res.WinnerIDs = []string{p.WinnerNominationID}
	}
	return res, noms, nil
}

func scopeAllows(scope LibraryScope, itemType string) bool {
	switch scope {
	case ScopeMovies:
		return itemType == "Movie"
	case ScopeSeries:
		return itemType == "Series"
	default:
		return itemType == "Movie" || itemType == "Series"
	}
}

// IsNotFound reports whether err indicates a missing entity.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
