package schema

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ResolvedTarget represents the result of resolving a target for a CLI command.
// On successful resolving of the target, exactly one of Key or Scope will be non-nil.
type ResolvedTarget struct {
	Key   *Key
	Scope *SearchScope
}

// AllArg is a string which, if given as an arg to the Resolver, will cause it to resolve
// all schemas in the registry.
const AllArg = "all"

// TargetResolver is used to resolve which schemas will be targeted with a CLI command.
// It can be configured with an explicit ID, Key or Scope, or it will attempt to guess
// based on an argument - e.g. [arg] in "jsm validate [arg]".
// If the arg is the special string "all", it will be resolved to a SearchScope of "" which
// will match all schemas in the registry.
type TargetResolver struct {
	registry *Registry
	arg      string // The optional arg passed to the command - e.g. [path] in "jsm validate [path]"

	explicitID    *string
	explicitKey   *Key
	explicitScope *SearchScope
}

// NewTargetResolver creates a new TargetResolver.
func NewTargetResolver(r *Registry, arg string) *TargetResolver {
	return &TargetResolver{
		registry: r,
		arg:      arg,
	}
}

// SetID sets an explicit Canonical ID to resolve.
func (a *TargetResolver) SetID(id string) {
	a.explicitID = &id
}

// SetKey sets an explicit Key to resolve.
func (a *TargetResolver) SetKey(k Key) {
	a.explicitKey = &k
}

// SetScope sets an explicit SearchScope to resolve.
func (a *TargetResolver) SetScope(s SearchScope) {
	a.explicitScope = &s
}

// Resolve identifies what the argument represents and resolves it.
func (a *TargetResolver) Resolve() (ResolvedTarget, error) {
	if a.arg == AllArg {
		scope := SearchScope("")
		return ResolvedTarget{Scope: &scope}, nil
	}

	// 1. Explicit overrides
	if res, ok, err := a.resolveExplicit(); ok {
		return res, err
	}

	// 2. Try to guess based on the arg
	return a.resolveGuess()
}

func (a *TargetResolver) resolveExplicit() (ResolvedTarget, bool, error) {
	if a.explicitKey != nil {
		k, err := ParseKey(string(*a.explicitKey))
		if err != nil {
			return ResolvedTarget{}, true, err
		}
		return ResolvedTarget{Key: &k}, true, nil
	}
	if a.explicitID != nil {
		id := *a.explicitID
		if id == "" {
			return ResolvedTarget{}, true, &NoSchemaTargetsError{}
		}
		key, err := a.resolveIDtoKey(id)
		if err != nil {
			return ResolvedTarget{}, true, err
		}
		// resolveIDtoKey already validates via NewKey
		return ResolvedTarget{Key: &key}, true, nil
	}
	if a.explicitScope != nil {
		if *a.explicitScope != "" {
			if _, err := NewSearchScope(string(*a.explicitScope)); err != nil {
				return ResolvedTarget{}, true, err
			}
			return ResolvedTarget{Scope: a.explicitScope}, true, nil
		}
		return ResolvedTarget{}, true, &InvalidSearchScopeError{spec: ""}
	}
	return ResolvedTarget{}, false, nil
}

func (a *TargetResolver) resolveGuess() (ResolvedTarget, error) {
	arg := a.arg

	if arg == "" {
		return ResolvedTarget{}, &NoSchemaTargetsError{}
	}

	// Is it a URL?
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		key, err := a.resolveIDtoKey(arg)
		if err != nil {
			return ResolvedTarget{}, err
		}
		// resolveIDtoKey now returns a validated Key (via NewKey)
		return ResolvedTarget{Key: &key}, nil
	}

	// Is it a path to a file or directory?
	if info, err := os.Stat(arg); err == nil {
		return a.resolvePath(arg, info.IsDir())
	}

	// Not an existing path.
	// If it contains '_', guess it's a Key
	if strings.Contains(arg, KeySeparatorString) {
		k, err := NewKey(arg)
		if err != nil {
			return ResolvedTarget{}, err
		}
		return ResolvedTarget{Key: &k}, nil
	}

	// Otherwise, treat as SearchScope
	s, err := NewSearchScope(arg)
	if err != nil {
		return ResolvedTarget{}, err
	}
	return ResolvedTarget{Scope: &s}, nil
}

func (a *TargetResolver) resolvePath(path string, isDir bool) (ResolvedTarget, error) {
	if !isDir {
		// It's a file - resolve to Key
		key, err := a.registry.KeyFromSchemaPath(path)
		if err != nil {
			return ResolvedTarget{}, err
		}
		return ResolvedTarget{Key: &key}, nil
	}

	// It's a directory - resolve to SearchScope
	scope, err := a.resolvePathToScope(path)
	if err != nil {
		return ResolvedTarget{}, err
	}
	return ResolvedTarget{Scope: &scope}, nil
}

// resolveIDtoKey attempts to convert a Canonical ID back to a Key.
func (a *TargetResolver) resolveIDtoKey(idStr string) (Key, error) {
	u, err := url.Parse(idStr)
	if err != nil {
		return "", err
	}

	// The key is the filename minus the suffix.
	filename := filepath.Base(u.Path)
	if !strings.HasSuffix(filename, SchemaSuffix) {
		return "", &NotASchemaFileError{Path: idStr}
	}

	keyStr := strings.TrimSuffix(filename, SchemaSuffix)
	return NewKey(keyStr)
}

// resolvePathToScope converts an absolute or relative directory path to a SearchScope.
func (a *TargetResolver) resolvePathToScope(path string) (SearchScope, error) {
	cp, err := a.registry.pathResolver.CanonicalPath(path)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(a.registry.rootDirectory, cp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &LocationOutsideRootDirectoryError{Location: cp, RootDirectory: a.registry.rootDirectory}
	}

	if rel == "." {
		return "", nil // Registry root
	}

	// SearchScope uses '/' as separator regardless of OS
	scope := filepath.ToSlash(rel)
	return SearchScope(scope), nil
}

// ResolveScopeToSingleKey searches the registry for schemas matching the given scope.
// If exactly one schema is found, it returns its Key.
// If zero schemas are found, it returns a NotFoundError.
// If two or more schemas are found, it returns a TargetArgumentTargetsMultipleSchemasError.
func (a *TargetResolver) ResolveScopeToSingleKey(ctx context.Context, scope SearchScope, arg string) (Key, error) {
	searcher, err := NewSearcher(a.registry, scope)
	if err != nil {
		return "", &NotFoundError{Path: string(scope)}
	}

	keys := make([]Key, 0, 2)
	for res := range searcher.Schemas(ctx) {
		if res.Err != nil {
			return "", res.Err
		}
		keys = append(keys, res.Key)
		if len(keys) > 1 {
			return "", &TargetArgumentTargetsMultipleSchemasError{Arg: arg}
		}
	}

	if len(keys) == 0 {
		return "", &NotFoundError{Path: string(scope)}
	}

	return keys[0], nil
}
