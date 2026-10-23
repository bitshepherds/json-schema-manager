package schema

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
)

// RegistryRootAtGitRootError is returned when a distribution is built from a registry whose root is the
// git root. The dist directory is written next to the registry root, which would be outside the repository.
type RegistryRootAtGitRootError struct {
	Path string
}

func (e *RegistryRootAtGitRootError) Error() string {
	return fmt.Sprintf("registry root cannot be the same as the git root: %s. build-dist writes dist next to "+
		"the registry root, which would be outside the repository: move the registry into a subdirectory", e.Path)
}

// DistBuilder is an interface for building schema distributions.
type DistBuilder interface {
	BuildAll(ctx context.Context, env config.Env, collapse bool) (int, error)
	BuildChanged(ctx context.Context, env config.Env, anchor repo.Revision, collapse bool) (int, error)
	SetNumWorkers(n int)
}

// FSDistBuilder builds distribution directories of rendered schemas on the filesystem.
type FSDistBuilder struct {
	registry    *Registry
	config      *config.Config
	gitter      repo.Gitter
	distDirName string
	distDir     string // Found when a build starts, so that commands which don't build need no dist directory
	numWorkers  int
}

// NewFSDistBuilder creates a new DistBuilder for the given registry and config, which writes to a directory
// called distDirName next to the registry root.
func NewFSDistBuilder(r *Registry, cfg *config.Config, g repo.Gitter, distDirName string) DistBuilder {
	return &FSDistBuilder{
		registry:    r,
		config:      cfg,
		gitter:      g,
		distDirName: distDirName,
		numWorkers:  runtime.GOMAXPROCS(0),
	}
}

// SetNumWorkers controls the number of rendering workers.
func (b *FSDistBuilder) SetNumWorkers(n int) {
	b.numWorkers = n
}

// BuildAll renders all schemas in the registry in parallel for the given environment.
func (b *FSDistBuilder) BuildAll(ctx context.Context, env config.Env, collapse bool) (int, error) {
	if err := b.ensureDistDir(ctx, env); err != nil {
		return 0, err
	}

	searcher, err := NewSearcher(b.registry, "")
	if err != nil {
		return 0, err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	resultC := searcher.Schemas(runCtx)

	var wg sync.WaitGroup
	sem := make(chan struct{}, b.numWorkers)

	var finalErr error
	var errOnce sync.Once
	var count int
	var countMu sync.Mutex

Loop:
	for res := range resultC {
		if res.Err != nil {
			errOnce.Do(func() {
				finalErr = res.Err
				cancelRun()
			})
			break Loop
		}

		select {
		case <-runCtx.Done():
			break Loop
		case sem <- (struct{}{}):
		}

		wg.Add(1)
		go func(k Key) {
			defer wg.Done()
			defer func() { <-sem }()

			if rErr := b.renderAndWrite(runCtx, env, k, collapse); rErr != nil {
				errOnce.Do(func() {
					finalErr = rErr
					cancelRun()
				})
				return
			}

			countMu.Lock()
			count++
			countMu.Unlock()
		}(res.Key)
	}

	wg.Wait()

	if ctx.Err() != nil {
		return count, ctx.Err()
	}

	if finalErr != nil {
		return count, finalErr
	}

	return count, nil
}

// BuildChanged renders schemas that have changed since the given anchor for the given environment.
func (b *FSDistBuilder) BuildChanged(
	ctx context.Context,
	env config.Env,
	anchor repo.Revision,
	collapse bool,
) (int, error) {
	if err := b.ensureDistDir(ctx, env); err != nil {
		return 0, err
	}

	changes, err := b.gitter.GetSchemaChanges(ctx, anchor, b.registry.RootDirectory(), SchemaSuffix)
	if err != nil {
		return 0, err
	}

	var count int
	for _, change := range changes {
		if change.IsDeleted {
			continue
		}
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		k, kErr := b.registry.KeyFromSchemaPath(change.Path)
		if kErr != nil {
			// Skip files that don't map to valid keys
			continue
		}

		if rwErr := b.renderAndWrite(ctx, env, k, collapse); rwErr != nil {
			return count, rwErr
		}
		count++
	}

	return count, nil
}

// renderAndWrite renders a single schema and writes it to the dist directory for the given environment.
func (b *FSDistBuilder) renderAndWrite(ctx context.Context, env config.Env, k Key, collapse bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	ec, err := b.config.EnvConfig(env)
	if err != nil {
		return fmt.Errorf("failed to get environment config for %s: %w", env, err)
	}

	s, err := b.registry.GetSchemaByKey(k)
	if err != nil {
		return fmt.Errorf("failed to get schema %s: %w", k, err)
	}

	ri, err := b.registry.CoordinateRender(s, ec)
	if err != nil {
		return fmt.Errorf("failed to render schema %s: %w", k, err)
	}

	output := ri.Rendered
	if collapse {
		inliner := NewInliner(b.registry, ec)
		collapsed, cErr := inliner.Collapse(ri.Rendered)
		if cErr != nil {
			return fmt.Errorf("failed to collapse schema %s: %w", k, cErr)
		}
		if vErr := inliner.Verify(s, ri.Validator, collapsed); vErr != nil {
			return vErr
		}
		output = collapsed
	}

	envDir := filepath.Join(b.distDir, string(env))
	subDir := "private"
	if s.IsPublic() {
		subDir = "public"
	}

	outputPath := filepath.Join(envDir, subDir, s.Filename())
	if wErr := os.WriteFile(outputPath, output, 0o600); wErr != nil {
		return fmt.Errorf("failed to write schema %s: %w", k, wErr)
	}

	return nil
}

// ensureDistDir finds the distribution directory, and prepares it for the given environment.
func (b *FSDistBuilder) ensureDistDir(ctx context.Context, env config.Env) error {
	distDir, err := distDirectory(ctx, b.registry.pathResolver, b.registry.RootDirectory(), b.distDirName)
	if err != nil {
		return err
	}
	b.distDir = distDir
	return b.prepareDistDir(env)
}

// prepareDistDir empties the distribution directory for the given environment, creating it if need be.
func (b *FSDistBuilder) prepareDistDir(env config.Env) error {
	// Ensure parent dist directory exists
	if err := os.MkdirAll(b.distDir, 0o750); err != nil {
		return fmt.Errorf("failed to create base dist directory: %w", err)
	}

	envDir := filepath.Join(b.distDir, string(env))

	// Clean environment-specific directory
	if err := os.RemoveAll(envDir); err != nil {
		return fmt.Errorf("failed to clean environment dist directory %s: %w", env, err)
	}

	// Recreate environment-specific directory with public/private subdirectories
	for _, sub := range []string{"public", "private"} {
		if err := os.MkdirAll(filepath.Join(envDir, sub), 0o750); err != nil {
			return fmt.Errorf("failed to create environment dist subdirectory %s/%s: %w", env, sub, err)
		}
	}

	return nil
}

// distDirectory finds the directory to use for the distribution artefacts.
// It returns an error if the registry root is the same as the git root.
// Otherwise it returns a sibling directory of the registry root.
func distDirectory(
	ctx context.Context,
	pathResolver fsh.PathResolver,
	registryRoot, distDirName string,
) (string, error) {
	//nolint:gosec // CMD arguments are internal and path is from the registry
	cmd := exec.CommandContext(ctx, "git", "-C", registryRoot, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		// If git fails, we assume we're not in a git repo and just return a sibling
		return filepath.Join(filepath.Dir(registryRoot), distDirName), nil //nolint:nilerr // fallback
	}

	gitRoot, err := pathResolver.CanonicalPath(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}

	if registryRoot == gitRoot {
		return "", &RegistryRootAtGitRootError{Path: registryRoot}
	}

	return filepath.Join(filepath.Dir(registryRoot), distDirName), nil
}
