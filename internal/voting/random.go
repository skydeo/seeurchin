package voting

import (
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
)

// Random picks the winner uniformly at random from the nominations — a "throw a
// dart" mode for groups who can't decide. It has no voting round: the host
// closes round 1 and a winner is drawn and frozen (see AutoDecider). Because the
// draw is persisted, the result is stable across reads.
type Random struct{}

func (Random) Key() string   { return "random" }
func (Random) Label() string { return "Random pick" }

func (Random) DefaultConfig() json.RawMessage { return json.RawMessage(`{}`) }

func (Random) ValidateConfig(_ json.RawMessage) error { return nil }

// ValidateBallot rejects ballots: a random poll never opens a voting round, so
// there is nothing to submit.
func (Random) ValidateBallot(_ json.RawMessage, b Ballot, _, _ []string) error {
	if len(b.Selections) > 0 {
		return fmt.Errorf("a random poll has no voting round")
	}
	return nil
}

// Tally reports the candidates with no scores; the winner is supplied separately
// from the persisted draw (see poll.Service.Results), so the tally itself stays
// deterministic.
func (Random) Tally(_ json.RawMessage, allIDs []string, _ []Ballot) (Results, error) {
	ranked := make([]Result, 0, len(allIDs))
	for _, id := range allIDs {
		ranked = append(ranked, Result{NominationID: id})
	}
	return Results{Method: "random", Ranked: ranked}, nil
}

// Decide draws a winning nomination uniformly at random from allIDs.
func (Random) Decide(_ json.RawMessage, allIDs []string) (string, error) {
	if len(allIDs) == 0 {
		return "", fmt.Errorf("no nominations to choose from")
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(len(allIDs))))
	if err != nil {
		return "", err
	}
	return allIDs[n.Int64()], nil
}

var _ AutoDecider = Random{}
