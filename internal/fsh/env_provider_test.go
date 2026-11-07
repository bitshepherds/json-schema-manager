package fsh_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
)

// mockEnvProvider is a test implementation of EnvProvider.
type mockEnvProvider struct {
	values map[string]string
}

func (m *mockEnvProvider) Get(key string) string {
	if m.values == nil {
		return ""
	}
	return m.values[key]
}

func TestOSEnvProvider(t *testing.T) {
	t.Parallel()

	t.Run("Get returns environment variable", func(t *testing.T) {
		t.Parallel()
		provider := fsh.NewEnvProvider()

		// PATH should always be set
		path := provider.Get("PATH")
		assert.NotEmpty(t, path)
	})

	t.Run("Get returns empty for unset variable", func(t *testing.T) {
		t.Parallel()
		provider := fsh.NewEnvProvider()

		value := provider.Get("UNLIKELY_TO_BE_SET_12345")
		assert.Empty(t, value)
	})
}

func TestMockEnvProvider(t *testing.T) {
	t.Parallel()

	t.Run("Get returns configured value", func(t *testing.T) {
		t.Parallel()
		mock := &mockEnvProvider{
			values: map[string]string{
				"TEST_KEY": "test_value",
			},
		}

		assert.Equal(t, "test_value", mock.Get("TEST_KEY"))
		assert.Empty(t, mock.Get("MISSING_KEY"))
	})

	t.Run("Get returns empty for nil map", func(t *testing.T) {
		t.Parallel()
		mock := &mockEnvProvider{}

		assert.Empty(t, mock.Get("ANY_KEY"))
	})
}
