// Package textmate tokenizes source files using TextMate grammars, intended for syntax highlighting.
// Workflow:
// 1) Parse JSON grammar into an internal rule tree (MatchRule)
// 2) Tokenizer walks the rules and emits scoped tokens
package textmate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/friedelschoen/go-textmate/regexp"
)

var (
	ErrScopeName = errors.New("unexpected `scopeName`")
)

// GrammarExtension is the expected extension for grammar files (used for "source.*" includes).
var GrammarExtension = ".tmLanguage.json"

// Operation controls parse stack behavior when a rule matches.
// Expand tries subrules only; Push/Pop open/close a block by mutating the stack.
type Operation int

const (
	OperationNOP Operation = iota
	OperationPush
	OperationPop
	OperationExpand
)

// GrammarJSON mirrors the (subset of) TextMate JSON/Plist grammar on disk.
// It is decoded as-is and later compiled into Grammar.
type GrammarJSON struct {
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
	Directory    string
	ScopeName    string
	FileTypes    []string
	FoldingStart *regexp.Regexp
	FoldingEnd   *regexp.Regexp
	FirstLine    *regexp.Regexp
	Repository   map[string]*MatchRule
	Root         *MatchRule
}

// MatchRule is an executable rule.
// If Pattern != nil it's a concrete regex match; otherwise it's a container/redirect (Includes or Rules).
// Operation drives stack behavior; Includes supports $self, #repo, and source.*.
type MatchRule struct {
	Name      string
	Pattern   *regexp.Regexp
	Captures  []*MatchRule
	Rules     []*MatchRule
	Operation Operation
	Includes  string
}

// LoadGrammar reads a *.tmLanguage.json, validates scopeName vs filename,
// and compiles it into a usable Grammar.
func LoadGrammar(pathname string) (*Grammar, error) {
	content, err := os.ReadFile(pathname)
	if err != nil {
		return nil, err
	}
	var encoded GrammarJSON
	err = json.Unmarshal(content, &encoded)
	if err != nil {
		return nil, err
	}
	return CompileGrammar(encoded, path.Dir(pathname), path.Base(pathname))
}

// CompileGrammar compiles a decoded GrammarJSON into an executable Grammar.
// dirname decides where 'source.*' includes are resolved; filename is used
// to strictly validate j.ScopeName ("source.<basename>").
func CompileGrammar(j GrammarJSON, dirname string, filename string) (*Grammar, error) {
	if filename != "" {
		filesource := path.Base(filename)
		filesource, _ = strings.CutSuffix(filesource, GrammarExtension)
		jsonsource, _ := strings.CutPrefix(j.ScopeName, "source.")
		if jsonsource != filesource {
			return nil, fmt.Errorf("%w: expected 'source.%s', got '%s'", ErrScopeName, filesource, j.ScopeName)
		}
	}

	if dirname == "" {
		dirname = "."
	}
	res := &Grammar{
		Directory: dirname,
		ScopeName: j.ScopeName,
		FileTypes: j.FileTypes,
	}
	if j.FoldingStart != "" {
		expr, err := regexp.Compile(j.FoldingStart, 0)
		if err != nil {
			return nil, err
		}
		res.FoldingStart = expr
	}
	if j.FoldingEnd != "" {
		expr, err := regexp.Compile(j.FoldingEnd, 0)
		if err != nil {
			return nil, err
		}
		res.FoldingEnd = expr
	}
	if j.FirstLine != "" {
		expr, err := regexp.Compile(j.FirstLine, 0)
		if err != nil {
			return nil, err
		}
		res.FirstLine = expr
	}
	rules := make([]*MatchRule, len(j.Patterns))
	var err error
	for i, jp := range j.Patterns {
		rules[i], err = compileRule(jp)
		if err != nil {
			return nil, err
		}
	}
	res.Root = &MatchRule{Name: j.ScopeName, Rules: rules, Operation: OperationExpand}
	res.Repository = make(map[string]*MatchRule, len(j.Repository))
	for name, jp := range j.Repository {
		res.Repository[name], err = compileRule(jp)
		if err != nil {
			return nil, err
		}
	}

	return res, nil
}

// compileCaptures converts string-indexed captures ("1","2",...) to a slice
// sized 0..maxIndex, leaving missing indices as nil.
// Each capture may carry a scope name and/or subrules.
func compileCaptures(j map[string]RuleJSON) ([]*MatchRule, error) {
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

	res := make([]*MatchRule, maxcaptures+1)
	for num, jp := range j {
		/* already checked if index is number */
		i, _ := strconv.Atoi(num)

		capture := &MatchRule{
			Name: jp.Name,
		}
		var err error
		capture.Rules = make([]*MatchRule, len(jp.Patterns))
		for i, jp := range jp.Patterns {
			capture.Rules[i], err = compileRule(jp)
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
func compileRule(j RuleJSON) (*MatchRule, error) {
	switch {
	case j.Include != "":
		return &MatchRule{
			Includes: j.Include,
		}, nil
	case j.Match != "":
		match, err := regexp.Compile(j.Match, 0)
		if err != nil {
			return nil, err
		}
		captures, err := compileCaptures(j.Captures)
		if err != nil {
			return nil, err
		}
		return &MatchRule{
			Name:     j.Name,
			Pattern:  match,
			Captures: captures,
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
		var beginCaptures, endCaptures []*MatchRule
		if len(j.Captures) > 0 {
			captures, err := compileCaptures(j.BeginCaptures)
			if err != nil {
				return nil, err
			}
			beginCaptures = captures
			endCaptures = captures
		} else {
			beginCaptures, err = compileCaptures(j.BeginCaptures)
			if err != nil {
				return nil, err
			}
			endCaptures, err = compileCaptures(j.EndCaptures)
			if err != nil {
				return nil, err
			}
		}

		rules := make([]*MatchRule, len(j.Patterns)+1)
		rules[0] = &MatchRule{
			Name:      j.Name,
			Pattern:   end,
			Captures:  endCaptures,
			Operation: OperationPop,
		}
		for i, jp := range j.Patterns {
			rules[i+1], err = compileRule(jp)
			if err != nil {
				return nil, err
			}
		}
		return &MatchRule{
			Pattern:   begin,
			Captures:  beginCaptures,
			Rules:     rules,
			Operation: OperationPush,
		}, nil
	case j.Begin != "" || j.End != "":
		return nil, fmt.Errorf("found rule with begin or end omitted")
	default:
		rules := make([]*MatchRule, len(j.Patterns))
		var err error
		for i, jp := range j.Patterns {
			rules[i], err = compileRule(jp)
			if err != nil {
				return nil, err
			}
		}
		return &MatchRule{
			Name:      j.Name,
			Rules:     rules,
			Operation: OperationExpand,
		}, nil
	}
}
