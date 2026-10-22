package app

import (
	"context"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelOnSignal(t *testing.T) {
	t.Parallel()

	sigC := make(chan os.Signal, 1)
	ctx, stop := cancelOnSignal(context.Background(), sigC)
	defer stop()

	sigC <- syscall.SIGTERM
	<-ctx.Done()
	var se *SignalError
	require.ErrorAs(t, context.Cause(ctx), &se)
	assert.Equal(t, syscall.SIGTERM, se.Signal)
	assert.Equal(t, "stopped by signal: terminated", se.Error())
}

func TestStoppedError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	assert.Equal(t, context.Canceled, stoppedError(ctx, context.Canceled), "ctx is not cancelled")
	cancel(&SignalError{Signal: syscall.SIGTERM})
	assert.Equal(t, &SignalError{Signal: syscall.SIGTERM}, stoppedError(ctx, context.Canceled))
}

func TestNotifyContext(t *testing.T) {
	t.Parallel()

	ctx, stop := NotifyContext(context.Background())
	stop()
	<-ctx.Done()
	assert.ErrorIs(t, context.Cause(ctx), context.Canceled, "stopping cancels without a signal")
}
