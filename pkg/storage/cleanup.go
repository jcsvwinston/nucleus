package storage

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Cleaner runs periodic cleanup of temporary objects.
type Cleaner struct {
	store    Store
	prefix   string
	maxAge   time.Duration
	interval time.Duration
	enabled  bool
	stopCh   chan struct{}
	logger   *slog.Logger
}

// NewCleaner creates a background cleanup goroutine.
func NewCleaner(store Store, cfg CleanupConfig, logger *slog.Logger) (*Cleaner, error) {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "_tmp/"
	}

	maxAge, err := time.ParseDuration(cfg.MaxAge)
	if err != nil {
		maxAge = 24 * time.Hour
	}

	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		interval = time.Hour
	}

	// cfg.Enabled is carried on the Cleaner rather than acted on here: the
	// two branches this replaced returned byte-identical Cleaners, so the
	// switch decided nothing and Start() — which only ever looked at the
	// interval — swept regardless. Building a disabled Cleaner is still
	// correct (app.New wants a non-nil handle to Stop on shutdown); what
	// must not happen is that it runs.
	return &Cleaner{
		store:    store,
		prefix:   prefix,
		enabled:  cfg.Enabled,
		maxAge:   maxAge,
		interval: interval,
		stopCh:   make(chan struct{}),
		logger:   logger,
	}, nil
}

// Start begins the background cleanup loop.
// Call Stop() to terminate gracefully.
// Start begins the periodic sweep. It is a no-op when cleanup is disabled
// in config, which is the whole point of the switch: run() sweeps once
// immediately, before the first tick, so a cleaner that starts at all has
// already deleted by the time anyone could stop it.
func (c *Cleaner) Start() {
	if !c.enabled {
		if c.logger != nil {
			c.logger.Debug("storage cleaner disabled by config; not starting", "prefix", c.prefix)
		}
		return
	}
	if c.interval <= 0 {
		return
	}

	go c.run()
}

func (c *Cleaner) run() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run immediately on start
	c.runCleanup()

	for {
		select {
		case <-ticker.C:
			c.runCleanup()
		case <-c.stopCh:
			if c.logger != nil {
				c.logger.Info("storage cleaner stopped")
			}
			return
		}
	}
}

func (c *Cleaner) runCleanup() {
	if c.logger != nil {
		c.logger.Info("storage cleanup started", "prefix", c.prefix, "max_age", c.maxAge)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	deleted := 0
	errors := 0

	// Paginate through all objects with the temp prefix
	marker := ""
	for {
		result, err := c.store.List(ctx, ListOptions{
			Prefix: c.prefix,
			Limit:  100,
			Marker: marker,
		})
		if err != nil {
			if c.logger != nil {
				c.logger.Error("storage cleanup list error", "error", err)
			}
			errors++
			break
		}

		cutoff := time.Now().Add(-c.maxAge)

		for _, obj := range result.Objects {
			if obj.UpdatedAt.Before(cutoff) {
				if err := c.store.Delete(ctx, obj.Key); err != nil {
					if c.logger != nil {
						c.logger.Error("storage cleanup delete error",
							"key", obj.Key, "error", err)
					}
					errors++
				} else {
					deleted++
					if c.logger != nil {
						c.logger.Debug("storage cleanup deleted", "key", obj.Key, "age", time.Since(obj.UpdatedAt))
					}
				}
			}
		}

		if !result.Truncated {
			break
		}
		marker = result.NextMarker
	}

	if c.logger != nil {
		c.logger.Info("storage cleanup completed",
			"deleted", deleted,
			"errors", errors,
			"duration", time.Since(time.Now()).Abs())
	}
}

// Stop signals the cleaner to terminate.
func (c *Cleaner) Stop() {
	close(c.stopCh)
}

// CleanupTempKey generates a temporary key with the configured prefix.
// Use this for import uploads and other temporary storage that should
// be cleaned up automatically.
func CleanupTempKey(purpose string) string {
	ts := time.Now().UTC().Format("20060102150405")
	return "_tmp/" + purpose + "_" + ts
}

// IsTempKey checks if a key is a temporary key.
func IsTempKey(key string) bool {
	return strings.HasPrefix(normalizeKey(key), "_tmp/")
}
