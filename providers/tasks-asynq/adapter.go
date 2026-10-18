package asynqprovider

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// wrapHandler converts a generic tasks.HandlerFunc into an asynq.HandlerFunc
func wrapHandler(h tasks.HandlerFunc) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		return h(ctx, t)
	}
}

func policyToOptions(p tasks.EnqueuePolicy) []asynq.Option {
	opts := make([]asynq.Option, 0, 5)
	if p.Queue != "" {
		opts = append(opts, asynq.Queue(p.Queue))
	}
	if p.MaxRetry >= 0 {
		opts = append(opts, asynq.MaxRetry(p.MaxRetry))
	}
	if p.Timeout > 0 {
		opts = append(opts, asynq.Timeout(p.Timeout))
	}
	if p.ProcessIn > 0 {
		opts = append(opts, asynq.ProcessIn(p.ProcessIn))
	}
	if p.Retention > 0 {
		opts = append(opts, asynq.Retention(p.Retention))
	}
	// BackoffBase and BackoffMax have no asynq option: the retry delay is a
	// SERVER-WIDE function there (asynq.Config.RetryDelayFunc), not something
	// a single task carries. They are deliberately not translated rather than
	// approximated, and the provider's documentation says so — a curve that
	// silently applied to every other job in the process would be worse than
	// one that is honestly ignored.
	return opts
}
