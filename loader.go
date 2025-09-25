package textmate

import (
	"encoding/json"
	"io/fs"
	"iter"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

type Loader struct {
	filetypes map[string][]*GrammarJSON
	scopes    map[string]*GrammarJSON
}

func loadFile(pathname string) (*GrammarJSON, error) {
	content, err := os.ReadFile(pathname)
	if err != nil {
		return nil, err
	}
	var encoded GrammarJSON
	if strings.HasSuffix(pathname, ".json") {
		err = json.Unmarshal(content, &encoded)
	} else {
		_, err = plist.Unmarshal(content, &encoded)
	}
	return &encoded, err
}

func NewLoader(paths iter.Seq[string]) (*Loader, bool) {
	loader := Loader{
		scopes:    make(map[string]*GrammarJSON),
		filetypes: make(map[string][]*GrammarJSON),
	}

	for pathname := range paths {
		grm, err := loadFile(pathname)
		if err != nil {
			// fmt.Fprintf(os.Stderr, "unable to load %s: %v\n", pathname, err)
			/* logging? */
			continue
		}
		loader.scopes[grm.ScopeName] = grm
		for _, ft := range grm.FileTypes {
			ft = strings.TrimLeft(ft, ".")
			fts, _ := loader.filetypes[ft]
			loader.filetypes[ft] = append(fts, grm)
		}
	}
	return &loader, len(loader.scopes) > 0
}

func NewLoaderFromDir(dir string, walk bool) (*Loader, bool) {
	if walk {
		return NewLoader(func(yield func(string) bool) {
			filepath.WalkDir(dir, func(pathname string, d fs.DirEntry, err error) error {
				if !d.IsDir() {
					if !yield(path.Join(dir, pathname)) {
						return filepath.SkipAll
					}
				}
				return nil
			})
		})
	} else {
		return NewLoader(func(yield func(string) bool) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					if !yield(path.Join(dir, entry.Name())) {
						return
					}
				}
			}
		})
	}
}

func (l *Loader) FromScope(scope string) (*Grammar, error) {
	grm, ok := l.scopes[scope]
	if !ok {
		return nil, os.ErrNotExist
	}
	return CompileGrammar(l, grm)
}

func (l *Loader) FromFileType(ft string, index int) (*Grammar, error) {
	grms, ok := l.filetypes[ft]
	if !ok || index >= len(grms) {
		return nil, os.ErrNotExist
	}
	return CompileGrammar(l, grms[index])
}

func (l *Loader) Scopes() iter.Seq[string] {
	return maps.Keys(l.scopes)
}

func (l *Loader) FileTypes() iter.Seq[string] {
	return maps.Keys(l.filetypes)
}

func (l *Loader) FileTypeNames() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		for ft, grms := range l.filetypes {
			var names []string
			for _, grm := range grms {
				names = append(names, grm.Name)
			}
			if !yield(ft, names) {
				return
			}
		}
	}
}
