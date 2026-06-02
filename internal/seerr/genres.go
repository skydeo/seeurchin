package seerr

import "strings"

// TMDB exposes fixed, well-known genre lists (one for movies, one for TV) keyed
// by numeric id. seeurchin stores a poll's genre restriction as Jellyfin genre
// *names*, so to gate Seerr/TMDB search results we translate those names into
// the TMDB ids they could mean and keep results whose genres intersect.
//
// The mapping is necessarily approximate: TMDB's TV genres are merged buckets
// ("Sci-Fi & Fantasy", "Action & Adventure", "War & Politics") that don't line
// up 1:1 with the movie-style names Jellyfin usually reports, so a name may map
// to several ids (its movie id plus the TV bucket it falls under). Keys are
// lowercased; unknown names map to nothing and simply don't constrain results.
var tmdbGenreIDs = map[string][]int{
	"action":             {28, 10759},    // movie Action + TV Action & Adventure
	"adventure":          {12, 10759},    // movie Adventure + TV Action & Adventure
	"action & adventure": {10759},        // TV
	"animation":          {16},           //
	"comedy":             {35},           //
	"crime":              {80},           //
	"documentary":        {99},           //
	"drama":              {18},           //
	"family":             {10751},        //
	"fantasy":            {14, 10765},    // movie Fantasy + TV Sci-Fi & Fantasy
	"history":            {36},           //
	"horror":             {27},           //
	"music":              {10402},        //
	"musical":            {10402},        // alias
	"mystery":            {9648},         //
	"romance":            {10749},        //
	"science fiction":    {878, 10765},   // movie Sci-Fi + TV Sci-Fi & Fantasy
	"sci-fi":             {878, 10765},   // alias
	"sci-fi & fantasy":   {10765},        // TV
	"tv movie":           {10770},        //
	"thriller":           {53},           //
	"war":                {10752, 10768}, // movie War + TV War & Politics
	"war & politics":     {10768},        // TV
	"western":            {37},           //
	"kids":               {10762},        // TV
	"news":               {10763},        // TV
	"reality":            {10764},        // TV
	"soap":               {10766},        // TV
	"talk":               {10767},        // TV
}

// GenreIDSet returns the set of TMDB genre ids the given (Jellyfin) genre names
// could map to. Names with no known mapping are skipped, so an empty result
// means none were recognized — callers should treat that as "do not filter"
// rather than "exclude everything".
func GenreIDSet(names []string) map[int]bool {
	set := map[int]bool{}
	for _, n := range names {
		for _, id := range tmdbGenreIDs[strings.ToLower(strings.TrimSpace(n))] {
			set[id] = true
		}
	}
	return set
}

// MatchesGenres reports whether a result tagged with the given TMDB genre ids
// falls within the allowed set. An empty allowed set means no restriction.
func MatchesGenres(genreIDs []int, allowed map[int]bool) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, id := range genreIDs {
		if allowed[id] {
			return true
		}
	}
	return false
}
