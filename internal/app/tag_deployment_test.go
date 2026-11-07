package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
)

func TestNewTagDeploymentCmd(t *testing.T) {
	t.Parallel()
	m := &MockManager{}
	cmd := NewTagDeploymentCmd(m)

	assert.Equal(t, "tag-deployment [environment]", cmd.Use)

	t.Run("execute tag-deployment", func(t *testing.T) {
		t.Parallel()
		m.On("TagDeployment", mock.Anything, config.Env("prod")).Return(nil)

		cmd.SetArgs([]string{"prod"})
		cmd.SetOut(new(bytes.Buffer))
		err := cmd.Execute()

		require.NoError(t, err)
		m.AssertExpectations(t)
	})
}
