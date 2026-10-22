package schema

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// RootDirEnvVar is the environment variable used to set the registry root directory.
const RootDirEnvVar = "JSM_REGISTRY_ROOT_DIR"

// Registry is the object which represents a JSM Registry, and also stores the schemas in memory.
type Registry struct {
	rootDirectory string
	config        *config.Config
	cache         Cache
	compiler      validator.Compiler
	pathResolver  fsh.PathResolver
	envProvider   fsh.EnvProvider
	mu            sync.RWMutex       // Protects cache
	loadGroup     singleflight.Group // Prevents duplicate loads
	renderGroup   singleflight.Group // Prevents duplicate renders/compilations
}

// NewRegistry creates a new JSM registry.
// If rootDirectory is empty, it will use the environment variable JSM_REGISTRY_ROOT_DIR.
func NewRegistry(
	rootDirectory string,
	compiler validator.Compiler,
	pathResolver fsh.PathResolver,
	envProvider fsh.EnvProvider,
) (*Registry, error) {
	rd, err := initRootDirectory(rootDirectory, pathResolver, envProvider)
	if err != nil {
		return nil, err
	}

	cfg, err := config.New(rd, compiler)
	var missing *config.MissingConfigError
	if errors.As(err, &missing) && rootDirectory == "" && envProvider.Get(RootDirEnvVar) == "" {
		return nil, &NoRegistryError{Dir: rd}
	}
	if err != nil {
		return nil, err
	}
	if fa, ok := compiler.(validator.FormatAsserter); ok && cfg.AssertFormat {
		fa.AssertFormat()
	}

	return &Registry{
		cache:         make(Cache),
		compiler:      compiler,
		rootDirectory: rd,
		config:        cfg,
		pathResolver:  pathResolver,
		envProvider:   envProvider,
	}, nil
}

// initRootDirectory attempts to initialise the registry root directory.
// If rootDirectory is empty, it will attempt to find the root directory from
// the environment variable JSM_REGISTRY_ROOT_DIR.
func initRootDirectory(rd string, pathResolver fsh.PathResolver, envProvider fsh.EnvProvider) (string, error) {
	source := "--registry"
	if rd == "" {
		rd = envProvider.Get(RootDirEnvVar)
		source = "$" + RootDirEnvVar
	}

	// If we get here, we have at least a string which *may* be a candidate.
	rdc, err := pathResolver.CanonicalPath(rd)
	if err != nil {
		return "", &RegistryInitError{Path: rd, Source: source, Err: err}
	}
	rd = rdc

	info, err := os.Stat(rd)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", &RegistryRootNotFolderError{Path: rd}
	}
	return rd, nil
}

// RootDirectory returns the root directory of the registry.
func (r *Registry) RootDirectory() string {
	return r.rootDirectory
}

// Config returns the registry configuration.
func (r *Registry) Config() (*config.Config, error) {
	if r.config == nil {
		return nil, errors.New("registry configuration not initialised")
	}
	return r.config, nil
}

// Reset clears all registered schemas from the compiler and invalidates
// the schema cache. This forces all schemas to be re-loaded from disk and
// re-registered with the compiler on next access.
func (r *Registry) Reset() {
	r.compiler.Clear()

	// Clear the entire schema cache so schemas are re-loaded from disk
	r.mu.Lock()
	r.cache = make(Cache)
	r.mu.Unlock()
}

// KeyFromSchemaPath converts a file path to a Key.
// It handles both absolute and relative paths, validates the file ends with SchemaSuffix,
// ensures it's a file (not a directory), and extracts the Key from the filename.
func (r *Registry) KeyFromSchemaPath(path string) (Key, error) {
	var err error

	path, err = r.pathResolver.CanonicalPath(path)
	if err != nil {
		return "", &NotFoundError{Path: path}
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	if info.IsDir() {
		return "", &NotASchemaFileError{Path: path}
	}

	if !strings.HasSuffix(path, SchemaSuffix) {
		return "", &NotASchemaFileError{Path: path}
	}

	// Verify the path is within the registry root directory
	rel, err := filepath.Rel(r.rootDirectory, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &LocationOutsideRootDirectoryError{Location: path, RootDirectory: r.rootDirectory}
	}

	// Strip the schema suffix from the filename before parsing as a Key
	stem := strings.TrimSuffix(filepath.Base(path), SchemaSuffix)
	c, err := NewCoreFromString(stem, KeySeparator)
	if err != nil {
		return "", err
	}
	return c.Key(), nil
}

// GetSchemaByKey loads a schema by its key with thread-safe cache access.
// If multiple goroutines request the same uncached schema, only one will load it.
func (r *Registry) GetSchemaByKey(k Key) (*Schema, error) {
	// Try to get from cache with read lock
	r.mu.RLock()
	if s, ok := r.cache[k]; ok {
		r.mu.RUnlock()
		return s, nil
	}
	r.mu.RUnlock()

	// Use singleflight to prevent duplicate loads
	v, err, _ := r.loadGroup.Do(string(k), func() (interface{}, error) {
		// Double-check cache after acquiring singleflight
		r.mu.RLock()
		if s, ok := r.cache[k]; ok {
			r.mu.RUnlock()
			return s, nil
		}
		r.mu.RUnlock()

		// Load the schema
		s, err := Load(k, r)
		if err != nil {
			return nil, err
		}

		// Store in cache with write lock
		r.mu.Lock()
		r.cache[k] = s
		r.mu.Unlock()

		return s, nil
	})

	if err != nil {
		return nil, err
	}

	// Check type assertion
	s, _ := v.(*Schema)
	return s, nil
}

// GetSchema either returns a previously loaded schema, or loads it (and the JSM
// schemas it depends on), then returns it.
func (r *Registry) GetSchema(path string) (*Schema, error) {
	k, err := r.KeyFromSchemaPath(path)
	if err != nil {
		return nil, err
	}
	return r.GetSchemaByKey(k)
}

// CreateSchema creates a new schema in the registry using the specified domain and family name
// compoents. Each component is separated by a forward slash.
// - e.g. "domain-a/family-name"
// - e.g. "domain-a/subdomain-a/family-name"
// There must be at least one domain component, and the family name is always the last component.
func (r *Registry) CreateSchema(domainAndFamilyName string) (s *Schema, err error) {
	if _, err = NewSearchScope(domainAndFamilyName); err != nil {
		return nil, err
	}
	c, err := NewCoreFromString(domainAndFamilyName+"/1/0/0", '/')
	if err != nil {
		return nil, &InvalidCreateSchemaArgError{Arg: domainAndFamilyName}
	}
	key := c.Key()

	// Check cache with read lock
	r.mu.RLock()
	_, exists := r.cache[key]
	r.mu.RUnlock()

	if exists {
		return nil, &AlreadyExistsError{K: key}
	}

	s = New(key, r)

	// Check if schema file already exists on disk to avoid overwriting
	if _, statErr := os.Stat(s.Path(FilePath)); statErr == nil {
		return nil, &AlreadyExistsError{K: key}
	}

	if iErr := r.initNewRegistrySchema(s); iErr != nil {
		return nil, iErr
	}

	return s, nil
}

// CreateSchemaVersion creates a new version in an existing schema family, based
// on the version of the schema identified by Key, and the specified ReleaseType.
// The new schema version will be the next logical version.
// For example, if the schema identified by Key has version 1.2.3, and the
// ReleaseType is ReleaseTypeMajor, the new schema version will be 2.0.0.
// If the ReleaseType is ReleaseTypeMinor, the new schema version will be 1.3.0.
// If the ReleaseType is ReleaseTypePatch, the new schema version will be 1.2.4.
func (r *Registry) CreateSchemaVersion(k Key, rt ReleaseType) (*Schema, error) {
	cs, err := r.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}

	ns := New(k, r)
	ns.BumpVersion(rt)

	if dErr := ns.DuplicateSchemaFiles(cs); dErr != nil {
		return nil, dErr
	}

	r.mu.Lock()
	r.cache[ns.Key()] = ns
	r.mu.Unlock()

	return ns, nil
}

// initNewRegistrySchema initialises a new schema in the registry.
func (r *Registry) initNewRegistrySchema(s *Schema) error {
	if err := s.WriteNewSchemaFiles(); err != nil {
		return err
	}

	r.mu.Lock()
	r.cache[s.Key()] = s
	r.mu.Unlock()

	return nil
}

// CoordinateRender coordinates the rendering and compilation of a schema using singleflight
// to ensure that parallel render requests for the same schema version are only executed once.
// Parallel requests will be quite common because JSON Schemas are often composed of other
// schemas (via $ref) and so a given worker rendering a schema might otherwise end up
// rendering the same schema as another worker when following the recursive $ref tree.
func (r *Registry) CoordinateRender(s *Schema, ec *config.EnvConfig) (RenderInfo, error) {
	key := string(s.Key()) + ":" + string(ec.Env)

	v, err, _ := r.renderGroup.Do(key, func() (interface{}, error) {
		// Double check the local cache inside the singleflight
		s.mu.Lock()
		ri := s.computed.RenderInfo(ec.Env)
		s.mu.Unlock()

		if ri.Validator != nil {
			return ri, nil
		}

		// Perform the actual rendering
		renderer := NewRenderer(s, ec)
		var err error
		ri.Rendered, ri.Unmarshalled, err = renderer.Render()
		if err != nil {
			return RenderInfo{}, err
		}

		id := s.CanonicalID(ec)

		// Register and compile with the validator. This is the part that was
		// causing the race condition.
		if addErr := s.registry.compiler.AddSchema(string(id), ri.Unmarshalled); addErr != nil {
			return RenderInfo{}, addErr
		}

		ri.Validator, err = s.registry.compiler.Compile(string(id))
		if err != nil {
			return RenderInfo{}, InvalidJSONSchemaError{Path: s.Path(FilePath), Wrapped: err}
		}

		// Cache the result back in the schema
		s.mu.Lock()
		s.computed.StoreRenderInfo(ec.Env, ri)
		s.mu.Unlock()

		return ri, nil
	})

	if err != nil {
		return RenderInfo{}, err
	}

	// Check type assertion
	ri, _ := v.(RenderInfo)
	return ri, nil
}

// CreateNewSchemaVersion creates a new version of a schema in the registry, along with its folders.
func (r *Registry) CreateNewSchemaVersion(path string, rt ReleaseType) (*Schema, error) {
	cs, err := r.GetSchema(path)
	if err != nil {
		return nil, err
	}

	// Create a new schema, initially setup to match the current schema.
	ns := New(cs.Key(), r)

	ns.BumpVersion(rt)
	if dErr := ns.DuplicateSchemaFiles(cs); dErr != nil {
		return nil, dErr
	}

	// Add to cache with write lock
	r.mu.Lock()
	r.cache[ns.Key()] = ns
	r.mu.Unlock()

	return ns, nil
}
