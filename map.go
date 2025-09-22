package textmate

import (
	"iter"
	"slices"
)

// Mapper is an index→tokens structure.
// For each byte position, it stores the tokens covering that position.
// Useful for renderers that draw only when the set of active tokens changes.
type Mapper [][]*Token

// Add inserts the token for all positions it covers. Empty scopes are ignored.
// Note: O(tok.Length); can be expensive for very long tokens.
func (tm Mapper) Add(tok *Token) {
	if tok.Scope == "" {
		return
	}
	for idx := range tok.Length {
		i := idx + tok.Start
		if i >= len(tm) {
			/* out of bounds */
			break
		}
		tm[i] = append(tm[i], tok)
	}
}

// Iter returns an iterator yielding (pos, tokens) whenever the set of tokens changes.
// Tokens at each position are stabilized via CompareToken for deterministic order.
func (tm Mapper) Iter() iter.Seq2[int, []*Token] {
	return func(yield func(int, []*Token) bool) {
		var prev []*Token
		for i, cur := range tm {
			slices.SortFunc(cur, CompareToken)
			if !slices.Equal(prev, cur) {
				if !yield(i, cur) {
					return
				}
				prev = cur
			}
		}
	}
}
