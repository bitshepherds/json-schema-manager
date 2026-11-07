// Package main provides a script to run tests and check coverage.
package main

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
)

// defaultTimeout limits each package's test binary. Without it, a stuck run waits for Go's default of 10 minutes.
// The slowest package, cmd/jsm, has taken at most 21.8 seconds in CI. It goes first in the test arguments, so a later
// -timeout from the caller wins, and nothing after -args is affected.
const defaultTimeout = "-timeout=45s"

func main() {
	args := os.Args[1:]
	testArgs, cfg := parseFlags(args)

	if cfg.isCoverageRun() {
		testArgs = setupCoverage(testArgs, cfg)
	}

	// Run tests
	_, err := exec.LookPath("gotestsum")
	if err == nil && !cfg.isCoverageRun() {
		runCommand("gotestsum", append([]string{"--"}, testArgs...))
	} else {
		runCommand("go", append([]string{"test"}, testArgs...))
	}

	// Post-test actions
	handlePostTest(cfg)
}

type testerConfig struct {
	checkCoverage bool
	showSummary   bool
	openBrowser   bool
	generateBadge bool
	coverageFile  string
}

func (c *testerConfig) isCoverageRun() bool {
	return c.checkCoverage || c.showSummary || c.openBrowser || c.generateBadge
}

func parseFlags(args []string) ([]string, *testerConfig) {
	testArgs := []string{defaultTimeout}
	cfg := &testerConfig{}

	for _, arg := range args {
		switch {
		case arg == "--test-race-coverage":
			cfg.checkCoverage = true
		case arg == "--summary":
			cfg.showSummary = true
		case arg == "--browser":
			cfg.openBrowser = true
		case arg == "--badge":
			cfg.generateBadge = true
		case strings.HasPrefix(arg, "-coverprofile="):
			cfg.coverageFile = strings.TrimPrefix(arg, "-coverprofile=")
			testArgs = append(testArgs, arg)
		default:
			testArgs = append(testArgs, arg)
		}
	}
	return testArgs, cfg
}

func setupCoverage(testArgs []string, cfg *testerConfig) []string {
	if cfg.coverageFile == "" {
		cfg.coverageFile = "coverage.out"
		testArgs = append(testArgs, "-coverprofile="+cfg.coverageFile)
	}
	// Ensure we are covering the internal packages
	hasCoverPkg := false
	for _, arg := range testArgs {
		if strings.HasPrefix(arg, "-coverpkg") {
			hasCoverPkg = true
			break
		}
	}
	if !hasCoverPkg {
		testArgs = append(testArgs, "-coverpkg=./internal/...")
	}
	return testArgs
}

func handlePostTest(cfg *testerConfig) {
	switch {
	case cfg.checkCoverage:
		checkCoverageThresholds(cfg.coverageFile)
	case cfg.showSummary:
		runCommand("go", []string{"tool", "cover", "-func", cfg.coverageFile})
	case cfg.openBrowser:
		runCommand("go", []string{"tool", "cover", "-html", cfg.coverageFile})
	case cfg.generateBadge:
		generateCoverageBadge(cfg.coverageFile)
	}
}

func runCommand(name string, args []string) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	// Clear Git environment variables to avoid conflicts with lefthook
	cmd.Env = os.Environ()
	for i := len(cmd.Env) - 1; i >= 0; i-- {
		if strings.HasPrefix(cmd.Env[i], "GIT_") {
			cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
		}
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Command failed: %v\n", err)
		os.Exit(1)
	}
}

func checkCoverageThresholds(coverageFile string) {
	cmd := exec.CommandContext(context.Background(), "go", "tool", "cover", "-func", coverageFile)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("❌ Error running go tool cover: %v\n", err)
		os.Exit(1)
	}

	failures, totalLine := parseCoverageOutput(output)

	if len(failures) > 0 {
		fmt.Println("\n❌ Coverage check failed! The following functions have less than 100% coverage:")
		for _, f := range failures {
			fmt.Printf("  %s\n", f)
		}
		os.Exit(1)
	}

	if totalLine != "" {
		fmt.Printf("\n📊 %s\n", totalLine)
	}
	fmt.Printf("✅ Coverage check passed - 100%% coverage of internal packages achieved, " +
		"excluding approved exceptions\n")
}

func generateCoverageBadge(coverageFile string) {
	cmd := exec.CommandContext(context.Background(), "go", "tool", "cover", "-func", coverageFile)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("❌ Error running go tool cover: %v\n", err)
		os.Exit(1)
	}

	_, totalLine := parseCoverageOutput(output)
	if totalLine == "" {
		fmt.Println("❌ Could not find total coverage in output")
		os.Exit(1)
	}

	parts := strings.Fields(totalLine)
	percentageStr := parts[len(parts)-1] // e.g. "99.8%"

	var percentage float64
	if _, scanErr := fmt.Sscanf(strings.TrimSuffix(percentageStr, "%"), "%f", &percentage); scanErr != nil {
		fmt.Printf("❌ Error parsing coverage percentage: %v\n", scanErr)
		os.Exit(1)
	}

	percentageInt := int(math.Round(percentage))
	percentageDisplay := fmt.Sprintf("%d%%", percentageInt)

	colour := "#e05d44" // red
	switch {
	case percentage >= 100:
		colour = "#4c1" // green
	case percentage >= 90:
		colour = "#a4a61d" // yellowgreen
	case percentage >= 80:
		colour = "#dfb317" // yellow
	case percentage >= 70:
		colour = "#fe7d37" // orange
	}

	//nolint:misspell // SVG uses stop-color
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="104" height="20">
  <linearGradient id="b" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <mask id="a"><rect width="104" height="20" rx="3" fill="#fff"/></mask>
  <g mask="url(#a)">
    <path fill="#555" d="M0 0h67v20H0z"/>
    <path fill="%s" d="M67 0h37v20H67z"/>
    <path fill="url(#b)" d="M0 0h104v20H0z"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="DejaVu Sans,Verdana,Geneva,sans-serif" font-size="11">
    <text x="33.5" y="15" fill="#010101" fill-opacity=".3">coverage</text>
    <text x="33.5" y="14">coverage</text>
    <text x="84.5" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="84.5" y="14">%s</text>
  </g>
</svg>`, colour, percentageDisplay, percentageDisplay)

	err = os.WriteFile("coverage.svg", []byte(svg), 0o600)
	if err != nil {
		fmt.Printf("❌ Error writing coverage.svg: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Coverage badge generated: coverage.svg (%s)\n", percentageStr)
}

func parseCoverageOutput(output []byte) (failures []string, totalLine string) {
	// Function-level exclusions (Package:Function)
	exclusions := map[string]float64{
		// filepath.Abs() error path is unreachable on Darwin
		"github.com/bitshepherds/json-schema-manager/v2/internal/fsh/path_resolver.go:27": 75.0,
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "total:") {
			totalLine = line
			continue
		}

		if shouldSkipLine(line) {
			continue
		}

		if isLineExcluded(line, exclusions) {
			continue
		}

		if !strings.Contains(line, "100.0%") {
			failures = append(failures, line)
		}
	}
	return failures, totalLine
}

func shouldSkipLine(line string) bool {
	if !strings.Contains(line, ":") {
		return true
	}
	if strings.Contains(line, "/scripts/") {
		return true
	}
	// Skip main functions in main.go as they are entry points
	if strings.Contains(line, "main.go") && strings.Contains(line, "main") {
		return true
	}
	return false
}

func isLineExcluded(line string, exclusions map[string]float64) bool {
	for pattern, threshold := range exclusions {
		if !strings.Contains(line, pattern) {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		percentageStr := strings.TrimSuffix(parts[len(parts)-1], "%")
		var percentage float64
		if _, err := fmt.Sscanf(percentageStr, "%f", &percentage); err == nil {
			if percentage >= threshold {
				return true
			}
		}
	}
	return false
}
