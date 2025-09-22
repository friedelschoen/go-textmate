# go-textmate

A Go implementation of a **TextMate grammar tokenizer**.
It can load `.tmLanguage.json` grammars, compile them into an internal rule tree, and tokenize source text into scoped tokens. This is useful for syntax highlighting or code analysis.

## Features

- Load and compile **TextMate grammars** (`.tmLanguage.json`)
- Support for:
  - `match`, `begin`/`end` blocks
  - `captures`, `beginCaptures`, `endCaptures`
  - `include` (`#repo`, `$self`, `source.*`)
- Tokenizer with proper **stack-based push/pop rules**
- Tokens carry:
  - `Scope` (TextMate scope name)
  - `Start` and `Length`
  - `Depth` (nesting depth, for overlapping tokens)
- Mapper utility to iterate over tokens efficiently
- Written in idiomatic Go, no C dependencies

## Installation

```bash
% go get github.com/friedelschoen/go-textmate
```

## Usage

### Load a grammar

```go
grammar, err := textmate.LoadGrammar("grammars/go.tmLanguage.json")
if err != nil {
    panic(err)
}
```

### Tokenize text

```go
f, _ := os.Open("example.go")
tokens, err := grammar.TokenizeReader(f)
if err != nil {
    panic(err)
}
for _, tok := range tokens {
    fmt.Printf("%s: %d..%d\n", tok.Scope, tok.Start, tok.End())
}
```

### Using Mapper

```go
mapper := make(textmate.Mapper, fileSize)
for _, tok := range tokens {
    mapper.Add(tok)
}
for pos, scopes := range mapper.Iter() {
    fmt.Println(pos, scopes)
}
```

## Project structure

* **`compile.go`** – parsing and compiling JSON grammar into `Grammar` and `MatchRule`
* **`matcher.go`** – the tokenizer and stack machine for applying rules
* **`map.go`** – utilities to map tokens to positions and iterate efficiently


## License

Zlib License.
