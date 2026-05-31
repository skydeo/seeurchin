package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/enderu/seeurchin/internal/poll"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedPoll(t *testing.T, s *Store) (*poll.Poll, *poll.Participant) {
	t.Helper()
	p := &poll.Poll{
		ID:              poll.NewID(),
		Code:            "K7P2QX",
		Title:           "Friday Night",
		LibraryScope:    poll.ScopeBoth,
		Status:          poll.StatusRound1,
		SubmissionRules: poll.SubmissionRules{Min: 1, Max: 3},
		VotingMethod:    "approval",
		VotingConfig:    json.RawMessage(`{"votes_per_user":3}`),
		AllowGuests:     true,
	}
	host := &poll.Participant{ID: poll.NewID(), DisplayName: "Host"}
	if err := s.CreatePoll(context.Background(), p, host); err != nil {
		t.Fatalf("CreatePoll: %v", err)
	}
	return p, host
}

func TestCreateAndGetPoll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, host := seedPoll(t, s)

	if p.HostParticipantID != host.ID {
		t.Fatalf("host id not set on poll: %q", p.HostParticipantID)
	}
	got, err := s.GetPollByCode(ctx, "K7P2QX")
	if err != nil {
		t.Fatalf("GetPollByCode: %v", err)
	}
	if got.Title != "Friday Night" || got.VotingMethod != "approval" || got.SubmissionRules.Max != 3 {
		t.Fatalf("unexpected poll: %+v", got)
	}
	if got.HostParticipantID != host.ID {
		t.Fatalf("host mismatch: %q != %q", got.HostParticipantID, host.ID)
	}

	if _, err := s.GetPollByCode(ctx, "NOPE12"); err != poll.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := s.UpdatePollStatus(ctx, p.ID, poll.StatusRound2); err != nil {
		t.Fatalf("UpdatePollStatus: %v", err)
	}
	got, _ = s.GetPollByID(ctx, p.ID)
	if got.Status != poll.StatusRound2 {
		t.Fatalf("status = %q, want round2", got.Status)
	}
}

func TestNominationDedupeAndWithdraw(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, host := seedPoll(t, s)

	guest := &poll.Participant{ID: poll.NewID(), PollID: p.ID, DisplayName: "Guest", SessionToken: "tok"}
	if err := s.CreateParticipant(ctx, guest); err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}

	mk := func() *poll.Nomination {
		return &poll.Nomination{PollID: p.ID, JellyfinItemID: "item-1", Snapshot: poll.ItemSnapshot{Title: "Dune", Year: 2021, Type: "Movie"}}
	}
	// Two participants nominate the same item -> one nomination, two nominators.
	n1, err := s.AddNomination(ctx, mk(), host.ID)
	if err != nil {
		t.Fatalf("AddNomination host: %v", err)
	}
	n2, err := s.AddNomination(ctx, mk(), guest.ID)
	if err != nil {
		t.Fatalf("AddNomination guest: %v", err)
	}
	if n1.ID != n2.ID {
		t.Fatalf("expected dedupe to same nomination, got %q and %q", n1.ID, n2.ID)
	}

	noms, err := s.ListNominations(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListNominations: %v", err)
	}
	if len(noms) != 1 || len(noms[0].Nominators) != 2 {
		t.Fatalf("expected 1 nomination with 2 nominators, got %+v", noms)
	}

	if c, _ := s.CountNominationsByParticipant(ctx, p.ID, host.ID); c != 1 {
		t.Fatalf("host nomination count = %d, want 1", c)
	}

	// Host withdraws -> nomination survives (guest still nominates it).
	if err := s.WithdrawNomination(ctx, p.ID, n1.ID, host.ID); err != nil {
		t.Fatalf("WithdrawNomination host: %v", err)
	}
	noms, _ = s.ListNominations(ctx, p.ID)
	if len(noms) != 1 || len(noms[0].Nominators) != 1 {
		t.Fatalf("expected nomination to survive with 1 nominator, got %+v", noms)
	}
	// Guest withdraws -> nomination deleted.
	if err := s.WithdrawNomination(ctx, p.ID, n1.ID, guest.ID); err != nil {
		t.Fatalf("WithdrawNomination guest: %v", err)
	}
	noms, _ = s.ListNominations(ctx, p.ID)
	if len(noms) != 0 {
		t.Fatalf("expected nomination deleted, got %+v", noms)
	}
}

func TestReplaceVotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, host := seedPoll(t, s)
	n, _ := s.AddNomination(ctx, &poll.Nomination{PollID: p.ID, JellyfinItemID: "i1", Snapshot: poll.ItemSnapshot{Title: "A"}}, host.ID)

	if voted, _ := s.HasVoted(ctx, p.ID, host.ID); voted {
		t.Fatal("should not have voted yet")
	}
	if err := s.ReplaceVotes(ctx, p.ID, host.ID, []poll.Vote{{NominationID: n.ID, Value: 2}}); err != nil {
		t.Fatalf("ReplaceVotes: %v", err)
	}
	// Replace again with a different value -> old votes gone.
	if err := s.ReplaceVotes(ctx, p.ID, host.ID, []poll.Vote{{NominationID: n.ID, Value: 1}}); err != nil {
		t.Fatalf("ReplaceVotes 2: %v", err)
	}
	votes, _ := s.ListVotes(ctx, p.ID)
	if len(votes) != 1 || votes[0].Value != 1 {
		t.Fatalf("expected single vote value 1, got %+v", votes)
	}
	if voters, _ := s.CountVoters(ctx, p.ID); voters != 1 {
		t.Fatalf("voters = %d, want 1", voters)
	}
	if voted, _ := s.HasVoted(ctx, p.ID, host.ID); !voted {
		t.Fatal("should have voted")
	}
}
