package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// NewExtension adapts the agent into an app.Extension so callers can wire
// it through pkg/app.WithExtensions. The extension is fail-open: when
// adminCfg.Endpoints is empty, Attach returns nil and the framework starts
// without an agent.
//
// Example wiring (typically in cmd/server/main.go):
//
//	a, err := app.New(cfg,
//	    app.WithExtensions(
//	        agent.NewExtension(agent.ExtensionConfig{Endpoints: []string{"https://admin:8443"}}, stateDir, "v0.7.0"),
//	    ),
//	)
//
// The agent's lifecycle is bound to App.Shutdown: graceful Goodbye and
// drain happen when the framework shuts down, no extra wiring needed.
func NewExtension(adminCfg ExtensionConfig, stateDir, version string) app.Extension {
	return &extension{
		adminCfg: adminCfg,
		stateDir: stateDir,
		version:  version,
	}
}

type extension struct {
	adminCfg ExtensionConfig
	stateDir string
	version  string

	agent     *Agent
	runCancel context.CancelFunc
	runDone   chan error
}

func (e *extension) Name() string { return "admin-agent" }

func (e *extension) Attach(a *app.App) error {
	if a == nil {
		return errors.New("admin agent extension: nil app")
	}
	if len(e.adminCfg.Endpoints) == 0 {
		// Fail-open: agent disabled, framework continues unchanged.
		return nil
	}
	if a.Observability == nil {
		// Without a bus the agent has nothing to ship; treat as
		// configuration error to surface this loudly during boot.
		return errors.New("admin agent extension: app.Observability is nil")
	}

	ag, err := New(Config{
		Endpoints:            e.adminCfg.Endpoints,
		Token:                e.adminCfg.Token,
		StateDir:             e.stateDir,
		NodeIDOverride:       e.adminCfg.NodeIDOverride,
		Version:              e.version,
		Labels:               e.adminCfg.Labels,
		Bus:                  a.Observability,
		HeartbeatInterval:    e.adminCfg.HeartbeatInterval,
		DrainTimeout:         e.adminCfg.DrainTimeout,
		HTTPBufferSize:       e.adminCfg.HTTPBufferSize,
		SQLBufferSize:        e.adminCfg.SQLBufferSize,
		SessionBufferSize:    e.adminCfg.SessionBufferSize,
		CustomBufferSize:     e.adminCfg.CustomBufferSize,
		MetricsAddr:          e.adminCfg.MetricsAddr,
		Registry:             a.Models,
		Databases:            a.DBs,
		DefaultDatabaseAlias: e.adminCfg.DefaultDatabaseAlias,
		Logger:               a.Logger,
	})
	if errors.Is(err, ErrDisabled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("admin agent extension: %w", err)
	}
	e.agent = ag

	ctx, cancel := context.WithCancel(context.Background())
	e.runCancel = cancel
	e.runDone = make(chan error, 1)
	go func() { e.runDone <- ag.Run(ctx) }()

	if e.adminCfg.RequireConnection {
		timeout := e.adminCfg.RequireConnectionTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		select {
		case <-ag.Connected():
			a.Logger.Info("admin agent reached admin server within boot deadline",
				"timeout", timeout, "node_id", ag.NodeID())
		case <-time.After(timeout):
			cancel()
			<-e.runDone
			return fmt.Errorf(
				"admin agent extension: require_connection set, no admin endpoint reached within %s",
				timeout,
			)
		}
	}

	return nil
}

func (e *extension) Shutdown(ctx context.Context) error {
	if e.runCancel != nil {
		e.runCancel()
	}
	if e.runDone == nil {
		return nil
	}
	select {
	case <-e.runDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
