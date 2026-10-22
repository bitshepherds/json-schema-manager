package app

import (
	"io"
	"log/slog"

	"github.com/spf13/cobra/doc"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

// ReferenceDir is where the generated command reference lives, relative to the repo root.
const ReferenceDir = "user-docs/reference"

// WriteReference writes the Markdown command reference, one page per command, into dir.
func WriteReference(dir string) error {
	root := NewRootCmd(&LazyManager{}, new(slog.LevelVar), io.Discard, io.Discard, fsh.NewEnvProvider())
	// The banner's colour codes don't belong in Markdown
	root.Long = LongDescription
	// Leave out the generation date, so the committed copy only changes when the CLI does
	root.DisableAutoGenTag = true
	return doc.GenMarkdownTree(root, dir)
}
