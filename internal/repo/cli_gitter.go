// Package repo provides git repository integration for JSM.
package repo

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

// CLIGitter is the concrete implementation of Gitter using the git CLI.
type CLIGitter struct {
	cfg          *config.Config
	pathResolver fsh.PathResolver
	repoRoot     string
	gitBinary    string
}

// NewCLIGitter creates a new CLIGitter instance.
func NewCLIGitter(cfg *config.Config, pathResolver fsh.PathResolver, repoRoot string) *CLIGitter {
	return &CLIGitter{
		cfg:          cfg,
		pathResolver: pathResolver,
		repoRoot:     repoRoot,
		gitBinary:    "git",
	}
}

// SetGitBinary allows overriding the git binary path (primarily for testing).
func (g *CLIGitter) SetGitBinary(bin string) {
	g.gitBinary = bin
}

// getEnvConfig looks up the EnvConfig for the given environment.
func (g *CLIGitter) getEnvConfig(env config.Env) (*config.EnvConfig, error) {
	return g.cfg.EnvConfig(env)
}

// tagPrefix returns the tag prefix for the given environment.
func (g *CLIGitter) tagPrefix(env config.Env) string {
	return fmt.Sprintf("%s/%s", JSMDeployTagPrefix, env)
}

// GetLatestAnchor finds the latest deployment tag for an environment.
// If no tag is found, it returns the repository's initial commit.
func (g *CLIGitter) GetLatestAnchor(ctx context.Context, env config.Env) (Revision, error) {
	if _, err := g.getEnvConfig(env); err != nil {
		return "", err
	}

	tagPattern := fmt.Sprintf("%s/*", g.tagPrefix(env))

	// --abbrev=0 finds the closest reachable tag matching the pattern
	//nolint:gosec // CMD arguments are internal
	cmd := exec.CommandContext(ctx, g.gitBinary, "describe", "--tags", "--match", tagPattern, "--abbrev=0")
	cmd.Dir = g.repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		// Fallback: Get the root commit (Day Zero)
		//nolint:gosec // CMD arguments are internal
		revCmd := exec.CommandContext(ctx, g.gitBinary, "rev-list", "--max-parents=0", "HEAD")
		revCmd.Dir = g.repoRoot
		revOut, rErr := revCmd.Output()
		if rErr != nil {
			return "", fmt.Errorf("could not find git history: %w", rErr)
		}
		return Revision(strings.TrimSpace(string(revOut))), nil
	}

	return Revision(strings.TrimSpace(out.String())), nil
}

// getGitRoot finds the top-level directory of the git repository.
func (g *CLIGitter) getGitRoot(ctx context.Context) (string, error) {
	//nolint:gosec // CMD arguments are internal
	cmd := exec.CommandContext(ctx, g.gitBinary, "rev-parse", "--show-toplevel")
	cmd.Dir = g.repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find git root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// TagDeploymentSuccess creates and pushes a new environment-specific deployment tag.
func (g *CLIGitter) TagDeploymentSuccess(ctx context.Context, env config.Env) (string, error) {
	if _, err := g.getEnvConfig(env); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102-150405")
	tagName := fmt.Sprintf("%s/%s", g.tagPrefix(env), timestamp)

	// 1. Create the local annotated tag
	//nolint:gosec // CMD arguments are internal
	tagCmd := exec.CommandContext(
		ctx,
		g.gitBinary,
		"tag",
		"-a",
		tagName,
		"-m",
		fmt.Sprintf("Successful JSM deployment to %s", env),
	)
	tagCmd.Dir = g.repoRoot
	if err := tagCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create git tag: %w", err)
	}

	// 2. Push the tag to origin
	//nolint:gosec // CMD arguments are internal
	pushCmd := exec.CommandContext(ctx, g.gitBinary, "push", "origin", tagName)
	pushCmd.Dir = g.repoRoot
	if err := pushCmd.Run(); err != nil {
		return tagName, fmt.Errorf("failed to push git tag to origin: %w", err)
	}

	return tagName, nil
}

// GetSchemaChanges identifies files with the given suffix changed between the anchor and HEAD.
func (g *CLIGitter) GetSchemaChanges(ctx context.Context, anchor Revision, sourceDir, suffix string) ([]Change, error) {
	if !filepath.IsAbs(sourceDir) {
		sourceDir = filepath.Join(g.repoRoot, sourceDir)
	}
	absSourceDir, err := g.pathResolver.Abs(sourceDir)
	if err != nil {
		return nil, err
	}

	root, err := g.getGitRoot(ctx)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // CMD arguments are internal and path is absolute
	cmd := exec.CommandContext(ctx, g.gitBinary, "diff", "--name-status", anchor.String(), "--", absSourceDir)
	cmd.Dir = g.repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w (output: %s)", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	changes := make([]Change, 0, len(lines))

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		path := fields[1]
		if !strings.HasSuffix(path, suffix) {
			continue
		}

		// git diff returns paths relative to the repo root.
		// Resolve these to absolute paths so they are correctly handled regardless of CWD.
		absPath := filepath.Join(root, path)

		changes = append(changes, Change{
			Path:      absPath,
			IsNew:     fields[0] == "A",
			IsDeleted: fields[0] == "D",
		})
	}
	return changes, nil
}
