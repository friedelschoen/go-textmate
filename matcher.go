package textmate

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/friedelschoen/go-textmate/regexp"
)

// Token describes a scoped span in the input.
// Tokens may overlap; render the token with the highest Depth at a position.
type Token struct {
	// Scope given by grammar
	Scope string
	// Index in text of start
	Start int
	// Length of the token
	Length int
	// Depth, if tokens overlap each other, the token with a higher depth should be used
	Depth int
}

func CompareToken(left *Token, right *Token) int {
	if left.Start != right.Start {
		return left.Start - right.Start
	}
	if left.Length != right.Length {
		return left.Length - right.Length
	}
	return left.Depth - right.Depth
}

func (tok Token) End() int {
	return tok.Start + tok.Length
}

// StackItem is one frame on the parse stack carrying the active rule context.
type StackItem struct {
	rules    []*MatchRule
	grammar  *Grammar
	offset   int
	previous *StackItem
}

// Root walks up to the nearest non-nil grammar on the stack.
// Panics if none is found (should not happen).
func (si *StackItem) Root() *Grammar {
	for si.grammar == nil {
		si = si.previous
	}
	if si == nil {
		panic("stack does not contain a grammar")
	}
	return si.grammar
}

// Depth returns the nesting depth of this frame (used for token priority).
func (si *StackItem) Depth() int {
	depth := 1
	for si != nil {
		si = si.previous
		depth++
	}
	return depth
}

// evaluateRule tries a single rule against text[start:end].
// Returns (newTop, advance, err). advance meanings:
//
//	>0 = number of consumed bytes, 0 = no match, -1 = context switch (include of other grammar).
func evaluateRule(offset int, text string, start int, end int, top *StackItem, yield func(*Token), rule *MatchRule) (*StackItem, int, error) {
	switch {
	case rule.Includes == "":
		/* continue */
	case rule.Includes[0] == '#':
		newrule, ok := top.Root().Repository[rule.Includes[1:]]
		if !ok {
			panic("unknown " + rule.Includes)
		}
		return evaluateRule(offset, text, start, end, top, yield, newrule)
	case rule.Includes == "$self":
		return evaluateRule(offset, text, start, end, top, yield, top.Root().Root)
	case strings.HasPrefix(rule.Includes, "source."):
		root := top.Root().Directory
		other, err := LoadGrammar(path.Join(root, rule.Includes[8:]+GrammarExtension))
		if err != nil {
			return nil, 0, fmt.Errorf("unable to include `%s`: %w", rule.Includes, err)
		}
		top = &StackItem{
			rules:    []*MatchRule{other.Root},
			grammar:  other,
			previous: top,
		}
		return top, -1, nil
	default:
		return nil, 0, fmt.Errorf("unable to include `%s`: invalid request", rule.Includes)
	}

	if rule.Operation == OperationExpand {
		var consumed int
		var err error
		for _, child := range rule.Rules {
			top, consumed, err = evaluateRule(offset, text, start, end, top, yield, child)
			if err != nil {
				return nil, 0, err
			}
			if consumed != 0 {
				return top, consumed, nil
			}
		}
		return top, 0, nil
	}

	groups, err := rule.Pattern.Match(text, start, len(text), regexp.OptionNotBeginPosition)
	if err != nil {
		return nil, 0, err
	}
	if groups == nil {
		return top, 0, nil
	}
	length := groups[0].Len()

	if rule.Name != "" {
		yield(&Token{
			Scope:  rule.Name,
			Start:  groups[0].Start + offset,
			Length: groups[0].Len(),
			Depth:  top.Depth(),
		})
	}

	for i, rng := range groups {
		if i >= len(rule.Captures) {
			break
		}
		if rule.Captures[i] == nil {
			continue
		}
		if rng.Len() == 0 {
			continue
		}

		cap := rule.Captures[i]
		if cap.Name != "" {
			yield(&Token{
				Scope:  cap.Name,
				Start:  rng.Start + offset,
				Length: rng.Len(),
				Depth:  top.Depth(),
			})
		}

		if cap.Rules != nil {
			var err error
			_, err = TokenizeLine(offset, text, rng.Start, rng.End, &StackItem{rules: cap.Rules, previous: top}, yield)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	switch rule.Operation {
	case OperationPush:
		top = &StackItem{
			offset:   start + offset,
			rules:    rule.Rules,
			previous: top,
		}
	case OperationPop:
		yield(&Token{
			Scope:  rule.Name,
			Start:  top.offset,
			Length: start + length + offset - top.offset,
			Depth:  top.Depth(),
		})
		top = top.previous
	}

	return top, length, nil
}

// TokenizeLine tokenizes text[start:end] within the given stack context.
// Always guarantees progress: if nothing matches, emits a 1-byte filler token (Scope:"").
func TokenizeLine(offset int, text string, start int, end int, top *StackItem, yield func(*Token)) (*StackItem, error) {
	lineoffset := start
	if end == 0 {
		end = len(text)
	}
	for lineoffset < end {
		consumed := false
		var err error
		var adv int
		for _, rule := range top.rules {
			top, adv, err = evaluateRule(offset, text, lineoffset, end, top, yield, rule)
			if err != nil {
				return nil, err
			}
			if adv > 0 {
				lineoffset += adv
			}
			/* either -1 or positive */
			if adv != 0 {
				consumed = true
				break
			}
		}
		if !consumed {
			yield(&Token{
				Scope:  "",
				Start:  lineoffset + offset,
				Length: 1,
			})

			lineoffset++
		}
	}
	return top, nil
}

// StackItem constructs a root frame for this grammar.
func (g *Grammar) StackItem() *StackItem {
	return &StackItem{
		rules:   []*MatchRule{g.Root},
		grammar: g,
	}
}

// TokenizeReader is a reference implementation that scans line-by-line.
// Offsets are global across lines; tokens are stabilized afterwards using CompareToken.
func (g *Grammar) TokenizeReader(reader io.Reader) ([]*Token, error) {
	top := g.StackItem()
	tokens := make([]*Token, 0)

	scanner := bufio.NewScanner(reader)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i+1], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	offset := 0
	var err error
	for scanner.Scan() {
		text := scanner.Text()
		top, err = TokenizeLine(offset, text, 0, len(text), top, func(t *Token) {
			tokens = append(tokens, t)
		})
		if err != nil {
			return nil, err
		}
		offset += len(text)
	}

	slices.SortFunc(tokens, CompareToken)

	return tokens, nil
}
