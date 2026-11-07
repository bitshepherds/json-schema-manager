package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
)

func TestNewCheckChangesCmd(t *testing.T) {
	t.Parallel()
	mockMgr := &MockManager{}
	cmd := NewCheckChangesCmd(mockMgr)
	assert.NotNil(t, cmd)

	mockMgr.On("CheckChanges", mock.Anything, config.Env("prod")).Return(nil)

	cmd.SetArgs([]string{"prod"})
	err := cmd.ExecuteContext(context.Background())
	require.NoError(t, err)

	mockMgr.AssertExpectations(t)
}
