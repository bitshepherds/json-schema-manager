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

// Change represents a file status detected in the repository.
type Change struct {
	Path      string
	IsNew     bool // True if status is 'A' (Added)
	IsDeleted bool // True if status is 'D' (Deleted)
}

// Gitter defines the interface for git repository operations.
type Gitter interface {
	// GetLatestAnchor finds the latest deployment tag for an environment.
	// If no tag is found, it returns the repository's initial commit.
	GetLatestAnchor(ctx context.Context, env config.Env) (Revision, error)

	// TagDeploymentSuccess creates and pushes a new environment-specific deployment tag.
	TagDeploymentSuccess(ctx context.Context, env config.Env) (string, error)

	// GetSchemaChanges identifies files with the given suffix changed between the anchor and HEAD.
	GetSchemaChanges(ctx context.Context, anchor Revision, sourceDir, suffix string) ([]Change, error)
}
