// Package main provides a script to build the jsm binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	binaryName := "jsm"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	// Get version
	versionCmd := exec.CommandContext(context.Background(), "go", "run", "scripts/version/main.go")
	versionOut, _ := versionCmd.Output()
	version := string(versionOut)

	ldflags := fmt.Sprintf("-X github.com/bitshepherds/json-schema-manager/v2/internal/app.Version=%s", version)

	// Ensure bin directory exists
	if err := os.MkdirAll("bin", 0o750); err != nil {
		fmt.Printf("❌ Failed to create bin directory: %v\n", err)
		os.Exit(1)
	}

	outputPath := filepath.Join("bin", binaryName)
	fmt.Printf("Building %s...\n", version)

	cmd := exec.CommandContext(
		context.Background(),
		"go",
		"build",
		"-ldflags",
		ldflags,
		"-o",
		outputPath,
		"cmd/jsm/main.go",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Build failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Build complete: %s\n", outputPath)
}
