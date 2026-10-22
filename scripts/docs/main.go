// Package main provides a script to regenerate the jsm command reference from the CLI.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitshepherds/json-schema-manager/internal/app"
)

func main() {
	// Remove the old pages first, so a removed command loses its page
	old, err := filepath.Glob(filepath.Join(app.ReferenceDir, "*.md"))
	if err == nil {
		for _, p := range old {
			err = os.Remove(p)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = os.MkdirAll(app.ReferenceDir, 0o755)
	}
	if err == nil {
		err = app.WriteReference(app.ReferenceDir)
	}
	if err != nil {
		fmt.Printf("❌ Generating the command reference failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Command reference written to " + app.ReferenceDir)
}
