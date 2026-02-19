package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func ContextWithShutdownSignal(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer signal.Stop(signals)
		select {
		case <-ctx.Done():
			return
		case <-signals:
			cancel()
		}
	}()

	return ctx, cancel
}
