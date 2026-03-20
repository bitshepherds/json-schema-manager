package schema

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SearchSeparator is the separator used to separate the domains, family name, and semantic version in a Searcher spec.
const SearchSeparator byte = '/'

// SearchSeparatorString is the string representation of SearchSeparator.
var SearchSeparatorString = string(SearchSeparator)

var validSearchScopeRegex = regexp.MustCompile(`^[a-z0-9-/]+$`)

// SearchScope is a string specification for a search scope, which can take one of the following forms:
// - "domain-a"
// - "domain-a/subdomain-a"
// - "domain-a/subdomain-a/family-name"
// - "domain-a/subdomain-a/family-name/1"
// - "domain-a/subdomain-a/family-name/1/0"
// - "domain-a/subdomain-a/family-name/1/0/0"
//
// (Note that there can be 1-n domains. The example above shows a two-level domain.)
type SearchScope string

// NewSearchScope creates a new SearchScope from a string specification.
func NewSearchScope(s string) (SearchScope, error) {
	cleanS := strings.TrimRight(s, SearchSeparatorString)
	if !validSearchScopeRegex.MatchString(cleanS) {
		return "", &InvalidSearchScopeError{spec: s}
	}
	return SearchScope(cleanS), nil
}

// Searcher is used to define the scope of a search for schemas in a registry, and then
// to execute a search for schemas that match the scope.
type Searcher struct {
	registry   *Registry
	searchRoot string
}

// NewSearcher creates a new searcher from a string specification.
func NewSearcher(r *Registry, s SearchScope) (*Searcher, error) {
	searchRoot, err := searchSpecToPath(r.rootDirectory, s)
	if err != nil {
		return nil, err
	}

	if _, sErr := os.Stat(searchRoot); sErr != nil {
		return nil, fmt.Errorf("search root does not exist: %w", sErr)
	}

	return &Searcher{
		registry:   r,
		searchRoot: searchRoot,
	}, nil
}

// searchSpecToPath checks the SearchScope and adjusts the root directory to match the scope.
func searchSpecToPath(rootDir string, s SearchScope) (string, error) {
	if len(s) == 0 {
		return rootDir, nil
	}
	ss := string(s)
	if !validSearchScopeRegex.MatchString(ss) {
		return "", &InvalidSearchScopeError{spec: ss}
	}

	parts := strings.Split(ss, SearchSeparatorString)

	return filepath.Join(append([]string{rootDir}, parts...)...), nil
}

// SearchResult is a result from a search for schemas.
type SearchResult struct {
	Key Key
	Err error
}

// Schemas walks the searchRoot and streams SearchResult over a channel as they're found.
// This allows consumers to process schemas in parallel with the filesystem traversal.
func (s *Searcher) Schemas(ctx context.Context) <-chan SearchResult {
	resC := make(chan SearchResult, 1)

	if s == nil {
		go func() {
			defer close(resC)
			resC <- SearchResult{Err: errors.New("searcher is nil")}
		}()
		return resC
	}

	go func() {
		defer close(resC)

		err := filepath.Walk(s.searchRoot, s.walkFunc(ctx, resC))
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case resC <- SearchResult{Err: err}:
			}
		}
	}()

	return resC
}

func (s *Searcher) walkFunc(ctx context.Context, resC chan<- SearchResult) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		filename := info.Name()
		if !strings.HasSuffix(filename, SchemaSuffix) {
			return nil
		}

		stem := strings.TrimSuffix(filename, SchemaSuffix)
		core, kErr := NewCoreFromString(stem, KeySeparator)
		if kErr != nil {
			return &InvalidSchemaFilenameError{Path: path}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case resC <- SearchResult{core.Key(), nil}:
		}

		return nil
	}
}
