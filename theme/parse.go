package theme

import (
	"image"
	"image/color"
	"strings"
)

type ThemeJSON struct {
	Default TokenColorJSON   `json:"default"`
	Tokens  []TokenColorJSON `json:"tokens"`
}

type TokenColorJSON struct {
	Scope    any `json:"scope"`
	Settings struct {
		Foreground string `json:"foreground"`
		Background string `json:"background"`
		FontStyle  string `json:"fontStyle"`
	} `json:"settings"`
}

type FontStyle int

const (
	Bold FontStyle = 1 << iota
	Italic
	Underline
	Strikethrough
)

func (s FontStyle) Has(has FontStyle) bool {
	return s&has == has
}

type TokenColor struct {
	// uniform images
	Foreground color.Color
	Background color.Color
	Children   map[string]TokenColor
	FontStyle  FontStyle
}

type Theme struct {
	TokenColor
	Tokens map[string]TokenColor

	slicedCache map[string]TokenColor
}

func setName(dest map[string]TokenColor, scope string, col TokenColor) {
	parts := strings.Split(scope, " ")
	current := dest

	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		c, _ := current[part]
		if i == len(parts)-1 {
			// final part, assign color
			c.Foreground = col.Foreground
			c.Background = col.Background
		}
		if c.Children == nil {
			c.Children = make(map[string]TokenColor)
		}
		current[part] = c
		current = c.Children
	}
}

func parseToken(jc TokenColorJSON) (col TokenColor) {
	if jc.Settings.Foreground != "" {
		if c, err := parseColor(jc.Settings.Foreground); err == nil {
			col.Foreground = image.NewUniform(c)
		}
	}
	if jc.Settings.Background != "" {
		if c, err := parseColor(jc.Settings.Background); err == nil {
			col.Background = image.NewUniform(c)
		}
	}
	for field := range strings.FieldsSeq(jc.Settings.FontStyle) {
		switch field {
		case "bold":
			col.FontStyle |= Bold
		case "italic":
			col.FontStyle |= Italic
		case "underline":
			col.FontStyle |= Underline
		case "strikethrough":
			col.FontStyle |= Strikethrough
		}
	}
	return
}

func ParseTheme(j ThemeJSON) *Theme {
	tokens := make(map[string]TokenColor)
	for _, jc := range j.Tokens {
		col := parseToken(jc)
		switch name := jc.Scope.(type) {
		case string:
			setName(tokens, name, col)
		case []any:
			for _, name := range name {
				if nstr, ok := name.(string); ok {
					setName(tokens, nstr, col)
				}
			}
		}
	}

	return &Theme{
		TokenColor:  parseToken(j.Default),
		Tokens:      tokens,
		slicedCache: make(map[string]TokenColor),
	}
}
