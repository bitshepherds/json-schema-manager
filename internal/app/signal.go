package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalError is the cause of a context which NotifyContext cancelled on a signal.
type SignalError struct {
	Signal os.Signal
}

func (e *SignalError) Error() string {
	return "stopped by signal: " + e.Signal.String()
}

// NotifyContext returns a context which is cancelled, with a *SignalError as its cause, when the
// process receives SIGINT or SIGTERM. Call stop to release it.
func NotifyContext(parent context.Context) (ctx context.Context, stop func()) {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := cancelOnSignal(parent, sigC)
	return ctx, func() {
		signal.Stop(sigC)
		cancel()
	}
}

// cancelOnSignal returns a context which is cancelled, with a *SignalError as its cause, when a
// signal arrives on sigC.
func cancelOnSignal(parent context.Context, sigC <-chan os.Signal) (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(parent)
	go func() {
		select {
		case s := <-sigC:
			cancel(&SignalError{Signal: s})
		case <-ctx.Done():
		}
	}()
	return ctx, func() { cancel(nil) }
}
