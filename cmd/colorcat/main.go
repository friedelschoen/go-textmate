package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/friedelschoen/go-textmate"
	"github.com/friedelschoen/go-textmate/theme"
)

var grammarDir = "share/colorcat/grammars"
var themeDir = "share/colorcat/themes"

func main() {
	// Flags
	var grammarName, themeName string
	var transparent, doList bool
	flag.StringVar(&grammarName, "syntax", "", "Name")
	flag.StringVar(&themeName, "theme", "default", "Theme")
	flag.BoolVar(&transparent, "transparent", false, "Theme")
	flag.BoolVar(&doList, "list", false, "List all themes and available syntaxes")
	flag.Parse()

	userdir, userdirErr := os.UserHomeDir()

	loader, _ := textmate.NewLoader(func(yield func(string) bool) {
		dir := filepath.Join("/usr", grammarDir)
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if !entry.IsDir() {
				if !yield(path.Join(dir, entry.Name())) {
					return
				}
			}
		}
		if userdirErr == nil {
			dir = filepath.Join(userdir, ".local", grammarDir)
			entries, _ = os.ReadDir(dir)
			for _, entry := range entries {
				if !entry.IsDir() {
					if !yield(path.Join(dir, entry.Name())) {
						return
					}
				}
			}
		}
	})

	if doList {
		fmt.Println("File Types:")
		fts := slices.Collect(loader.FileTypes())
		names := maps.Collect(loader.FileTypeNames())
		slices.Sort(fts)
		for _, ft := range fts {
			fmt.Printf("- %s: %s\n", ft, strings.Join(names[ft], ", "))
		}

		os.Exit(1)
	}

	// Defaults if not set
	themePath := filepath.Join("/usr", themeDir, themeName+".json")
	if _, err := os.Stat(themePath); err != nil {
		if userdirErr != nil {
			fmt.Fprintf(os.Stderr, "unable to determine home directory: %v\n", err)
			os.Exit(1)
		}
		themePath = filepath.Join(userdir, ".local", themeDir, themeName+".json")
	}

	sourceFile := os.Stdin
	defer sourceFile.Close()
	// Require a source file
	if flag.NArg() > 0 {
		name := flag.Arg(0)
		var err error
		sourceFile, err = os.Open(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load file `%s`: %v\n", name, err)
			os.Exit(1)
		}
		if grammarName == "" {
			grammarName = strings.TrimPrefix(path.Ext(name), ".")
		}
	}

	// Load grammar
	grammar, err := loader.FromFileType(grammarName, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load grammar `%s`: %v\n", grammarName, err)
		os.Exit(1)
	}

	// Load theme
	themeBytes, err := os.ReadFile(themePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read theme: %v\n", err)
		os.Exit(1)
	}
	var themeJSON theme.ThemeJSON
	if err := json.Unmarshal(themeBytes, &themeJSON); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse theme JSON: %v\n", err)
		os.Exit(1)
	}
	t := theme.ParseTheme(themeJSON)

	// Read source file
	sourceBytes, err := io.ReadAll(sourceFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read source file: %v\n", err)
		os.Exit(1)
	}
	source := string(sourceBytes)

	// Tokenize
	mapper := make(textmate.Mapper, len(sourceBytes))
	var off int
	stack := grammar.StackItem()
	for _, line := range strings.SplitAfter(source, "\n") {
		stack, err = textmate.TokenizeSequence(off, line, stack, mapper.Add, grammar)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tokenization error: %v\n", err)
			os.Exit(1)
		}
		off += len(line)
	}

	// Map tokens to theme
	tokens := t.MapTokens(mapper.Iter())

	// Render with ANSI escapes
	cur := -1
	for i, chr := range source {
		if cur < len(tokens)-1 && tokens[cur+1].Offset == i {
			cur++
			tok := tokens[cur]
			if !transparent {
				if tok.Foreground == nil {
					tok.Foreground = t.Foreground
				}
				if tok.Background == nil {
					tok.Background = t.Background
				}
			}

			var csi bytes.Buffer

			// Reset attributes
			csi.WriteString("\033[0")

			// Font style
			if tok.FontStyle.Has(theme.Bold) {
				csi.WriteString(";1")
			}
			if tok.FontStyle.Has(theme.Italic) {
				csi.WriteString(";3")
			}
			if tok.FontStyle.Has(theme.Underline) {
				csi.WriteString(";4")
			}
			if tok.FontStyle.Has(theme.Strikethrough) {
				csi.WriteString(";9")
			}

			// Colors
			if tok.Foreground != nil {
				r, g, b, _ := tok.Foreground.RGBA()
				fmt.Fprintf(&csi, ";38;2;%d;%d;%d", r>>8, g>>8, b>>8)
			}
			if tok.Background != nil {
				r, g, b, _ := tok.Background.RGBA()
				fmt.Fprintf(&csi, ";48;2;%d;%d;%d", r>>8, g>>8, b>>8)
			}
			csi.WriteByte('m')
			csi.WriteTo(os.Stdout)
		}
		fmt.Printf("%c", chr)
	}

	// Reset formatting at the end
	fmt.Printf("\033[0m\n")
}
