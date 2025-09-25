// Package textmate tokenizes source files using TextMate grammars, intended for syntax highlighting.
// Workflow:
// 1) Parse JSON grammar into an internal rule tree (MatchRule)
// 2) Tokenizer walks the rules and emits scoped tokens
package textmate

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/friedelschoen/go-textmate/regexp"
)

var (
	ErrScopeName = errors.New("unexpected `scopeName`")
)

// GrammarExtension is the expected extension for grammar files (used for "source.*" includes).
var GrammarExtension = ".tmLanguage.json"

// operation controls parse stack behavior when a rule matches.
// Expand tries subrules only; Push/Pop open/close a block by mutating the stack.
type operation int

const (
	opNOP operation = iota
	opPush
	opPop
	opExpand
)

// GrammarJSON mirrors the (subset of) TextMate JSON/Plist grammar on disk.
// It is decoded as-is and later compiled into Grammar.
type GrammarJSON struct {
	Name         string              `json:"name" plist:"name"`
	ScopeName    string              `json:"scopeName" plist:"scopeName"`
	FileTypes    []string            `json:"fileTypes" plist:"fileTypes"`
	FoldingStart string              `json:"foldingStartMarker" plist:"foldingStartMarker"`
	FoldingEnd   string              `json:"foldingStopMarker" plist:"foldingStopMarker"`
	FirstLine    string              `json:"firstLineMatch" plist:"firstLineMatch"`
	Repository   map[string]RuleJSON `json:"repository" plist:"repository"`
	Patterns     []RuleJSON          `json:"patterns" plist:"patterns"`
}

// RuleJSON is a raw grammar rule (as found in the JSON file).
// Note: capture groups are addressed by string indices "1","2",...
type RuleJSON struct {
	Name          string              `json:"name" plist:"name"`
	Match         string              `json:"match" plist:"match"`
	Begin         string              `json:"begin" plist:"begin"`
	End           string              `json:"end" plist:"end"`
	Patterns      []RuleJSON          `json:"patterns" plist:"patterns"`
	Captures      map[string]RuleJSON `json:"captures" plist:"captures"`
	BeginCaptures map[string]RuleJSON `json:"beginCaptures" plist:"beginCaptures"`
	EndCaptures   map[string]RuleJSON `json:"endCaptures" plist:"endCaptures"`
	Include       string              `json:"include" plist:"include"`
}

// Grammar is the compiled grammar with precompiled regexes and an executable rule tree.
type Grammar struct {
	loader       *Loader
	scopeName    string
	fileTypes    []string
	foldingStart *regexp.Regexp
	foldingEnd   *regexp.Regexp
	firstLine    *regexp.Regexp
	repository   map[string]*matchRule
	root         *matchRule
}

// matchRule is an executable rule.
// If Pattern != nil it's a concrete regex match; otherwise it's a container/redirect (Includes or Rules).
// Operation drives stack behavior; Includes supports $self, #repo, and source.*.
type matchRule struct {
	name      string
	pattern   *regexp.Regexp
	captures  []*matchRule
	rules     []*matchRule
	operation operation
	includes  string
	grammar   *Grammar
}

// CompileGrammar compiles a decoded GrammarJSON into an executable Grammar.
// dirname decides where 'source.*' includes are resolved and defaults to `.`; filename is used
// to strictly validate j.ScopeName ("source.<basename>") and may be omitted.
func CompileGrammar(l *Loader, j *GrammarJSON) (*Grammar, error) {
	res := &Grammar{
		loader:    l,
		scopeName: j.ScopeName,
		fileTypes: j.FileTypes,
	}
	if j.FoldingStart != "" {
		expr, err := regexp.Compile(j.FoldingStart, 0)
		if err != nil {
			return nil, err
		}
		res.foldingStart = expr
	}
	if j.FoldingEnd != "" {
		expr, err := regexp.Compile(j.FoldingEnd, 0)
		if err != nil {
			return nil, err
		}
		res.foldingEnd = expr
	}
	if j.FirstLine != "" {
		expr, err := regexp.Compile(j.FirstLine, 0)
		if err != nil {
			return nil, err
		}
		res.firstLine = expr
	}
	rules := make([]*matchRule, len(j.Patterns))
	var err error
	for i, jp := range j.Patterns {
		rules[i], err = compileRule(res, jp)
		if err != nil {
			return nil, err
		}
	}
	res.root = &matchRule{name: j.ScopeName, rules: rules, operation: opExpand, grammar: res}
	res.repository = make(map[string]*matchRule, len(j.Repository))
	for name, jp := range j.Repository {
		res.repository[name], err = compileRule(res, jp)
		if err != nil {
			return nil, err
		}
	}

	return res, nil
}

// compileCaptures converts string-indexed captures ("1","2",...) to a slice
// sized 0..maxIndex, leaving missing indices as nil.
// Each capture may carry a scope name and/or subrules.
func compileCaptures(grammar *Grammar, j map[string]RuleJSON) ([]*matchRule, error) {
	if j == nil {
		return nil, nil
	}

	maxcaptures := 0
	for num := range j {
		i, err := strconv.Atoi(num)
		if err != nil {
			return nil, err
		}

		if i > maxcaptures {
			maxcaptures = i
		}
	}

	res := make([]*matchRule, maxcaptures+1)
	for num, jp := range j {
		/* already checked if index is number */
		i, _ := strconv.Atoi(num)

		capture := &matchRule{
			name:    jp.Name,
			grammar: grammar,
		}
		var err error
		capture.rules = make([]*matchRule, len(jp.Patterns))
		for i, jp := range jp.Patterns {
			capture.rules[i], err = compileRule(grammar, jp)
			if err != nil {
				return nil, err
			}
		}
		res[i] = capture
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// compileRule compiles a single RuleJSON into a MatchRule.
// Case order follows TM conventions: Include, Match, Begin/End, Container.
func compileRule(grammar *Grammar, j RuleJSON) (*matchRule, error) {
	switch {
	case j.Include != "":
		return &matchRule{
			includes: j.Include,
			grammar:  grammar,
		}, nil
	case j.Match != "":
		match, err := regexp.Compile(j.Match, 0)
		if err != nil {
			return nil, err
		}
		captures, err := compileCaptures(grammar, j.Captures)
		if err != nil {
			return nil, err
		}
		return &matchRule{
			name:     j.Name,
			pattern:  match,
			captures: captures,
			grammar:  grammar,
		}, nil
	case j.Begin != "" && j.End != "":
		begin, err := regexp.Compile(j.Begin, 0)
		if err != nil {
			return nil, err
		}
		end, err := regexp.Compile(j.End, 0)
		if err != nil {
			return nil, err
		}
		var beginCaptures, endCaptures []*matchRule
		if len(j.Captures) > 0 {
			captures, err := compileCaptures(grammar, j.BeginCaptures)
			if err != nil {
				return nil, err
			}
			beginCaptures = captures
			endCaptures = captures
		} else {
			beginCaptures, err = compileCaptures(grammar, j.BeginCaptures)
			if err != nil {
				return nil, err
			}
			endCaptures, err = compileCaptures(grammar, j.EndCaptures)
			if err != nil {
				return nil, err
			}
		}

		rules := make([]*matchRule, len(j.Patterns)+1)
		rules[0] = &matchRule{
			name:      j.Name,
			pattern:   end,
			captures:  endCaptures,
			operation: opPop,
			grammar:   grammar,
		}
		for i, jp := range j.Patterns {
			rules[i+1], err = compileRule(grammar, jp)
			if err != nil {
				return nil, err
			}
		}
		return &matchRule{
			pattern:   begin,
			captures:  beginCaptures,
			rules:     rules,
			operation: opPush,
			grammar:   grammar,
		}, nil
	case j.Begin != "" || j.End != "":
		return nil, fmt.Errorf("found rule with begin or end omitted")
	default:
		rules := make([]*matchRule, len(j.Patterns))
		var err error
		for i, jp := range j.Patterns {
			rules[i], err = compileRule(grammar, jp)
			if err != nil {
				return nil, err
			}
		}
		return &matchRule{
			name:      j.Name,
			rules:     rules,
			operation: opExpand,
			grammar:   grammar,
		}, nil
	}
}
