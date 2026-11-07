package repoconfig

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	devToolsGroup = "dev tools"
	actionsGroup  = "GitHub Actions"
	shippedGroup  = "shipped Go modules"
)

// kind is one kind of dependency the repo has, with the group its routine updates go in, and whether they may
// merge themselves.
type kind struct {
	name       string
	manager    string
	datasource string
	pkg        string
	group      string
	lowRisk    bool
}

var kinds = []kind{
	{"shipped Go module", "gomod", "go", "github.com/spf13/pflag", shippedGroup, false},
	{"test-only Go module testify", "gomod", "go", "github.com/stretchr/testify", shippedGroup, false},
	{"test-only Go module go-internal", "gomod", "go", "github.com/rogpeppe/go-internal", shippedGroup, false},
	{"go directive", "gomod", "golang-version", "go", "Go version", false},
	{"mise tool", "mise", "github-releases", "aqua:goreleaser/goreleaser", devToolsGroup, true},
	{"mise node", "mise", "node-version", "node", devToolsGroup, true},
	// Renovate names the custom manager's updates "regex", and matches them with "custom.regex".
	{"npm tool pin", "custom.regex", "npm", "@redocly/cli", devToolsGroup, true},
	{"GitHub Action", "github-actions", "github-tags", "actions/checkout", actionsGroup, true},
}

var (
	lowRiskUpdateTypes = []string{"patch", "minor", "pin", "digest", "pinDigest"}
	allUpdateTypes     = append(slices.Clone(lowRiskUpdateTypes), "major")
)

func (k kind) update(updateType string, security bool) dependency {
	return dependency{
		name:       k.name,
		manager:    k.manager,
		datasource: k.datasource,
		pkg:        k.pkg,
		updateType: updateType,
		security:   security,
	}
}

func TestRenovateNeverAutomergesShippedGoModules(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	shipped := kind{"shipped Go module", "gomod", "go", "golang.org/x/sync", shippedGroup, false}
	for _, updateType := range allUpdateTypes {
		for _, security := range []bool{false, true} {
			eff := cfg.resolve(shipped.update(updateType, security))
			assert.False(t, eff.automerge(), "%s update (security %v) must wait for a person", updateType, security)
		}
	}
	for _, updateType := range lowRiskUpdateTypes {
		assert.Equal(t, shippedGroup, cfg.resolve(shipped.update(updateType, false)).group())
	}
}

func TestRenovateAutomergesOnlyLowRiskDevToolAndActionUpdates(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	for _, k := range kinds {
		for _, updateType := range allUpdateTypes {
			eff := cfg.resolve(k.update(updateType, false))
			want := k.lowRisk && updateType != "major"
			assert.Equal(t, want, eff.automerge(), "%s %s update", k.name, updateType)
			if updateType != "major" {
				assert.Equal(t, k.group, eff.group(), "%s %s update", k.name, updateType)
			}
		}
	}
}

func TestRenovateHoldsEveryMajorForDashboardApproval(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	assert.Equal(t, true, cfg.top["separateMajorMinor"], "majors must be split from minors explicitly")
	for _, k := range kinds {
		major := cfg.resolve(k.update("major", false))
		assert.True(t, major.dashboardApproval(), "%s major must wait for approval on the dashboard", k.name)
		assert.False(t, major.automerge(), "%s major must wait for a person", k.name)
		assert.Empty(t, major.group(), "%s major must get a PR of its own", k.name)

		for _, updateType := range lowRiskUpdateTypes {
			assert.False(t, cfg.resolve(k.update(updateType, false)).dashboardApproval(),
				"%s %s update needs no approval to open", k.name, updateType)
		}
	}
}

// Go keeps one version list for the whole module, and gomodTidy runs on every update, so an update to a module
// only the tests import can raise a shipped one, such as gopkg.in/yaml.v3 or golang.org/x/sys. A person merges it.
func TestRenovateNeverAutomergesTestOnlyGoModules(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	for _, pkg := range []string{"github.com/stretchr/testify", "github.com/rogpeppe/go-internal"} {
		testOnly := kind{pkg, "gomod", "go", pkg, shippedGroup, false}
		for _, updateType := range allUpdateTypes {
			for _, security := range []bool{false, true} {
				eff := cfg.resolve(testOnly.update(updateType, security))
				assert.False(t, eff.automerge(), "%s %s update (security %v)", pkg, updateType, security)
				if updateType != "major" && !security {
					assert.Equal(t, shippedGroup, eff.group(), "%s %s update", pkg, updateType)
				}
			}
		}
	}
}

func TestRenovateRunsWeeklyWithAThreeDayReleaseAge(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	for _, k := range kinds {
		for _, updateType := range allUpdateTypes {
			eff := cfg.resolve(k.update(updateType, false))
			assert.Equal(t, []string{"* 0-5 * * 1"}, eff.strings("schedule"), "%s %s update", k.name, updateType)
			assert.Equal(t, "3 days", eff["minimumReleaseAge"], "%s %s update", k.name, updateType)

			// A digest has no release timestamp, so a required one would hold it forever.
			wantBehaviour := "timestamp-required"
			if updateType == "digest" || updateType == "pinDigest" {
				wantBehaviour = "timestamp-optional"
			}
			got, set := eff["minimumReleaseAgeBehaviour"]
			if !set {
				got = "timestamp-required" // Renovate's default.
			}
			assert.Equal(t, wantBehaviour, got, "%s %s update", k.name, updateType)
		}
	}
}

func TestRenovateLabelsSecurityUpdates(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	assert.Equal(t, true, cfg.top["osvVulnerabilityAlerts"], "osv.dev is a second source of security alerts")
	for _, k := range kinds {
		for _, updateType := range allUpdateTypes {
			fix := cfg.resolve(k.update(updateType, true))
			assert.Contains(t, fix.strings("labels"), "security", "%s %s security update", k.name, updateType)
			// Renovate creates security PRs before any other, whatever their prPriority, so none is set here.
			assert.Empty(t, fix.strings("schedule"), "%s %s security update skips the schedule", k.name, updateType)
			assert.Nil(t, fix["minimumReleaseAge"], "%s %s security update skips the release age", k.name, updateType)
			assert.Equal(t, k.lowRisk && updateType != "major", fix.automerge(),
				"%s %s security update merges as its dependency's updates do", k.name, updateType)

			routine := cfg.resolve(k.update(updateType, false))
			assert.NotContains(t, routine.strings("labels"), "security", "%s %s routine update", k.name, updateType)
		}
	}
}

func TestRenovateOpensSecurityMajorsAtOnceForAPerson(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	for _, k := range kinds {
		fix := cfg.resolve(k.update("major", true))
		assert.False(t, fix.dashboardApproval(), "%s security major must open at once", k.name)
		assert.False(t, fix.automerge(), "%s security major must wait for a person", k.name)
		assert.Empty(t, fix.strings("schedule"), "%s security major skips the schedule", k.name)
	}
}

func TestRenovateKeepsGoDirectiveUpdatesInTheirOwnPR(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	// The go and toolchain directives both come from the golang-version datasource.
	for _, depName := range []string{"go", "toolchain"} {
		directive := kind{depName, "gomod", "golang-version", "go", "", false}
		for _, updateType := range lowRiskUpdateTypes {
			eff := cfg.resolve(directive.update(updateType, false))
			require.NotEmpty(t, eff.group(), "%s %s update", depName, updateType)
			assert.NotEqual(t, shippedGroup, eff.group(), "%s %s update", depName, updateType)
			assert.NotEqual(t, devToolsGroup, eff.group(), "%s %s update", depName, updateType)
			assert.False(t, eff.automerge(), "%s %s update", depName, updateType)
		}
	}
}

func TestRenovateMergesThroughTheAPINotNativeAutomerge(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	automerged := 0
	for _, k := range kinds {
		for _, updateType := range allUpdateTypes {
			for _, security := range []bool{false, true} {
				eff := cfg.resolve(k.update(updateType, security))
				if eff.automerge() {
					automerged++
					// GitHub's native auto-merge ignores the ruleset's bypass actors, so it would never merge.
					assert.Equal(t, false, eff["platformAutomerge"],
						"%s %s update must merge through the API", k.name, updateType)
				}
			}
		}
	}
	require.Positive(t, automerged, "some updates must merge themselves")
}

func TestRenovateKeepsBestPracticesTheDashboardAndGoModTidy(t *testing.T) {
	t.Parallel()
	cfg := loadRenovate(t)
	assert.Equal(t, []any{"config:best-practices"}, cfg.top["extends"])
	assert.Equal(t, true, cfg.top["dependencyDashboard"])
	assert.Equal(t, []any{"gomodTidy"}, cfg.top["postUpdateOptions"])

	managers, ok := cfg.top["customManagers"].([]any)
	require.True(t, ok, "the npm tool pins need their regex manager")
	require.Len(t, managers, 1)
	npm, ok := managers[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "regex", npm["customType"])
	assert.Equal(t, "npm", npm["datasourceTemplate"])
	assert.Equal(t, []any{`/^scripts/(models|apis)/main\.go$/`}, npm["managerFilePatterns"])
}
