package schema

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// RootDirEnvVar is the environment variable used to set the registry root directory.
const RootDirEnvVar = "JSM_REGISTRY_ROOT_DIR"

// Registry is the object which represents a JSM Registry, and also stores the schemas in memory.
type Registry struct {
	rootDirectory string
	source        RootSource
	ignoredEnv    string
	config        *config.Config
	cache         Cache
	compiler      validator.Compiler
	pathResolver  fsh.PathResolver
	envProvider   fsh.EnvProvider
	mu            sync.RWMutex       // Protects cache
	loadGroup     singleflight.Group // Prevents duplicate loads
	renderGroup   singleflight.Group // Prevents duplicate renders/compilations
}

// RootSource says where the registry jsm uses was named.
type RootSource int

const (
	// SourceCurrentDir is the current directory.
	SourceCurrentDir RootSource = iota
	// SourceFlag is the --registry flag.
	SourceFlag
	// SourceEnv is the JSM_REGISTRY_ROOT_DIR environment variable.
	SourceEnv
)

// String names the source as a person reads it, such as "the current directory".
func (s RootSource) String() string {
	switch s {
	case SourceFlag:
		return "--registry"
	case SourceEnv:
		return "$" + RootDirEnvVar
	default:
		return "the current directory"
	}
}

// RootChoice is the registry root jsm chose, and why.
type RootChoice struct {
	Dir    string     // the registry's canonical root directory
	Source RootSource // where it was named
	// IgnoredEnv is JSM_REGISTRY_ROOT_DIR's value, as given, when it was set and jsm used the
	// current directory's registry instead of the one it names, or of a folder with none.
	IgnoredEnv string
}

// NewRegistry creates a new JSM registry.
// Its root is rootDirectory when given; else the current directory, when it holds a registry's
// config file; else the environment variable JSM_REGISTRY_ROOT_DIR; else the current directory,
// which then fails with a NoRegistryError.
func NewRegistry(
	rootDirectory string,
	compiler validator.Compiler,
	pathResolver fsh.PathResolver,
	envProvider fsh.EnvProvider,
) (*Registry, error) {
	choice, err := initRootDirectory(rootDirectory, ".", pathResolver, envProvider)
	if err != nil {
		return nil, err
	}
	rd := choice.Dir

	cfg, err := config.New(rd, compiler)
	var missing *config.MissingConfigError
	if errors.As(err, &missing) && choice.Source == SourceCurrentDir {
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
		source:        choice.Source,
		ignoredEnv:    choice.IgnoredEnv,
		config:        cfg,
		pathResolver:  pathResolver,
		envProvider:   envProvider,
	}, nil
}

// initRootDirectory chooses the registry root directory: rd, the --registry flag, when given;
// else wd, the current directory, when it holds a registry's config file; else the environment
// variable JSM_REGISTRY_ROOT_DIR, when set; else wd.
func initRootDirectory(
	rd, wd string,
	pathResolver fsh.PathResolver,
	envProvider fsh.EnvProvider,
) (RootChoice, error) {
	if rd != "" {
		dir, err := checkRootDirectory(rd, SourceFlag, pathResolver)
		return RootChoice{Dir: dir, Source: SourceFlag}, err
	}

	env := envProvider.Get(RootDirEnvVar)
	if hasRegistryConfig(wd) {
		dir, err := checkRootDirectory(wd, SourceCurrentDir, pathResolver)
		if err != nil {
			return RootChoice{}, err
		}
		choice := RootChoice{Dir: dir, Source: SourceCurrentDir}
		if env != "" {
			if envDir, envErr := pathResolver.CanonicalPath(env); envErr != nil || envDir != dir {
				choice.IgnoredEnv = env
			}
		}
		return choice, nil
	}

	if env != "" {
		dir, err := checkRootDirectory(env, SourceEnv, pathResolver)
		return RootChoice{Dir: dir, Source: SourceEnv}, err
	}

	dir, err := checkRootDirectory(wd, SourceCurrentDir, pathResolver)
	return RootChoice{Dir: dir, Source: SourceCurrentDir}, err
}

// hasRegistryConfig reports whether dir holds a registry's config file.
func hasRegistryConfig(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, config.JsmRegistryConfigFile))
	return err == nil && !info.IsDir()
}

// Source says where the registry was named: --registry, the current directory, or JSM_REGISTRY_ROOT_DIR.
func (r *Registry) Source() RootSource {
	return r.source
}

// IgnoredEnv is JSM_REGISTRY_ROOT_DIR's value when jsm ignored it for the current directory's
// registry, and empty otherwise.
func (r *Registry) IgnoredEnv() string {
	return r.ignoredEnv
}

// checkRootDirectory returns the canonical path of rd, a candidate registry root named by
// source, once it is known to be a directory.
func checkRootDirectory(rd string, source RootSource, pathResolver fsh.PathResolver) (string, error) {
	rdc, err := pathResolver.CanonicalPath(rd)
	if err != nil {
		return "", &RegistryInitError{Path: rd, Source: source.String(), Err: err}
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
	if want := c.Path(HomeDir, r.rootDirectory); filepath.Dir(path) != want {
		return "", &MisplacedSchemaError{Path: path, BelongsIn: want}
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
// The family's declaration, family.yml, is written beside its version directories, from spec.
func (r *Registry) CreateSchema(domainAndFamilyName string, spec FamilySpec) (s *Schema, err error) {
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

	declaration, err := spec.content()
	if err != nil {
		return nil, err
	}
	declarationPath := r.FamilyDeclarationPath(key)
	if _, statErr := os.Stat(declarationPath); statErr == nil {
		return nil, &FamilyAlreadyDeclaredError{Path: declarationPath}
	}

	if iErr := r.initNewRegistrySchema(s); iErr != nil {
		return nil, iErr
	}

	return s, os.WriteFile(declarationPath, declaration, 0o600)
}

// CreateSchemaVersion creates a new version in an existing schema family, based
// on the version of the schema identified by Key, and the specified ReleaseType.
// The new schema version will be the next logical version.
// For example, if the schema identified by Key has version 1.2.3, and the
// ReleaseType is ReleaseTypeMajor, the new schema version will be 2.0.0.
// If the ReleaseType is ReleaseTypeMinor, the new schema version will be 1.3.0.
// If the ReleaseType is ReleaseTypePatch, the new schema version will be 1.2.4.
//
// A new version in the same major copies only the schema when its family inherits documents, since
// earlier documents already run on it. Otherwise it copies the documents too: a major's documents may
// stop holding, so they're a starting point to edit, and with no inheritance nothing else would test it.
func (r *Registry) CreateSchemaVersion(k Key, rt ReleaseType) (*Schema, error) {
	cs, err := r.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}

	decl, err := r.FamilyDeclaration(k)
	if err != nil {
		return nil, err
	}

	ns := New(k, r)
	ns.BumpVersion(rt)

	duplicate := ns.DuplicateSchemaFiles
	if rt != ReleaseTypeMajor && decl.Facts.InheritsDocuments() {
		duplicate = ns.CopySchemaFile
	}
	if dErr := duplicate(cs); dErr != nil {
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

// CreateNewSchemaVersion creates a new version of the schema at the given path, as CreateSchemaVersion does.
func (r *Registry) CreateNewSchemaVersion(path string, rt ReleaseType) (*Schema, error) {
	cs, err := r.GetSchema(path)
	if err != nil {
		return nil, err
	}
	return r.CreateSchemaVersion(cs.Key(), rt)
}
