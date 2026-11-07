// Package main provides a script which builds the demo registry's collapsed schemas, and checks
// that the AsyncAPI and OpenAPI examples in examples/apis, which refer to them, are valid. It
// also checks that each validator rejects the deliberately broken examples in
// scripts/apis/testdata, so an upgrade which stops a validator catching mistakes fails the check.
// It needs Bun, and runs the validators with bunx --bun, so they run on Bun's runtime even where
// Node.js is installed.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// The validators, pinned so the check is repeatable.
const (
	asyncapi = "@asyncapi/cli@6.3.0"
	redocly  = "@redocly/cli@2.76.0"
)

// A broken example, and the rule a validator must report when it rejects it. Matching the rule,
// not only the exit code, means a validator which crashes doesn't pass for one which rejects.
type broken struct {
	file string
	rule string
}

func main() {
	if _, err := exec.LookPath("bunx"); err != nil {
		fail(
			"bunx not found: install Bun with 'make setup' (macOS/Linux) or '.\\win-make.ps1 setup' (Windows) " +
				"to check the API examples",
		)
	}
	repoRoot, err := os.Getwd()
	check(err)
	work, err := os.MkdirTemp("", "jsm-apis-")
	check(err)

	jsm := filepath.Join(work, "jsm")
	run("go", "build", "-o", jsm, "./cmd/jsm")
	// build-dist writes to dist/ at the root of the git repository, where the examples refer
	run(jsm, "-r", filepath.Join(repoRoot, "demo-registry"), "build-dist", "prod", "--all", "--collapse")

	asyncapiArgs := []string{"--bun", asyncapi, "validate"}
	redoclyArgs := []string{"--bun", redocly, "lint", "--extends=minimal"}
	examples := filepath.Join("examples", "apis")
	run("bunx", append(asyncapiArgs, filepath.Join(examples, "customer-events.asyncapi.yaml"))...)
	run("bunx", append(redoclyArgs, filepath.Join(examples, "customer.openapi.yaml"))...)

	testdata := filepath.Join("scripts", "apis", "testdata")
	for _, b := range []broken{
		{filepath.Join(testdata, "invalid-document.asyncapi.yaml"), "asyncapi-document-resolved"},
		{filepath.Join(testdata, "missing-schema.asyncapi.yaml"), "invalid-ref"},
	} {
		expectRejected(b, "bunx", append(asyncapiArgs, b.file)...)
	}
	for _, b := range []broken{
		{filepath.Join(testdata, "invalid-document.openapi.yaml"), "the struct rule"},
		{filepath.Join(testdata, "missing-schema.openapi.yaml"), "the no-unresolved-refs rule"},
	} {
		expectRejected(b, "bunx", append(redoclyArgs, b.file)...)
	}

	_ = os.RemoveAll(work)
	fmt.Println("✅ The API examples are valid, and the broken ones are rejected")
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

// expectRejected runs a validator on a broken example, and fails unless the validator exits
// non-zero and reports the rule the example breaks.
func expectRejected(b broken, name string, args ...string) {
	fmt.Println("$", name, strings.Join(args, " "), "(expected to fail)")
	var out bytes.Buffer
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	if err := cmd.Run(); err == nil {
		fail(fmt.Sprintf("%s accepted the broken example %s", name, b.file))
	}
	if !reportsRule(out.String(), b.rule) {
		fail(fmt.Sprintf("%s rejected %s, but didn't report %q", name, b.file, b.rule))
	}
	fmt.Printf("✅ %s was rejected, as expected\n", b.file)
}

// reportsRule reports whether a validator's output names the rule. Redocly colours the rule name
// when CI and GITHUB_ACTIONS are set, on Windows, or at a terminal, which splits the text with
// escape sequences, so they are removed before matching.
func reportsRule(out, rule string) bool {
	return strings.Contains(ansiEscape.ReplaceAllString(out, ""), rule)
}

// ansiEscape matches a terminal escape sequence: a CSI sequence such as a colour, or an OSC
// sequence such as a hyperlink.
var ansiEscape = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Println("❌ " + msg)
	os.Exit(1)
}
