package voting

import (
	"encoding/json"
	"fmt"
)

// Score implements score (range) voting: each voter rates every nomination
// from 0..MaxScore. The aggregate is the total of all scores by default, or
// the average across voters who rated the option.
type Score struct{}

type scoreConfig struct {
	MaxScore      int    `json:"max_score"`
	AllowSelfVote bool   `json:"allow_self_vote"`
	Aggregate     string `json:"aggregate"`                // "total" (default) | "average"
	MaxSelfVotes  *int   `json:"max_self_votes,omitempty"` // nil = use AllowSelfVote; <0 unlimited; 0 none; N cap
}

func (Score) Key() string   { return "score" }
func (Score) Label() string { return "Star / score rating" }

func (Score) DefaultConfig() json.RawMessage {
	return mustJSON(scoreConfig{MaxScore: 5, AllowSelfVote: true, Aggregate: "total"})
}

func parseScoreConfig(raw json.RawMessage) (scoreConfig, error) {
	cfg := scoreConfig{MaxScore: 5, AllowSelfVote: true, Aggregate: "total"}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("score config: %w", err)
		}
	}
	if cfg.Aggregate == "" {
		cfg.Aggregate = "total"
	}
	return cfg, nil
}

func (Score) ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseScoreConfig(raw)
	if err != nil {
		return err
	}
	if cfg.MaxScore < 2 {
		return fmt.Errorf("max_score must be >= 2")
	}
	if cfg.Aggregate != "total" && cfg.Aggregate != "average" {
		return fmt.Errorf("aggregate must be \"total\" or \"average\"")
	}
	return nil
}

func (Score) ValidateBallot(raw json.RawMessage, b Ballot, allIDs, ownIDs []string) error {
	cfg, err := parseScoreConfig(raw)
	if err != nil {
		return err
	}
	all := toSet(allIDs)
	own := toSet(ownIDs)
	selfLimit := selfVoteLimit(cfg.AllowSelfVote, cfg.MaxSelfVotes)
	selfScored := 0
	for id, s := range b.Selections {
		if !all[id] {
			return fmt.Errorf("unknown nomination %q", id)
		}
		if s < 0 || s > cfg.MaxScore {
			return fmt.Errorf("score for %q must be between 0 and %d", id, cfg.MaxScore)
		}
		if own[id] && s > 0 {
			selfScored++
		}
	}
	if selfLimit == 0 && selfScored > 0 {
		return fmt.Errorf("scoring your own nomination is not allowed")
	}
	if selfLimit > 0 && selfScored > selfLimit {
		return fmt.Errorf("score at most %d of your own nomination(s)", selfLimit)
	}
	return nil
}

func (Score) Tally(raw json.RawMessage, allIDs []string, ballots []Ballot) (Results, error) {
	cfg, err := parseScoreConfig(raw)
	if err != nil {
		return Results{}, err
	}
	totals := make(map[string]float64, len(allIDs))
	raters := make(map[string]int, len(allIDs))
	for _, b := range ballots {
		for id, s := range b.Selections {
			if s > 0 {
				totals[id] += float64(s)
				raters[id]++
			}
		}
	}
	scores := make(map[string]float64, len(allIDs))
	for _, id := range allIDs {
		switch cfg.Aggregate {
		case "average":
			if raters[id] > 0 {
				scores[id] = totals[id] / float64(raters[id])
			}
		default: // total
			scores[id] = totals[id]
		}
	}
	ranked, winners := rankByScore(scores, allIDs)
	return Results{Method: "score", Ranked: ranked, WinnerIDs: winners}, nil
}
