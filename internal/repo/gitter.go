package repo

import (
	"context"

	"github.com/bitshepherds/json-schema-manager/internal/config"
)

// JSMDeployTagPrefix is the prefix used for deployment tags.
var JSMDeployTagPrefix = "jsm-deploy"

// Revision represents a specific git point-in-time (tag or hash).
type Revision string

func (r Revision) String() string { return string(r) }

// Git's empty tree, for each object format. Diffing against it reports every tracked file as added.
const (
	emptyTreeSHA1   Revision = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	emptyTreeSHA256 Revision = "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
)

// IsEmptyTree reports whether r is git's empty tree, the anchor used when nothing has been deployed.
func (r Revision) IsEmptyTree() bool {
	return r == emptyTreeSHA1 || r == emptyTreeSHA256
}

// Change represents a file status detected in the repository.
type Change struct {
	Path      string
	IsNew     bool // True if status is 'A' (Added)
	IsDeleted bool // True if status is 'D' (Deleted)
}

// Gitter defines the interface for git repository operations.
type Gitter interface {
	// GetLatestAnchor finds the latest deployment tag for an environment.
	// If no tag is found, it returns git's empty tree, so every schema counts as new,
	// or a ShallowCloneError if the history is too shallow to tell.
	GetLatestAnchor(ctx context.Context, env config.Env) (Revision, error)

	// TagDeploymentSuccess creates a new environment-specific deployment tag, and pushes it to origin if push is set.
	// If the push fails, it returns the created tag's name with the error.
	TagDeploymentSuccess(ctx context.Context, env config.Env, push bool) (string, error)

	// GetSchemaChanges identifies files with the given suffix changed between the anchor and HEAD.
	GetSchemaChanges(ctx context.Context, anchor Revision, sourceDir, suffix string) ([]Change, error)

	// ShowFile returns the content of the file at path, as returned in a Change, at the given revision.
	ShowFile(ctx context.Context, rev Revision, path string) ([]byte, error)
}
