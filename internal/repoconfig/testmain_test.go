// Package repoconfig tests the repo's own configuration files, such as renovate.json. It holds no code that ships.
package repoconfig

import (
	"os"
	"testing"

	"github.com/bitshepherds/json-schema-manager/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.Hide()
	os.Exit(m.Run())
}
