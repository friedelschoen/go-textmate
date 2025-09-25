package textmate

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
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
	rules    []*matchRule
	offset   int
	previous *StackItem
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
func evaluateRule(offset int, text string, top *StackItem, yield func(*Token), rule *matchRule, basegrammar *Grammar) (*StackItem, int, error) {
	if rule.includes != "" {
		scopename, rulename, hasRule := strings.Cut(rule.includes, "#")

		var othergrammar *Grammar
		switch scopename {
		case "", "$self":
			othergrammar = rule.grammar
		case "$base":
			othergrammar = basegrammar
		default:
			var err error
			othergrammar, err = rule.grammar.loader.FromScope(scopename)
			if err != nil {
				return nil, 0, fmt.Errorf("unable to include `%s`: %w", rule.includes, err)
			}
		}
		rule = othergrammar.root
		if hasRule {
			var ok bool
			rule, ok = othergrammar.repository[rulename]
			if !ok {
				return nil, 0, fmt.Errorf("unable to include `%s`: unknown rule `%s`", rule.includes, rulename)
			}
		}
		/* continue with new rule */
	}

	if rule.operation == opExpand {
		var consumed int
		var err error
		for _, child := range rule.rules {
			top, consumed, err = evaluateRule(offset, text, top, yield, child, basegrammar)
			if err != nil || consumed != 0 {
				return top, consumed, err
			}
		}
		return top, 0, nil
	}

	groups, err := rule.pattern.Match(text, 0, len(text), regexp.OptionNotBeginPosition)
	if err != nil || groups == nil {
		return top, 0, err
	}
	length := groups[0].Len()

	if rule.name != "" {
		yield(&Token{
			Scope:  rule.name,
			Start:  groups[0].Start + offset,
			Length: groups[0].Len(),
			Depth:  top.Depth(),
		})
	}

	for i, rng := range groups {
		if i >= len(rule.captures) {
			break
		}
		if rule.captures[i] == nil {
			continue
		}
		if rng.Len() == 0 {
			continue
		}

		cap := rule.captures[i]
		if cap.name != "" {
			yield(&Token{
				Scope:  cap.name,
				Start:  offset + rng.Start,
				Length: rng.Len(),
				Depth:  top.Depth(),
			})
		}

		if cap.rules != nil {
			var err error
			_, err = TokenizeSequence(offset+rng.Start, text[rng.Start:rng.End], &StackItem{rules: cap.rules, previous: top}, yield, basegrammar)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	switch rule.operation {
	case opPush:
		top = &StackItem{
			offset:   offset,
			rules:    rule.rules,
			previous: top,
		}
	case opPop:
		yield(&Token{
			Scope:  rule.name,
			Start:  top.offset,
			Length: length + offset - top.offset,
			Depth:  top.Depth(),
		})
		top = top.previous
	}

	return top, length, nil
}

// TokenizeSequence tokenizes text[start:end] within the given stack context.
// Always guarantees progress: if nothing matches, emits a 1-byte filler token (Scope:"").
func TokenizeSequence(offset int, text string, top *StackItem, yield func(*Token), basegrammar *Grammar) (*StackItem, error) {
	lineoffset := 0
	for lineoffset < len(text) {
		consumed := false
		var err error
		var adv int
		for _, rule := range top.rules {
			top, adv, err = evaluateRule(offset+lineoffset, text[lineoffset:], top, yield, rule, basegrammar)
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
		rules: []*matchRule{g.root},
	}
}

// TokenizeReader is a reference implementation that scans line-by-line.
// Offsets are global across lines; tokens are stabilized afterwards using CompareToken.
func (g *Grammar) TokenizeReader(reader io.Reader) ([]*Token, error) {
	top := g.StackItem()
	var tokens []*Token

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
		top, err = TokenizeSequence(offset, text, top, func(t *Token) {
			tokens = append(tokens, t)
		}, g)
		if err != nil {
			return nil, err
		}
		offset += len(text)
	}

	slices.SortFunc(tokens, CompareToken)

	return tokens, nil
}
