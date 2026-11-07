package repoconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// renovatePath is the repo's Renovate config, relative to this package's directory, where go test runs.
const renovatePath = "../../renovate.json"

// dependency is one sample update, described by the values Renovate's matchers compare.
type dependency struct {
	name       string
	manager    string
	datasource string
	pkg        string
	updateType string
	security   bool
}

// renovateConfig is the subset of a Renovate config that decides how each update is raised and merged.
type renovateConfig struct {
	top                 map[string]any
	packageRules        []map[string]any
	vulnerabilityAlerts map[string]any
}

// effective is the config Renovate applies to one update, once every rule that matches it is merged in.
type effective map[string]any

// knownMatchers are the packageRules matchers the evaluator understands. Each compares one value of the update
// with a list of exact names.
var knownMatchers = map[string]func(dependency) string{
	"matchManagers":     func(d dependency) string { return d.manager },
	"matchDatasources":  func(d dependency) string { return d.datasource },
	"matchPackageNames": func(d dependency) string { return d.pkg },
	"matchUpdateTypes":  func(d dependency) string { return d.updateType },
}

// structuralKeys are top-level keys which hold structure, not settings an update inherits.
var structuralKeys = []string{
	"$schema", "extends", "customManagers", "packageRules", "vulnerabilityAlerts", "description",
}

var errUnknownMatcher = errors.New("unknown matcher")

func loadRenovate(t *testing.T) renovateConfig {
	t.Helper()
	raw, err := os.ReadFile(renovatePath)
	require.NoError(t, err)
	cfg, err := parseRenovate(raw)
	require.NoError(t, err)
	return cfg
}

// parseRenovate reads a Renovate config, and refuses any packageRules matcher the evaluator can't resolve, so a
// new kind of rule can't slip past the tests.
func parseRenovate(raw []byte) (renovateConfig, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return renovateConfig{}, err
	}
	cfg := renovateConfig{top: top}

	if rules, ok := top["packageRules"].([]any); ok {
		for i, r := range rules {
			rule, ok := r.(map[string]any)
			if !ok {
				return renovateConfig{}, fmt.Errorf("packageRules[%d] is not an object", i)
			}
			if err := checkMatchers(rule); err != nil {
				return renovateConfig{}, fmt.Errorf("packageRules[%d]: %w", i, err)
			}
			cfg.packageRules = append(cfg.packageRules, rule)
		}
	}
	if va, ok := top["vulnerabilityAlerts"].(map[string]any); ok {
		cfg.vulnerabilityAlerts = va
	}
	return cfg, nil
}

func checkMatchers(rule map[string]any) error {
	for key, value := range rule {
		if !isMatcher(key) {
			continue
		}
		if _, known := knownMatchers[key]; !known {
			return fmt.Errorf("%w %q", errUnknownMatcher, key)
		}
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%w %q: not a list", errUnknownMatcher, key)
		}
		for _, v := range list {
			// Renovate reads a glob, a /regex/ or a !negation as a pattern, which the evaluator doesn't resolve.
			s, ok := v.(string)
			if !ok || strings.ContainsAny(s, "*?!{}") || strings.HasPrefix(s, "/") {
				return fmt.Errorf("%w %q: %v is a pattern, not an exact name", errUnknownMatcher, key, v)
			}
		}
	}
	return nil
}

// isMatcher says whether a packageRules key selects updates, rather than setting what happens to them.
func isMatcher(key string) bool {
	return strings.HasPrefix(key, "match") || strings.HasPrefix(key, "exclude") || key == "isVulnerabilityAlert"
}

func matches(rule map[string]any, dep dependency) bool {
	for key, value := range rule {
		if !isMatcher(key) {
			continue
		}
		var names []string
		for _, v := range value.([]any) { //nolint:forcetypeassert // checkMatchers proved every matcher is a list.
			names = append(names, v.(string)) //nolint:forcetypeassert // checkMatchers proved each is a string.
		}
		if !slices.Contains(names, knownMatchers[key](dep)) {
			return false
		}
	}
	return true
}

// resolve works out the config Renovate applies to one update: the top-level settings, then each matching
// packageRule in order, a later one overriding an earlier one, then, for a security fix, vulnerabilityAlerts,
// which Renovate forces over every rule.
func (c renovateConfig) resolve(dep dependency) effective {
	eff := effective{}
	for key, value := range c.top {
		if !slices.Contains(structuralKeys, key) {
			eff[key] = value
		}
	}
	for _, rule := range c.packageRules {
		if !matches(rule, dep) {
			continue
		}
		for key, value := range rule {
			if !isMatcher(key) && key != "description" {
				eff[key] = value
			}
		}
	}
	if dep.security {
		for key, value := range c.vulnerabilityAlerts {
			eff[key] = value
		}
	}
	return eff
}

// automerge is false unless set, as it is in Renovate.
func (e effective) automerge() bool {
	v, _ := e["automerge"].(bool)
	return v
}

// group is the update's group, or "" when it gets a PR of its own.
func (e effective) group() string {
	v, _ := e["groupName"].(string)
	return v
}

func (e effective) dashboardApproval() bool {
	v, _ := e["dependencyDashboardApproval"].(bool)
	return v
}

func (e effective) strings(key string) []string {
	list, _ := e[key].([]any)
	out := []string{}
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestRenovateRuleEvaluatorRejectsUnknownMatchers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config string
	}{
		{"a matcher it doesn't know", `{"packageRules": [{"matchDepTypes": ["require"], "automerge": true}]}`},
		{"an exclude matcher", `{"packageRules": [{"excludePackageNames": ["x"], "automerge": true}]}`},
		{"a glob package name", `{"packageRules": [{"matchPackageNames": ["github.com/*"], "automerge": true}]}`},
		{"a regex package name", `{"packageRules": [{"matchPackageNames": ["/^github/"], "automerge": true}]}`},
		{"a negated manager", `{"packageRules": [{"matchManagers": ["!gomod"], "automerge": true}]}`},
		{"a matcher that isn't a list", `{"packageRules": [{"matchManagers": "gomod", "automerge": true}]}`},
		{"a vulnerability matcher", `{"packageRules": [{"isVulnerabilityAlert": true, "automerge": true}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRenovate([]byte(tt.config))
			require.ErrorIs(t, err, errUnknownMatcher)
		})
	}
}

func TestRenovateRuleEvaluatorAppliesRulesInOrder(t *testing.T) {
	t.Parallel()
	cfg, err := parseRenovate([]byte(`{
		"automerge": false,
		"packageRules": [
			{"matchManagers": ["gomod"], "groupName": "first", "automerge": true},
			{"matchManagers": ["gomod"], "matchUpdateTypes": ["major"], "groupName": null, "automerge": false},
			{"matchManagers": ["mise"], "groupName": "other"}
		],
		"vulnerabilityAlerts": {"labels": ["security"]}
	}`))
	require.NoError(t, err)

	minor := cfg.resolve(dependency{manager: "gomod", pkg: "m", updateType: "minor"})
	require.True(t, minor.automerge())
	require.Equal(t, "first", minor.group())

	major := cfg.resolve(dependency{manager: "gomod", pkg: "m", updateType: "major"})
	require.False(t, major.automerge(), "a later rule overrides an earlier one")
	require.Empty(t, major.group(), "a null groupName takes the update out of its group")

	require.Empty(t, minor.strings("labels"))
	fix := cfg.resolve(dependency{manager: "gomod", pkg: "m", updateType: "minor", security: true})
	require.Equal(t, []string{"security"}, fix.strings("labels"))
}

func TestRenovateRuleEvaluatorRejectsAMalformedConfig(t *testing.T) {
	t.Parallel()
	_, err := parseRenovate([]byte(`{"packageRules": ["not a rule"]}`))
	require.Error(t, err)
	_, err = parseRenovate([]byte(`not json`))
	require.Error(t, err)
}
