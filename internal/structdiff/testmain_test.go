package structdiff

import (
	"os"
	"testing"

	"github.com/bitshepherds/json-schema-manager/v2/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.Hide()
	os.Exit(m.Run())
}
