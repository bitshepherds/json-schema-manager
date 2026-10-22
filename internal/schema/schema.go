package schema

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// ID is the canonical ID of a JSON Schema.
// It is the value of the schema $id property, and also the URL of production schema.
type ID string

// Cache is the type used to store schemas in memory.
type Cache map[Key]*Schema

// ReleaseType is the type used to identify the type of release for a new version of an existing schema.
type ReleaseType string

const (
	// ReleaseTypeMajor indicates a major version release.
	ReleaseTypeMajor ReleaseType = "major"
	// ReleaseTypeMinor indicates a minor version release.
	ReleaseTypeMinor ReleaseType = "minor"
	// ReleaseTypePatch indicates a patch version release.
	ReleaseTypePatch ReleaseType = "patch"
)

// NewReleaseType returns a ReleaseType from a string.
func NewReleaseType(s string) (ReleaseType, error) {
	s = strings.ToLower(s)
	switch s {
	case string(ReleaseTypeMajor):
		return ReleaseTypeMajor, nil
	case string(ReleaseTypeMinor):
		return ReleaseTypeMinor, nil
	case string(ReleaseTypePatch):
		return ReleaseTypePatch, nil
	default:
		return "", &InvalidReleaseTypeError{Value: s}
	}
}

// TestDocType describes a category of test document.
type TestDocType string

const (
	// TestDocTypePass is a test document for a version of a schema which it should validate.
	TestDocTypePass TestDocType = "pass"
	// TestDocTypeFail is a test document for a version of a schema which it should not validate.
	TestDocTypeFail TestDocType = "fail"
)

// PathType is the type used to identify a particular type of path to generate for a schema.
type PathType string

const (
	// FamilyDir is the path to the directory containing all the versions of a schema family.
	FamilyDir PathType = "familydir"
	// HomeDir is the path to the directory containing a schema version, test files etc.
	HomeDir PathType = "homedir"
	// FilePath is the absolute path of the schema file.
	FilePath PathType = "filepath"
)

// SchemaSuffix is the suffix for schema files.
const SchemaSuffix = ".schema.json"

// NewSchemaContent returns initial content for a new schema file.
func NewSchemaContent(draft validator.Draft) string {
	return `{
	"$schema": "` + string(draft) + `",
	"$id": "{{ ID }}",
	"type": "object",
	"properties": {}
}
`
}

// TemplateSource is an interface for something that can be cloned into a *template.Template.
type TemplateSource interface {
	Clone() (*template.Template, error)
}

// Schema represents a specific version of a schema family in the registry.
type Schema struct {
	core     *Core     // The core information about the schema
	registry *Registry // The registry this schema is in.

	// fields set after reading the schema file:
	exists   bool           // true if the schema file exists on disk
	srcDoc   []byte         // The source schema document
	tmpl     TemplateSource // The parsed template ready for execution
	isPublic bool           // true if the schema is intended to be published to the public

	// information lazily evaluated after reading the schema file:
	mu       sync.Mutex // Protects computed
	computed Computed
}

// New creates a new schema in the registry initialised to match the given key.
// To create the schema files on disk, see WriteNewSchemaFiles().
func New(k Key, registry *Registry) *Schema {
	core := NewCoreFromKey(k)

	s := &Schema{
		core:     core,
		registry: registry,
	}
	s.exists = false

	return s
}

// Load loads the schema source file with the given key into the registry, and returns it.
// Note that the schema will not be rendered at this point. See Validator() for that.
func Load(k Key, r *Registry) (s *Schema, err error) {
	s = New(k, r)
	fp := s.Path(FilePath)

	//nolint:gosec // Path is constructed from internal registry logic
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	s.srcDoc = data

	if !json.Valid(data) {
		return nil, &InvalidJSONError{Path: fp}
	}

	s.exists = true
	s.isPublic, err = isSchemaPublic(fp, data)
	if err != nil {
		return nil, err
	}

	if tErr := s.loadTemplate(fp); tErr != nil {
		return nil, tErr
	}

	return s, nil
}

// IsPublic returns true if the schema is intended to be published to the public.
func (s *Schema) IsPublic() bool {
	return s.isPublic
}

// loadTemplate initialises the template which will be used later to render the schema to target environments.
// This only needs to be done once per schema.
func (s *Schema) loadTemplate(fp string) error {
	tmpl := template.New(fp)

	// Define the FuncMap so the parser recognises the template names without the need for the . prefix
	// (e.g. {{ ID }} instead of {{ .ID }})
	tmpl.Funcs(template.FuncMap{
		"ID":  func() (string, error) { return "", nil },
		"JSM": func(string) (string, error) { return "", nil },
	})

	parsed, err := tmpl.Parse(string(s.srcDoc))
	if err != nil {
		return &TemplateFormatInvalidError{Path: fp, Wrapped: err}
	}
	s.tmpl = parsed
	return nil
}

// Render returns rendered artefacts for the given environment. If the schema hasn't yet been rendered,
// then it will be rendered now, and the result cached for later consumption.
func (s *Schema) Render(ec *config.EnvConfig) (RenderInfo, error) {
	s.mu.Lock()
	ri := s.computed.RenderInfo(ec.Env)
	s.mu.Unlock()

	if ri.Validator != nil {
		return ri, nil
	}

	return s.registry.CoordinateRender(s, ec)
}

// isSchemaPublic will return false unless the schema explicitly has property x-public set totrue.
func isSchemaPublic(filePath string, data []byte) (bool, error) {
	var meta struct {
		XPublic bool `json:"x-public"` //nolint:tagliatelle // Following JSON Schema extension standard
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return false, &CannotReadXPublicError{Path: filePath}
	}
	return meta.XPublic, nil
}

// CanonicalID returns the fully resolved canonical ID for this schema for the given environment.
// This is the ID that will be set as the $id property of the schema when rendered for the given environment,
// and the URL at which it will be found once deployed.
func (s *Schema) CanonicalID(ec *config.EnvConfig) ID {
	env := ec.Env
	s.mu.Lock()
	if id, ok := s.computed.ids[env]; ok {
		s.mu.Unlock()
		return id
	}
	s.mu.Unlock()

	baseURL := ec.URLRoot(s.isPublic)

	// combine the baseURL and the filename in filePath:
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	// s.Key() uses the lock so read before locking
	key := s.Key()

	s.mu.Lock()
	s.computed.StoreID(env, ID(baseURL+string(key)+SchemaSuffix))
	id := s.computed.ids[env]
	s.mu.Unlock()

	return id
}

// Key returns the key used to uniquely identify the schema in a Cache and
// form part of the filename on disk.
func (s *Schema) Key() Key {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.computed.key != "" {
		return s.computed.key
	}

	s.computed.key = s.core.Key()
	return s.computed.key
}

// Path returns a schema path based on the PathType. It will cache the result for future use.
func (s *Schema) Path(pt PathType) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cached *string

	switch pt {
	case FilePath:
		cached = &s.computed.filePath
	case FamilyDir:
		cached = &s.computed.familyDir
	case HomeDir:
		cached = &s.computed.homeDir
	}

	if *cached == "" {
		*cached = s.core.Path(pt, s.registry.rootDirectory)
	}
	return *cached
}

// Filename returns the filename only of the schema file.
func (s *Schema) Filename() string {
	return string(s.Key()) + SchemaSuffix
}

// WriteNewSchemaFiles creates a new schema file and its folders.
func (s *Schema) WriteNewSchemaFiles() error {
	hd := s.Path(HomeDir)

	// Create the missing parent folders
	if err := os.MkdirAll(hd, 0o750); err != nil {
		return err
	}

	cfg, err := s.registry.Config()
	if err != nil {
		return err
	}

	content := NewSchemaContent(cfg.DefaultJSONSchemaVersion)
	err = os.WriteFile(s.Path(FilePath), []byte(content), 0o600)
	if err != nil {
		return err
	}

	// Create the pass and fail folders
	err = os.Mkdir(filepath.Join(hd, string(TestDocTypePass)), 0o750)
	if err != nil {
		return err
	}
	err = os.Mkdir(filepath.Join(hd, string(TestDocTypeFail)), 0o750)
	if err != nil {
		return err
	}

	return nil
}

// DuplicateSchemaFiles duplicates the schema files of another schema into this schema,
// including the schema file, and the pass and fail test files.
// It renames the duplicated schema to match the properties of this schema.
func (s *Schema) DuplicateSchemaFiles(other *Schema) error {
	ohd := other.Path(HomeDir)
	hd := s.Path(HomeDir)

	// Create the missing parent folders
	if err := os.MkdirAll(hd, 0o750); err != nil {
		return err
	}

	// Copy the home directory of the other schema to the home directory of this schema:
	if err := os.CopyFS(hd, os.DirFS(ohd)); err != nil {
		return err
	}

	// Rename the copied schema file. It initially has the name of the other schema file so
	// we need to rename it to be as expected in this schema.
	copiedFile := path.Join(hd, other.Filename())
	if err := os.Rename(copiedFile, s.Path(FilePath)); err != nil {
		return err
	}

	return nil
}

// BumpVersion bumps the latest major, minor or patch version of a schem in the family. It takes into
// account the version of the schema s as a base, checks the existing versions in the family, and calculates what would
// be the next version depending on the release type.
// For example, given the following extant versions in the family:
// 1.0.0, 1.0.1, 1.0.2
// 1.1.0, 1.1.1, 1.1.2, 1.1.3, 1.1.4
// 1.2.0, 1.2.1, 1.2.2, 1.2.3
// 2.0.0, 2.0.1, 2.0.2
// then for a schema currently at version 1.2.3, the new version would be:
// bumpVersion(ReleaseTypeMajor) -> 3.0.0
// bumpVersion(ReleaseTypeMinor) -> 1.3.0
// bumpVersion(ReleaseTypePatch) -> 1.2.4
// and for a schema currently at version 1.0.0, the new version would be:
// bumpVersion(ReleaseTypeMajor) -> 3.0.0
// bumpVersion(ReleaseTypeMinor) -> 1.1.5
// bumpVersion(ReleaseTypePatch) -> 1.0.3.
func (s *Schema) BumpVersion(rt ReleaseType) {
	fd := s.Path(FamilyDir)

	c := s.core
	switch rt {
	case ReleaseTypeMajor:
		c.version.Set(s.nextVersion(fd), 0, 0)
	case ReleaseTypeMinor:
		dir := filepath.Join(fd, strconv.FormatUint(c.version.Major(), 10))
		c.version.Set(c.version.Major(), s.nextVersion(dir), 0)
	case ReleaseTypePatch:
		dir := filepath.Join(fd, strconv.FormatUint(c.version.Major(), 10), strconv.FormatUint(c.version.Minor(), 10))
		c.version.Set(c.version.Major(), c.version.Minor(), s.nextVersion(dir))
	}
	// Clear computed items as they're now invalid:
	s.ClearComputed()
}

// FindNextVersion looks for the next version component.
func (s *Schema) nextVersion(parentDir string) uint64 {
	versions, err := s.registry.pathResolver.GetUintSubdirectories(parentDir)
	if err != nil || len(versions) == 0 {
		return 0
	}

	return slices.Max(versions) + 1
}

// ClearComputed removes the computed content. Use if the schema settings have changed.
func (s *Schema) ClearComputed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.computed = Computed{}
}

// TestDocuments returns the absolute paths of the passing or failing test documents
// for the version of the schema family represented by s, in filename alphabetical order.
// A missing test directory has no documents: git does not keep empty directories, so a
// directory created empty by create-schema is missing from a fresh clone.
func (s *Schema) TestDocuments(tt TestDocType) ([]TestInfo, error) {
	homeDir := s.Path(HomeDir)

	// Return the tests if we've already cached them:
	s.mu.Lock()
	if tests := s.computed.Tests(tt); tests != nil {
		s.mu.Unlock()
		return tests, nil
	}
	s.mu.Unlock()

	// Get the full path of every .json file in the test directory defined by tt
	docDir := filepath.Join(homeDir, string(tt))

	// Identify files ending in .json
	entries, err := os.ReadDir(docDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	testDocs := make([]TestInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".json" {
			fp := filepath.Join(docDir, entry.Name())
			ti, tErr := NewTestInfo(fp)
			if tErr != nil {
				return nil, tErr
			}
			testDocs = append(testDocs, ti)
		}
	}

	// Cache the test documents for future use. Note that we don't cache the full paths.
	s.mu.Lock()
	s.computed.StoreTests(tt, testDocs)
	s.mu.Unlock()

	return testDocs, nil
}

// MajorFamilyFutureSchemas identifies schemas which:
// a) Belong to the same major version in the same family
// b) Have a version which is later than this schema's version.
//
// This set of keys will be used to identify which schemas we should collect the 'pass' tests from in order to
// validate that no future schema in the same family with the same major version breaks the contract represented by
// this schema.
//
// If any of those tests, when applied to this schema, actually fail, it means that the author of a supposedly
// non-breaking change in a later version has inadvertently contributed a breaking change.
//
// This is a key capability of the JSON Schema Manager. It is vital that information providers can be confident that
// changes they want to do to an information payload will not break consumers in production. The aim is to identify
// these accidental breakages AHEAD of the future schema being published, but JSM can also give a complete view of
// breakages in supposedly non-breaking lineages of versions.
func (s *Schema) MajorFamilyFutureSchemas() ([]Key, error) {
	var keys []Key

	fd := s.Path(FamilyDir)
	majorDir := filepath.Join(fd, strconv.FormatUint(s.core.version.Major(), 10))

	minorVers, err := s.registry.pathResolver.GetUintSubdirectories(majorDir)
	if err != nil {
		return nil, err
	}

	for _, mv := range minorVers {
		if mv < s.core.version.Minor() {
			continue // All of the schemas in this part of the family are older than this schema.
		}
		minorDir := filepath.Join(majorDir, strconv.FormatUint(mv, 10))
		keys, err = s.addMinorFamilyFutureSchemas(mv, minorDir, keys)
		if err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// addMinorFamilyFutureSchemas finds schemas which share the same major version as this schema
// and represent a future version.
func (s *Schema) addMinorFamilyFutureSchemas(minorVersion uint64, minorDir string, keys []Key) ([]Key, error) {
	patchDirs, err := s.registry.pathResolver.GetUintSubdirectories(minorDir)
	if err != nil {
		return nil, err
	}

	k := s.Key()
	kParts := strings.Split(string(k), KeySeparatorString)
	lp := len(kParts)
	kParts[lp-2] = strconv.FormatUint(minorVersion, 10)

	for _, pv := range patchDirs {
		// if it's the same minor version as s, we only want future patches:
		if (minorVersion == s.core.version.Minor()) && (pv <= s.core.version.Patch()) {
			continue
		}
		// Generate a key
		kParts[lp-1] = strconv.FormatUint(pv, 10)
		nk := strings.Join(kParts, KeySeparatorString)
		keys = append(keys, Key(nk))
	}
	return keys, nil
}

// NewestInFamily returns the key of the newest version in the schema's family, across every major
// version, which may be the schema itself.
func (s *Schema) NewestInFamily() (Key, error) {
	dir := s.Path(FamilyDir)
	var version [3]uint64
	for i := range version {
		versions, err := s.registry.pathResolver.GetUintSubdirectories(dir)
		if err != nil {
			return "", err
		}
		if len(versions) == 0 {
			return "", &NotFoundError{Path: dir}
		}
		version[i] = slices.Max(versions)
		dir = filepath.Join(dir, strconv.FormatUint(version[i], 10))
	}

	parts := strings.Split(string(s.Key()), KeySeparatorString)
	lp := len(parts)
	for i, v := range version {
		parts[lp-3+i] = strconv.FormatUint(v, 10)
	}
	return Key(strings.Join(parts, KeySeparatorString)), nil
}

// MajorFamilyEarlierSchemas identifies schemas which:
// a) Belong to the same major version in the same family
// b) Have a version which is earlier than this schema's version.
//
// Purpose:
// This set of keys is used to check provider compatibility. When developing a new schema version,
// we need to verify that documents passing the new schema's validation will also pass validation
// by consumers still using earlier versions. This prevents providers from breaking consumers who
// haven't yet upgraded.
//
// If this schema's pass tests fail validation by an earlier schema, it means the new schema
// would produce documents that break consumers on older versions.
func (s *Schema) MajorFamilyEarlierSchemas() ([]Key, error) {
	var keys []Key

	fd := s.Path(FamilyDir)
	majorDir := filepath.Join(fd, strconv.FormatUint(s.core.version.Major(), 10))

	minorVers, err := s.registry.pathResolver.GetUintSubdirectories(majorDir)
	if err != nil {
		return nil, err
	}

	for _, mv := range minorVers {
		if mv > s.core.version.Minor() {
			continue // All of the schemas in this part of the family are newer than this schema.
		}
		minorDir := filepath.Join(majorDir, strconv.FormatUint(mv, 10))
		keys, err = s.addMinorFamilyEarlierSchemas(mv, minorDir, keys)
		if err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// addMinorFamilyEarlierSchemas finds schemas which share the same major version as this schema
// and represent an earlier version.
func (s *Schema) addMinorFamilyEarlierSchemas(minorVersion uint64, minorDir string, keys []Key) ([]Key, error) {
	patchDirs, err := s.registry.pathResolver.GetUintSubdirectories(minorDir)
	if err != nil {
		return nil, err
	}

	k := s.Key()
	kParts := strings.Split(string(k), KeySeparatorString)
	lp := len(kParts)
	kParts[lp-2] = strconv.FormatUint(minorVersion, 10)

	for _, pv := range patchDirs {
		// if it's the same minor version as s, we only want earlier patches:
		if (minorVersion == s.core.version.Minor()) && (pv >= s.core.version.Patch()) {
			continue
		}
		// Generate a key
		kParts[lp-1] = strconv.FormatUint(pv, 10)
		nk := strings.Join(kParts, KeySeparatorString)
		keys = append(keys, Key(nk))
	}
	return keys, nil
}
