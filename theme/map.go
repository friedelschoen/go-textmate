package theme

import (
	"iter"
	"strings"

	"github.com/friedelschoen/go-textmate"
)

type ColorMapping struct {
	TokenColor
	Offset int
}

func getSplitted(current map[string]TokenColor, name string) (TokenColor, bool) {
	for name != "" {
		s, ok := current[name]
		if ok {
			return s, true
		}
		i := strings.LastIndexByte(name, '.')
		if i == -1 {
			break
		}
		name = name[:i]
	}
	return TokenColor{}, false
}

func (t *Theme) getToken(toks []*textmate.Token) (TokenColor, bool) {
	current := t.Tokens
	var last TokenColor
	found := false

	for i, part := range toks {
		c, ok := getSplitted(current, part.Scope)
		if !ok && i == 0 {
			break
		}
		if !ok {
			continue
		}
		last = c
		found = true
		current = c.Children
	}

	return last, found
}

func (t *Theme) MapTokens(tokens iter.Seq2[int, []*textmate.Token]) []ColorMapping {
	var res []ColorMapping
	for off, toks := range tokens {
		s, _ := t.getToken(toks)
		res = append(res, ColorMapping{s, off})
	}
	return res
}
