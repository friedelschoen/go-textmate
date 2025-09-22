package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/friedelschoen/go-textmate"
	"github.com/friedelschoen/go-textmate/theme"
)

var grammarDir = "share/colorcat/grammars"
var themeDir = "share/colorcat/themes"

func main() {
	// Flags
	var grammarName, themeName string
	var transparent bool
	flag.StringVar(&grammarName, "syntax", "", "Name")
	flag.StringVar(&themeName, "theme", "default", "Theme")
	flag.BoolVar(&transparent, "transparent", false, "Theme")
	flag.Parse()

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

	grammarPath := filepath.Join("/usr", grammarDir, grammarName+".tmLanguage.json")
	themePath := filepath.Join("/usr", themeDir, themeName+".json")

	userdir, userdirErr := os.UserHomeDir()

	// Defaults if not set
	if _, err := os.Stat(grammarPath); err != nil {
		if userdirErr != nil {
			fmt.Fprintf(os.Stderr, "unable to determine home directory: %v\n", err)
			os.Exit(1)
		}
		grammarPath = filepath.Join(userdir, ".local", grammarDir, grammarName+".tmLanguage.json")
	}
	if _, err := os.Stat(themePath); err != nil {
		if userdirErr != nil {
			fmt.Fprintf(os.Stderr, "unable to determine home directory: %v\n", err)
			os.Exit(1)
		}
		themePath = filepath.Join(userdir, ".local", themeDir, themeName+".json")
	}

	// Load grammar
	grammar, err := textmate.LoadGrammar(grammarPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load grammar `%s`: %v\n", grammarPath, err)
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
		stack, err = textmate.TokenizeLine(off, line, 0, len(line), stack, mapper.Add)
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
