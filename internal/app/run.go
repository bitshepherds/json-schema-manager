package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"syscall"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// Exit codes returned by the jsm binary.
const (
	ExitOK          = 0   // the command succeeded and every check passed
	ExitCheckFailed = 1   // JSM ran, and a test or check found a problem
	ExitCannotRun   = 2   // JSM could not run: bad usage, configuration, schema or environment
	ExitInterrupted = 130 // stopped by SIGINT (128 + 2), or otherwise cancelled
	ExitTerminated  = 143 // stopped by SIGTERM (128 + 15)
)

// ExitCode maps an error returned by Run to the process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var se *SignalError
	if errors.As(err, &se) && se.Signal == syscall.SIGTERM {
		return ExitTerminated
	}
	if se != nil || errors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	if errors.Is(err, ErrTestsFailed) || errors.As(err, new(*schema.ChangedDeployedSchemasError)) {
		return ExitCheckFailed
	}
	return ExitCannotRun
}

// stoppedError returns why a run stopped part way: the cause of ctx's cancellation, such as a
// *SignalError, or err if ctx was not cancelled.
func stoppedError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

// Run executes the application with the given arguments.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, envProvider fsh.EnvProvider) error {
	logLevel := &slog.LevelVar{}
	logLevel.Set(slog.LevelInfo)

	// Local lazy instance ensures t.Parallel() safety
	lazy := &LazyManager{}

	if envProvider == nil {
		envProvider = fsh.NewEnvProvider()
	}

	rootCmd := NewRootCmd(lazy, logLevel, stdout, stderr, envProvider)
	rootCmd.SetArgs(args[1:]) // Skip the program name
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			// A non-zero exit, so that a CI job stopped part way does not pass
			cause := stoppedError(ctx, err)
			var se *SignalError
			if errors.As(cause, &se) && se.Signal == syscall.SIGTERM {
				_, _ = fmt.Fprintln(stderr, "Terminated")
			} else {
				_, _ = fmt.Fprintln(stderr, "Interrupted by user")
			}
			return cause
		}
		if errors.Is(err, ErrTestsFailed) {
			return err
		}
		// Print error to stderr for script tests and CLI users (SilenceErrors is set)
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return err
	}

	return nil
}
