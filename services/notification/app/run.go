package app

import (
	"context"
	"errors"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
	"sync"
)

// runServers runs the gRPC server, the REST server, the email consumer, and the
// saga participant consumer concurrently. The first one to exit (error or clean
// stop) cancels the shared context so the others shut down too, giving the
// process a single coordinated lifecycle.
func runServers(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	const workers = 4
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	go func() {
		defer wg.Done()
		defer cancel()
		errs[0] = runGRPCServer(ctx, cfg, deps, log)
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		errs[1] = runConsumer(ctx, cfg, deps, log)
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		errs[2] = runSagaConsumer(ctx, cfg, deps, log)
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		errs[3] = runRESTServer(ctx, cfg, deps, log)
	}()

	wg.Wait()
	return errors.Join(errs...)
}
