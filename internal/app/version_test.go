package app

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionFrom(t *testing.T) {
	t.Parallel()

	withVersion := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/bitshepherds/json-schema-manager", Version: v}}
	}

	tests := []struct {
		name string
		set  string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"set at build time", "1.4.1", withVersion("v1.4.0"), true, "1.4.1"},
		{"go install of a release", "dev", withVersion("v1.4.1"), true, "v1.4.1"},
		{
			"go install of a commit", "dev", withVersion("v1.4.1-0.20261023120000-abcdef123456"), true,
			"v1.4.1-0.20261023120000-abcdef123456",
		},
		{"built from its own source tree", "dev", withVersion("(devel)"), true, "dev"},
		{"no module version", "dev", withVersion(""), true, "dev"},
		{"no build info", "dev", nil, false, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, versionFrom(tt.set, tt.info, tt.ok))
		})
	}
}
