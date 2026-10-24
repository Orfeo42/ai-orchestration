package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type Source struct {
	Path       string
	YAML       []byte
	Hash       string
	Definition Definition
}

func LoadDir(dir string, reg step.Catalog) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read workflow dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)

	var sources []Source
	byName := map[string]string{}
	for _, n := range names {
		path := filepath.Join(dir, n)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		def, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := def.Validate(reg); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if first, dup := byName[def.Name]; dup {
			return nil, fmt.Errorf("%w: %s: workflow name %q already defined in %s", ErrInvalid, path, def.Name, first)
		}
		byName[def.Name] = path
		sources = append(sources, Source{Path: path, YAML: raw, Hash: Hash(raw), Definition: def})
	}
	return sources, nil
}
