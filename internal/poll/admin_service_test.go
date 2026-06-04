package poll_test

import (
	"context"
	"testing"
	"time"

	"github.com/enderu/seeurchin/internal/poll"
)

func TestForceAdvanceResolvesGracefully(t *testing.T) {
	ctx := context.Background()

	// Zero nominations: force-advance closes the poll empty (no winner), where a
	// host-driven Advance would error.
	t.Run("empty round1 closes", func(t *testing.T) {
		svc, _ := newSvc(t)
		p, _, err := svc.CreatePoll(ctx, baseInput())
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.ForceAdvance(ctx, p)
		if err != nil {
			t.Fatalf("ForceAdvance: %v", err)
		}
		if got.Status != poll.StatusClosed {
			t.Fatalf("status = %q, want closed", got.Status)
		}
		if got.WinnerNominationID != "" {
			t.Fatalf("unexpected winner %q", got.WinnerNominationID)
		}
	})

	// A single nomination is crowned the winner.
	t.Run("single nomination wins", func(t *testing.T) {
		svc, _ := newSvc(t)
		p, host, err := svc.CreatePoll(ctx, baseInput())
		if err != nil {
			t.Fatal(err)
		}
		nominate(t, svc, p, host, "solo")
		got, err := svc.ForceAdvance(ctx, p)
		if err != nil {
			t.Fatalf("ForceAdvance: %v", err)
		}
		if got.Status != poll.StatusClosed || got.WinnerNominationID == "" {
			t.Fatalf("want closed with winner, got status=%q winner=%q", got.Status, got.WinnerNominationID)
		}
	})

	// Two+ nominations advance to round 2, then to closed; advancing a closed
	// poll is a conflict.
	t.Run("normal advance then conflict", func(t *testing.T) {
		svc, _ := newSvc(t)
		p, host, err := svc.CreatePoll(ctx, baseInput())
		if err != nil {
			t.Fatal(err)
		}
		nominate(t, svc, p, host, "a", "b")
		got, err := svc.ForceAdvance(ctx, p)
		if err != nil {
			t.Fatalf("ForceAdvance to round2: %v", err)
		}
		if got.Status != poll.StatusRound2 {
			t.Fatalf("status = %q, want round2", got.Status)
		}
		got, err = svc.ForceAdvance(ctx, got)
		if err != nil {
			t.Fatalf("ForceAdvance to closed: %v", err)
		}
		if got.Status != poll.StatusClosed {
			t.Fatalf("status = %q, want closed", got.Status)
		}
		if _, err := svc.ForceAdvance(ctx, got); err == nil {
			t.Fatal("expected conflict advancing a closed poll")
		}
	})
}

func TestPurgeExpiredNoOpWhenDisabledOrRecent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)
	p, host, err := svc.CreatePoll(ctx, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	nominate(t, svc, p, host, "a")
	if _, err := svc.ForceAdvance(ctx, p); err != nil { // closes it now
		t.Fatal(err)
	}

	// Retention disabled (0) is always a no-op.
	if n, err := svc.PurgeExpired(ctx, time.Now(), 0); err != nil || n != 0 {
		t.Fatalf("disabled purge = %d (err %v), want 0", n, err)
	}
	// A poll that just closed is well within a 30-day window, so it survives.
	if n, err := svc.PurgeExpired(ctx, time.Now(), 30); err != nil || n != 0 {
		t.Fatalf("recent purge = %d (err %v), want 0", n, err)
	}
}

func TestListHistory(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)
	p1, host1, err := svc.CreatePoll(ctx, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	nominate(t, svc, p1, host1, "a", "b")

	in := baseInput()
	in.Title = "Second"
	if _, _, err := svc.CreatePoll(ctx, in); err != nil {
		t.Fatal(err)
	}

	hist, err := svc.ListHistory(ctx)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist))
	}
	// Find the first poll's summary and check its counts.
	for _, sum := range hist {
		if sum.Poll.ID == p1.ID {
			if sum.Counts.Participants != 1 || sum.Counts.Nominations != 2 {
				t.Fatalf("counts = %+v, want 1 participant / 2 nominations", sum.Counts)
			}
			return
		}
	}
	t.Fatal("first poll missing from history")
}
