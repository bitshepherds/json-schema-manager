// Package main provides a script which runs the commands in user-docs/generating-models.md against
// the demo registry, and checks that the generated TypeScript and Go compile. It needs Node.js.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The generators and compiler the guide uses, pinned so the check is repeatable.
const (
	json2ts    = "json-schema-to-typescript@15.0.4"
	quicktype  = "quicktype@23.0.176"
	typescript = "typescript@7.0.2"
)

// customer refers to two other schemas, so collapsing it inlines them.
const customer = "domain-c_domain-c-a_customer_1_0_0"

func main() {
	if _, err := exec.LookPath("npx"); err != nil {
		fail("npx not found: install Node.js to check the generated models")
	}
	repoRoot, err := os.Getwd()
	check(err)
	// The working directory is kept if the check fails, to inspect what was generated
	work, err := os.MkdirTemp("", "jsm-models-")
	check(err)
	workDir = work

	jsm := filepath.Join(work, "jsm")
	run(repoRoot, "go", "build", "-o", jsm, "./cmd/jsm")
	registry := filepath.Join(repoRoot, "demo-registry")

	// One schema, to TypeScript and Go
	collapsed := runOutput(work, jsm, "-r", registry, "render-schema", customer, "--collapse")
	check(os.WriteFile(filepath.Join(work, "customer.schema.json"), collapsed, 0o600))

	run(work, "npx", "--yes", json2ts, "-i", "customer.schema.json", "-o", "customer.ts")
	run(work, "npx", "--yes", "-p", typescript, "tsc", "--noEmit", "--strict", "customer.ts")

	goDir := filepath.Join(work, "customer")
	check(os.MkdirAll(goDir, 0o755))
	run(work, "npx", "--yes", quicktype, "--src-lang", "schema", "--lang", "go", "--package", "customer",
		"--top-level", "Customer", "-o", "customer/customer.go", "customer.schema.json")
	check(os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.com/customer\n\ngo 1.21\n"), 0o600))
	run(goDir, "go", "vet", "./...")

	// A whole environment, to TypeScript. build-dist writes to dist/ at the root of the git repository.
	run(work, jsm, "-r", registry, "build-dist", "prod", "--all", "--collapse")
	built := filepath.Join(repoRoot, "dist", "prod", "private", "*.schema.json")
	run(work, "npx", "--yes", json2ts, "-i", built, "-o", "models/")
	tsFiles, err := filepath.Glob(filepath.Join(work, "models", "*.ts"))
	check(err)
	if len(tsFiles) == 0 {
		fail("no TypeScript was generated for the environment")
	}
	run(work, "npx", append([]string{"--yes", "-p", typescript, "tsc", "--noEmit", "--strict"}, tsFiles...)...)

	_ = os.RemoveAll(work)
	fmt.Println("✅ The generated models compile")
}

// workDir is where the models are generated.
var workDir string

func run(dir, name string, args ...string) {
	fmt.Println("$", name, strings.Join(args, " "))
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail(fmt.Sprintf("%s failed: %v", name, err))
	}
}

func runOutput(dir, name string, args ...string) []byte {
	fmt.Println("$", name, strings.Join(args, " "))
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fail(fmt.Sprintf("%s failed: %v", name, err))
	}
	return out
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Println("❌ " + msg)
	if workDir != "" {
		fmt.Println("The generated files are in " + workDir)
	}
	os.Exit(1)
}
