package voting

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Ranked implements ranked-choice voting via instant-runoff (IRV). Voters rank
// candidates (1 = top choice); the lowest first-preference candidate is
// eliminated each round and its ballots redistributed until one candidate holds
// a majority of the still-active ballots.
type Ranked struct{}

type rankedConfig struct {
	AllowSelfVote bool `json:"allow_self_vote"`
	MaxRanked     int  `json:"max_ranked"`               // 0 = no limit on how many may be ranked
	MaxSelfVotes  *int `json:"max_self_votes,omitempty"` // nil = use AllowSelfVote; <0 unlimited; 0 none; N cap
}

func (Ranked) Key() string   { return "ranked" }
func (Ranked) Label() string { return "Ranked-choice (IRV)" }

func (Ranked) DefaultConfig() json.RawMessage {
	return mustJSON(rankedConfig{AllowSelfVote: true, MaxRanked: 0})
}

func parseRankedConfig(raw json.RawMessage) (rankedConfig, error) {
	cfg := rankedConfig{AllowSelfVote: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("ranked config: %w", err)
		}
	}
	return cfg, nil
}

func (Ranked) ValidateConfig(raw json.RawMessage) error {
	cfg, err := parseRankedConfig(raw)
	if err != nil {
		return err
	}
	if cfg.MaxRanked < 0 {
		return fmt.Errorf("max_ranked must be >= 0")
	}
	return nil
}

func (Ranked) ValidateBallot(raw json.RawMessage, b Ballot, allIDs, ownIDs []string) error {
	cfg, err := parseRankedConfig(raw)
	if err != nil {
		return err
	}
	all := toSet(allIDs)
	own := toSet(ownIDs)
	selfLimit := selfVoteLimit(cfg.AllowSelfVote, cfg.MaxSelfVotes)
	seenRank := map[int]bool{}
	selfRanked := 0
	for id, rank := range b.Selections {
		if !all[id] {
			return fmt.Errorf("unknown nomination %q", id)
		}
		if rank < 1 {
			return fmt.Errorf("rank for %q must be >= 1", id)
		}
		if seenRank[rank] {
			return fmt.Errorf("duplicate rank %d", rank)
		}
		seenRank[rank] = true
		if own[id] {
			selfRanked++
		}
	}
	if selfLimit == 0 && selfRanked > 0 {
		return fmt.Errorf("ranking your own nomination is not allowed")
	}
	if selfLimit > 0 && selfRanked > selfLimit {
		return fmt.Errorf("rank at most %d of your own nomination(s)", selfLimit)
	}
	if cfg.MaxRanked > 0 && len(b.Selections) > cfg.MaxRanked {
		return fmt.Errorf("rank at most %d nomination(s)", cfg.MaxRanked)
	}
	return nil
}

func (Ranked) Tally(raw json.RawMessage, allIDs []string, ballots []Ballot) (Results, error) {
	// Build each ballot's preference order (most-preferred first).
	prefs := make([][]string, 0, len(ballots))
	for _, b := range ballots {
		if order := rankedOrder(b.Selections); len(order) > 0 {
			prefs = append(prefs, order)
		}
	}

	active := make(map[string]bool, len(allIDs))
	for _, id := range allIDs {
		active[id] = true
	}

	var rounds []RoundResult
	var elimOrder []string
	finalCount := make(map[string]float64, len(allIDs))
	var winners []string

	roundNum := 0
	for {
		roundNum++
		counts := make(map[string]float64, len(active))
		for id := range active {
			counts[id] = 0
		}
		totalActive := 0
		for _, order := range prefs {
			for _, id := range order {
				if active[id] {
					counts[id]++
					totalActive++
					break
				}
			}
		}
		snapshot := make(map[string]float64, len(counts))
		for id, c := range counts {
			snapshot[id] = c
			finalCount[id] = c
		}
		round := RoundResult{Round: roundNum, Counts: snapshot}

		activeIDs := keysSorted(active)
		if len(activeIDs) == 0 || totalActive == 0 {
			rounds = append(rounds, round)
			break
		}

		maxC, minC := -1.0, math.MaxFloat64
		for _, id := range activeIDs {
			if counts[id] > maxC {
				maxC = counts[id]
			}
			if counts[id] < minC {
				minC = counts[id]
			}
		}

		// Majority of active ballots, or only one candidate remains.
		if maxC*2 > float64(totalActive) || len(activeIDs) == 1 {
			for _, id := range activeIDs {
				if counts[id] == maxC {
					winners = append(winners, id)
				}
			}
			rounds = append(rounds, round)
			break
		}
		// Everyone tied: genuine multi-way tie, no further progress possible.
		if maxC == minC {
			winners = append(winners, activeIDs...)
			rounds = append(rounds, round)
			break
		}

		// Eliminate the single lowest (deterministic tie-break by ID order).
		var elim string
		for _, id := range activeIDs {
			if counts[id] == minC {
				elim = id
				break
			}
		}
		delete(active, elim)
		elimOrder = append(elimOrder, elim)
		round.Eliminated = []string{elim}
		rounds = append(rounds, round)
	}

	ordered := orderRanked(allIDs, winners, active, elimOrder, finalCount)
	results := make([]Result, 0, len(ordered))
	for _, id := range ordered {
		results = append(results, Result{NominationID: id, Score: finalCount[id]})
	}
	return Results{Method: "ranked", Ranked: results, WinnerIDs: winners, Rounds: rounds}, nil
}

// orderRanked produces the final best-first ordering: winners, then any other
// surviving candidates, then eliminated candidates in reverse elimination
// order (later eliminations rank higher), each grouped by final count.
func orderRanked(allIDs, winners []string, active map[string]bool, elimOrder []string, finalCount map[string]float64) []string {
	placed := make(map[string]bool, len(allIDs))
	var ordered []string

	byCountThenID := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			if finalCount[ids[i]] != finalCount[ids[j]] {
				return finalCount[ids[i]] > finalCount[ids[j]]
			}
			return ids[i] < ids[j]
		})
	}

	w := append([]string(nil), winners...)
	byCountThenID(w)
	for _, id := range w {
		if !placed[id] {
			ordered = append(ordered, id)
			placed[id] = true
		}
	}

	var rem []string
	for _, id := range keysSorted(active) {
		if !placed[id] {
			rem = append(rem, id)
		}
	}
	byCountThenID(rem)
	for _, id := range rem {
		ordered = append(ordered, id)
		placed[id] = true
	}

	for i := len(elimOrder) - 1; i >= 0; i-- {
		if id := elimOrder[i]; !placed[id] {
			ordered = append(ordered, id)
			placed[id] = true
		}
	}
	for _, id := range allIDs {
		if !placed[id] {
			ordered = append(ordered, id)
			placed[id] = true
		}
	}
	return ordered
}

// rankedOrder returns nomination IDs ordered most-preferred first (lowest rank
// number first), breaking rank ties by ID for determinism.
func rankedOrder(sel map[string]int) []string {
	type kv struct {
		id   string
		rank int
	}
	arr := make([]kv, 0, len(sel))
	for id, r := range sel {
		if r > 0 {
			arr = append(arr, kv{id, r})
		}
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].rank != arr[j].rank {
			return arr[i].rank < arr[j].rank
		}
		return arr[i].id < arr[j].id
	})
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, e.id)
	}
	return out
}

func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
