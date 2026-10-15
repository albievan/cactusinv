// Package migrations embeds the goose migrations so the binary can apply them.
package migrations

import (
	"embed"
	"io/fs"
	"slices"
	"strings"
)

// files holds every SQL migration: the main schema (*.sql, also read by sqlc) and the
// trigger migrations in triggers/ (kept apart because sqlc cannot parse triggers).
//
//go:embed *.sql triggers/*.sql
var files embed.FS

// FS presents the root and triggers/ migrations as one flat directory, the layout goose
// expects.
var FS fs.FS = flat{}

type flat struct{}

func (flat) Open(name string) (fs.File, error) {
	if f, err := files.Open(name); err == nil || name == "." {
		return f, err
	}
	return files.Open("triggers/" + name)
}

func (flat) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, fs.ErrNotExist
	}
	root, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	trig, err := files.ReadDir("triggers")
	if err != nil {
		return nil, err
	}
	var out []fs.DirEntry
	for _, e := range append(root, trig...) {
		if !e.IsDir() {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out, nil
}
