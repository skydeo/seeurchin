package poll_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/enderu/seeurchin/internal/poll"
	"github.com/enderu/seeurchin/internal/store"
)

// fakeResolver returns a movie for any item id, so nominations validate without
// a real Jellyfin.
type fakeResolver struct{}

func (fakeResolver) GetItem(_ context.Context, id string) (*poll.ResolvedItem, error) {
	return &poll.ResolvedItem{ID: id, Title: "Title " + id, Type: "Movie", Year: 2020}, nil
}

func newSvc(t *testing.T) (*poll.Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return poll.NewService(st, fakeResolver{}, 0), st
}

func baseInput() poll.CreatePollInput {
	return poll.CreatePollInput{
		Title:        "Movie Night",
		HostName:     "Alice",
		LibraryScope: poll.ScopeBoth,
		VotingMethod: "approval",
		AllowGuests:  true,
	}
}

func nominate(t *testing.T, svc *poll.Service, p *poll.Poll, by *poll.Participant, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := svc.SubmitNomination(context.Background(), p, by, id); err != nil {
			t.Fatalf("nominate %q: %v", id, err)
		}
	}
}

// near asserts that a duration in seconds is within tol of want.
func near(t *testing.T, label string, gotSec, want, tol int) {
	t.Helper()
	if gotSec < want-tol || gotSec > want+tol {
		t.Fatalf("%s = %ds, want ~%ds", label, gotSec, want)
	}
}

func TestQuickTimerLifecycle(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 60
	in.Round2DurationSec = 30

	p, host, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.DeadlineMode != poll.DeadlineQuick || p.Round1DurationSec != 60 {
		t.Fatalf("config not stored: %+v", p)
	}
	if p.Round1ClosesAt != nil {
		t.Fatal("round 1 should be armed (ClosesAt nil) until the host starts it")
	}

	// Start → running.
	if _, err := svc.StartTimer(ctx, p, host); err != nil {
		t.Fatalf("start: %v", err)
	}
	got, _ := st.GetPollByID(ctx, p.ID)
	if got.Round1ClosesAt == nil {
		t.Fatal("start should stamp round1_closes_at")
	}
	near(t, "remaining after start", int(time.Until(*got.Round1ClosesAt).Seconds()), 60, 3)

	// Pause → remembers remaining, clears ClosesAt.
	if _, err := svc.PauseTimer(ctx, got, host); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ = st.GetPollByID(ctx, p.ID)
	if got.Round1ClosesAt != nil {
		t.Fatal("pause should clear round1_closes_at")
	}
	near(t, "paused remaining", got.TimerPausedSec, 60, 3)

	// Extend while paused.
	if _, err := svc.ExtendTimer(ctx, got, host, 30); err != nil {
		t.Fatalf("extend: %v", err)
	}
	got, _ = st.GetPollByID(ctx, p.ID)
	near(t, "paused remaining after +30", got.TimerPausedSec, 90, 3)

	// Resume → re-stamps from remaining.
	if _, err := svc.StartTimer(ctx, got, host); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ = st.GetPollByID(ctx, p.ID)
	if got.TimerPausedSec != 0 || got.Round1ClosesAt == nil {
		t.Fatalf("resume should clear paused + restamp: %+v", got)
	}
	near(t, "remaining after resume", int(time.Until(*got.Round1ClosesAt).Seconds()), 90, 3)
}

func TestSweepAdvancesQuickRound1ToRound2(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 10
	in.Round2DurationSec = 20

	p, host, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	nominate(t, svc, p, host, "a", "b")
	if _, err := svc.StartTimer(ctx, p, host); err != nil {
		t.Fatalf("start: %v", err)
	}

	changed, err := svc.SweepDueTimers(ctx, time.Now().Add(11*time.Second))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed poll, got %d", len(changed))
	}
	got, _ := st.GetPollByID(ctx, p.ID)
	if got.Status != poll.StatusRound2 {
		t.Fatalf("status = %q, want round2", got.Status)
	}
	if got.Round2ClosesAt == nil {
		t.Fatal("entering round 2 should stamp round2_closes_at (quick)")
	}
	if got.TimerPausedSec != 0 {
		t.Fatalf("paused should reset on advance, got %d", got.TimerPausedSec)
	}
}

func TestSweepLoneNomineeWins(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 10

	p, host, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	nominate(t, svc, p, host, "solo")
	if _, err := svc.StartTimer(ctx, p, host); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.SweepDueTimers(ctx, time.Now().Add(11*time.Second)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, _ := st.GetPollByID(ctx, p.ID)
	if got.Status != poll.StatusClosed {
		t.Fatalf("status = %q, want closed", got.Status)
	}
	noms, _ := st.ListNominations(ctx, p.ID)
	if len(noms) != 1 || got.WinnerNominationID != noms[0].ID {
		t.Fatalf("lone nominee not crowned: winner=%q noms=%d", got.WinnerNominationID, len(noms))
	}
}

func TestSweepZeroNominationsClosesEmpty(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 10

	p, host, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.StartTimer(ctx, p, host); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.SweepDueTimers(ctx, time.Now().Add(11*time.Second)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got, _ := st.GetPollByID(ctx, p.ID)
	if got.Status != poll.StatusClosed {
		t.Fatalf("status = %q, want closed", got.Status)
	}
	if got.WinnerNominationID != "" {
		t.Fatalf("empty poll should have no winner, got %q", got.WinnerNominationID)
	}
}

func TestScheduledValidation(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	now := time.Now()

	past := now.Add(-time.Hour)
	in := baseInput()
	in.DeadlineMode = poll.DeadlineScheduled
	in.Round1ClosesAt = &past
	if _, _, err := svc.CreatePoll(ctx, in); err == nil {
		t.Fatal("expected error for past close time")
	}

	r1 := now.Add(2 * time.Hour)
	r2 := now.Add(time.Hour) // before r1 — invalid
	in = baseInput()
	in.DeadlineMode = poll.DeadlineScheduled
	in.Round1ClosesAt = &r1
	in.Round2ClosesAt = &r2
	if _, _, err := svc.CreatePoll(ctx, in); err == nil {
		t.Fatal("expected error when nominations close after voting")
	}

	r1 = now.Add(time.Hour)
	r2 = now.Add(2 * time.Hour)
	in = baseInput()
	in.DeadlineMode = poll.DeadlineScheduled
	in.Round1ClosesAt = &r1
	in.Round2ClosesAt = &r2
	p, _, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("valid scheduled poll rejected: %v", err)
	}
	if p.Round1ClosesAt == nil || p.Round2ClosesAt == nil {
		t.Fatal("scheduled close times not stored")
	}
}

func TestQuickRequiresADuration(t *testing.T) {
	svc, _ := newSvc(t)
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick // no durations
	if _, _, err := svc.CreatePoll(context.Background(), in); err == nil {
		t.Fatal("quick mode with no durations should error")
	}
}

func TestOnlyHostControlsTimer(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 30

	p, _, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	guest, err := svc.JoinAsGuest(ctx, p, "Bob")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := svc.StartTimer(ctx, p, guest); err == nil {
		t.Fatal("a guest should not be able to start the timer")
	}
}

func TestManualAdvanceUntimedStillNeedsTwo(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	p, host, err := svc.CreatePoll(ctx, baseInput()) // DeadlineNone
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	nominate(t, svc, p, host, "only-one")
	if _, err := svc.Advance(ctx, p, host); err == nil {
		t.Fatal("untimed poll should still require 2 nominations to advance manually")
	}
}

func TestEndNowOnTimedPollCrownsLoneNominee(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	in := baseInput()
	in.DeadlineMode = poll.DeadlineQuick
	in.Round1DurationSec = 60

	p, host, err := svc.CreatePoll(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	nominate(t, svc, p, host, "solo")
	if _, err := svc.StartTimer(ctx, p, host); err != nil {
		t.Fatalf("start: %v", err)
	}
	// "End now" is a host-driven Advance; on a timed poll it resolves gracefully.
	if _, err := svc.Advance(ctx, p, host); err != nil {
		t.Fatalf("advance: %v", err)
	}
	got, _ := st.GetPollByID(ctx, p.ID)
	if got.Status != poll.StatusClosed || got.WinnerNominationID == "" {
		t.Fatalf("end-now should crown the lone nominee: %+v", got)
	}
}
