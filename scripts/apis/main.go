// Package main provides a script which builds the demo registry's collapsed schemas, and checks
// that the AsyncAPI and OpenAPI examples in examples/apis, which refer to them, are valid. It
// needs Node.js.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The validators, pinned so the check is repeatable.
const (
	asyncapi = "@asyncapi/cli@6.2.2"
	redocly  = "@redocly/cli@2.70.0"
)

func main() {
	if _, err := exec.LookPath("npx"); err != nil {
		fail("npx not found: install Node.js to check the API examples")
	}
	repoRoot, err := os.Getwd()
	check(err)
	work, err := os.MkdirTemp("", "jsm-apis-")
	check(err)

	jsm := filepath.Join(work, "jsm")
	run("go", "build", "-o", jsm, "./cmd/jsm")
	// build-dist writes to dist/ at the root of the git repository, where the examples refer
	run(jsm, "-r", filepath.Join(repoRoot, "demo-registry"), "build-dist", "prod", "--all", "--collapse")

	run("npx", "--yes", asyncapi, "validate", filepath.Join("examples", "apis", "customer-events.asyncapi.yaml"))
	run(
		"npx",
		"--yes",
		redocly,
		"lint",
		"--extends=minimal",
		filepath.Join("examples", "apis", "customer.openapi.yaml"),
	)

	_ = os.RemoveAll(work)
	fmt.Println("✅ The API examples are valid")
}

func run(name string, args ...string) {
	fmt.Println("$", name, strings.Join(args, " "))
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail(fmt.Sprintf("%s failed: %v", name, err))
	}
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Println("❌ " + msg)
	os.Exit(1)
}
