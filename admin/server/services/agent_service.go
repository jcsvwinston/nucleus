// Package services holds the Connect-RPC handler implementations for
// AgentService (admin <-> agent) and ControlService (UI <-> admin).
//
// Both services depend on the same shared state: nodes.Registry,
// routing.EventBus, routing.Replay, routing.SnapshotRouter. The State
// struct in this package wires them together.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/admin/server/nodes"
	"github.com/jcsvwinston/nucleus/admin/server/routing"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// State groups the shared dependencies the two service handlers need.
// One State per server lifetime.
type State struct {
	Nodes          *nodes.Registry
	EventBus       *routing.EventBus
	Replay         *routing.Replay
	Snapshots      *routing.SnapshotRouter
	DataStudio     *routing.DataStudioRouter
	Logger         *slog.Logger
	SendChanBuffer int
	OnAgentSubMode func(*nodes.Entry, *routing.EventBus) // hook called whenever bus demand changes
	HeartbeatGrace time.Duration                          // tolerance window for stale heartbeat reports
}

// AgentService implements adminv1connect.AgentServiceHandler.
type AgentService struct {
	state *State
}

// NewAgentService constructs the handler.
func NewAgentService(state *State) *AgentService {
	return &AgentService{state: state}
}

// Stream is the bidi entry point for an agent. The handler:
//   - reads the first frame, expects NodeRegistration, registers the
//     agent in the nodes.Registry,
//   - spawns a writer goroutine that drains entry.Send into the stream,
//   - dispatches subsequent inbound frames (events, heartbeats,
//     snapshot responses) until the stream ends.
func (s *AgentService) Stream(ctx context.Context, stream *connect.BidiStream[adminv1.Frame, adminv1.Frame]) error {
	if s == nil || s.state == nil {
		return connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}

	first, err := stream.Receive()
	if err != nil {
		return connect.NewError(connect.CodeAborted, fmt.Errorf("admin agent: receive registration: %w", err))
	}
	reg := first.GetRegistration()
	if reg == nil || strings.TrimSpace(reg.GetNodeId()) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("admin agent: first frame must be a non-empty NodeRegistration"))
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	info := nodes.NodeInfo{
		NodeID:           strings.TrimSpace(reg.GetNodeId()),
		Version:          reg.GetVersion(),
		Labels:           cloneLabels(reg.GetLabels()),
		StartedAt:        reg.GetStartedAt().AsTime(),
		RegisteredModels: append([]string(nil), reg.GetRegisteredModels()...),
	}

	entry, deregister := s.state.Nodes.Add(streamCtx, info, s.state.SendChanBuffer)
	defer deregister()

	s.state.Logger.Info("admin agent connected",
		"node_id", info.NodeID,
		"version", info.Version)

	// Push the initial agent-side aggregate Subscribe so the agent starts
	// shipping events that any current UI cares about.
	if s.state.OnAgentSubMode != nil {
		s.state.OnAgentSubMode(entry, s.state.EventBus)
	}

	// Writer goroutine: drains entry.Send into the bidi stream serially.
	writerDone := make(chan error, 1)
	go func() { writerDone <- s.runWriter(streamCtx, entry, stream) }()

	// Reader goroutine: us. Process inbound frames until error/EOF.
	for {
		frame, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			} else if connect.CodeOf(err) == connect.CodeCanceled || streamCtx.Err() != nil {
				err = nil
			}
			cancel()
			<-writerDone
			return err
		}

		s.state.Nodes.Touch(info.NodeID, time.Now().UTC())

		switch body := frame.GetBody().(type) {
		case *adminv1.Frame_Event:
			if body.Event != nil {
				s.state.Replay.Push(body.Event)
				s.state.EventBus.Publish(body.Event)
			}
		case *adminv1.Frame_Heartbeat:
			// last-seen already touched above; nothing else to do.
		case *adminv1.Frame_SnapshotResponse:
			s.state.Snapshots.Resolve(body.SnapshotResponse)
		case *adminv1.Frame_DataStudioResponse:
			if s.state.DataStudio != nil {
				s.state.DataStudio.Resolve(body.DataStudioResponse)
			}
		case *adminv1.Frame_Goodbye:
			s.state.Logger.Info("admin agent client goodbye",
				"node_id", info.NodeID,
				"reason", body.Goodbye.GetReason())
			cancel()
			<-writerDone
			return nil
		case *adminv1.Frame_Registration:
			// Defensive: an agent that re-registers mid-stream gets its
			// metadata refreshed but we do not change the registry key.
			s.state.Logger.Debug("admin agent re-registration on live stream",
				"node_id", info.NodeID)
		default:
			s.state.Logger.Debug("admin agent ignoring unknown frame",
				"node_id", info.NodeID, "type", fmt.Sprintf("%T", body))
		}
	}
}

// runWriter drains entry.Send and pushes frames to the bidi stream.
// Returns when the stream context is cancelled.
func (s *AgentService) runWriter(ctx context.Context, entry *nodes.Entry, stream *connect.BidiStream[adminv1.Frame, adminv1.Frame]) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-entry.Send:
			if frame == nil {
				continue
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		}
	}
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Compile-time assertion that AgentService satisfies the proto interface.
var _ adminv1connect.AgentServiceHandler = (*AgentService)(nil)

// _ keeps sync imported even when unused by future edits.
var _ = sync.Mutex{}
