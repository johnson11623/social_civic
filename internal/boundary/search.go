package boundary

import (
	"sort"
	"strings"
)

// Search scores, highest first. Ties break by level (county before
// constituency before ward), then registered voters, then name.
const (
	scoreExact         = 100
	scorePrefix        = 90
	scoreCompactPrefix = 85 // "homabay" → HOMA BAY, "muranga" → MURANG'A
	scoreWordPrefix    = 80 // "makadara" → MJI WA KALE/MAKADARA
	scoreAllWords      = 75 // "ziwa ngombe" → ZIWA LA NG'OMBE
	scoreWithContext   = 65 // "township kiharu" → TOWNSHIP ward in Kiharu
	scoreSubstring     = 60
	scoreFuzzy         = 50 // minus 10 per edit
	aliasPenalty       = 5

	DefaultSearchLimit = 10
	MaxSearchLimit     = 50
	minQueryLen        = 2
)

// aliases are well-known alternative names, keyed by level and IEBC name.
var aliases = map[key2][]string{
	{LevelConstituency, "MT. ELGON"}:  {"mount elgon"},
	{LevelConstituency, "SUBA NORTH"}: {"mbita"}, // renamed before the 2022 election
}

type key2 struct {
	level Level
	name  string
}

// SearchOptions filters a search.
type SearchOptions struct {
	Level Level // 0 = all levels except national
	Limit int   // 0 = DefaultSearchLimit
}

// Match is one search result.
type Match struct {
	Unit      Unit
	Ancestors []Unit // nearest first, national excluded
	Score     int
}

type searchEntry struct {
	unit      Unit
	names     []string // normalized name first, then aliases
	spaced    []string // " " + name, for word-prefix checks without allocating
	compact   []string
	tokens    []string // tokens of the normalized name
	ctxTokens []string // tokens of ancestor names
}

type searchIndex struct {
	entries []searchEntry
}

func newSearchIndex(t *Tree) *searchIndex {
	idx := &searchIndex{}
	for _, u := range t.units {
		if u.Level == LevelNational {
			continue
		}
		e := searchEntry{unit: u}
		e.names = append(e.names, normalize(u.Name))
		for _, a := range aliases[key2{u.Level, u.Name}] {
			e.names = append(e.names, normalize(a))
		}
		for _, n := range e.names {
			e.spaced = append(e.spaced, " "+n)
			e.compact = append(e.compact, strings.ReplaceAll(n, " ", ""))
		}
		e.tokens = strings.Fields(e.names[0])
		for _, a := range t.Ancestors(u) {
			e.ctxTokens = append(e.ctxTokens, strings.Fields(normalize(a.Name))...)
		}
		idx.entries = append(idx.entries, e)
	}
	return idx
}

// normalize lowercases, drops apostrophes, and turns every other non-alphanumeric
// character into a single space: "ZIWA LA NG'OMBE" → "ziwa la ngombe",
// "MJI WA KALE/MAKADARA" → "mji wa kale makadara".
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’' || r == '`':
			continue
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	return b.String()
}

// Search finds units whose names match q, tolerating case, punctuation,
// spacing and small typos.
func (t *Tree) Search(q string, opts SearchOptions) []Match {
	nq := normalize(q)
	if len(nq) < minQueryLen {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	qTokens := strings.Fields(nq)
	qCompact := strings.ReplaceAll(nq, " ", "")
	qSpaced := " " + nq

	var matches []Match
	for i := range t.index.entries {
		e := &t.index.entries[i]
		if opts.Level != 0 && e.unit.Level != opts.Level {
			continue
		}
		if s := e.score(nq, qSpaced, qCompact, qTokens); s > 0 {
			matches = append(matches, Match{Unit: e.unit, Score: s})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Unit.Level != b.Unit.Level {
			return a.Unit.Level > b.Unit.Level
		}
		if a.Unit.RegisteredVoters != b.Unit.RegisteredVoters {
			return a.Unit.RegisteredVoters > b.Unit.RegisteredVoters
		}
		return a.Unit.DisplayName < b.Unit.DisplayName
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	for i := range matches {
		matches[i].Ancestors = t.Ancestors(matches[i].Unit)
	}
	return matches
}

func (e *searchEntry) score(q, qSpaced, qCompact string, qTokens []string) int {
	best := 0
	keep := func(s int) {
		if s > best {
			best = s
		}
	}
	for i, name := range e.names {
		penalty := 0
		if i > 0 {
			penalty = aliasPenalty
		}
		switch {
		case name == q:
			keep(scoreExact - penalty)
		case strings.HasPrefix(name, q):
			keep(scorePrefix - penalty)
		case len(qCompact) >= 3 && strings.HasPrefix(e.compact[i], qCompact):
			keep(scoreCompactPrefix - penalty)
		case strings.Contains(e.spaced[i], qSpaced):
			keep(scoreWordPrefix - penalty)
		case len(q) >= 3 && strings.Contains(name, q):
			keep(scoreSubstring - penalty)
		}
	}
	if best >= scoreAllWords {
		return best
	}

	if len(qTokens) > 1 {
		ownHit, allHit := 0, true
		for _, qt := range qTokens {
			switch {
			case anyPrefix(e.tokens, qt):
				ownHit++
			case anyPrefix(e.ctxTokens, qt):
			default:
				allHit = false
			}
		}
		if allHit && ownHit == len(qTokens) {
			keep(scoreAllWords)
		} else if allHit && ownHit > 0 {
			keep(scoreWithContext)
		}
	}

	if best == 0 && len(q) >= 4 && len(qTokens) == 1 {
		allowed := 1
		if len(q) >= 8 {
			allowed = 2
		}
		for _, tok := range e.tokens {
			d := editDistance(q, tok, allowed)
			if len(tok) > len(q) {
				d = min(d, editDistance(q, tok[:len(q)], allowed))
			}
			if d <= allowed {
				keep(scoreFuzzy - 10*d)
			}
		}
	}
	return best
}

func anyPrefix(tokens []string, p string) bool {
	for _, t := range tokens {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// editDistance is the Damerau–Levenshtein (optimal string alignment) distance
// between ASCII strings a and b, returning max+1 once it exceeds max.
func editDistance(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	// Three rows on the stack for typical names; heap only for very long input.
	var buf [3][64]int
	var prev2, prev, cur []int
	if len(b)+1 <= len(buf[0]) {
		prev2, prev, cur = buf[0][:len(b)+1], buf[1][:len(b)+1], buf[2][:len(b)+1]
	} else {
		prev2, prev, cur = make([]int, len(b)+1), make([]int, len(b)+1), make([]int, len(b)+1)
	}
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > max {
			return max + 1
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}
