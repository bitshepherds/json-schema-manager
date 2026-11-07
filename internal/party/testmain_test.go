package party

import (
	"os"
	"testing"

	"github.com/bitshepherds/json-schema-manager/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.Hide()
	os.Exit(m.Run())
}
