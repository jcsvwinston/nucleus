package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/nucleus/admin/server/nodes"
	"github.com/jcsvwinston/nucleus/admin/server/routing"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// ControlService implements adminv1connect.ControlServiceHandler.
type ControlService struct {
	state *State

	// EventChannelSize is the per-StreamEvents subscription buffer.
	EventChannelSize int

	// SnapshotTimeout caps how long GetSnapshot waits for the agent.
	SnapshotTimeout time.Duration
}

// NewControlService constructs the handler.
func NewControlService(state *State, eventChannelSize int, snapshotTimeout time.Duration) *ControlService {
	if eventChannelSize <= 0 {
		eventChannelSize = 256
	}
	if snapshotTimeout <= 0 {
		snapshotTimeout = 5 * time.Second
	}
	return &ControlService{
		state:            state,
		EventChannelSize: eventChannelSize,
		SnapshotTimeout:  snapshotTimeout,
	}
}

// ListNodes returns a stable view of every connected agent.
func (s *ControlService) ListNodes(ctx context.Context, req *connect.Request[adminv1.ListNodesRequest]) (*connect.Response[adminv1.ListNodesResponse], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}

	infos := s.state.Nodes.List()
	out := &adminv1.ListNodesResponse{
		Nodes: make([]*adminv1.NodeInfo, 0, len(infos)),
	}
	for _, info := range infos {
		out.Nodes = append(out.Nodes, &adminv1.NodeInfo{
			NodeId:     info.NodeID,
			Version:    info.Version,
			Labels:     cloneLabels(info.Labels),
			StartedAt:  timestamppb.New(info.StartedAt),
			LastSeenAt: timestamppb.New(info.LastSeenAt),
			Connected:  info.Connected,
		})
	}
	return connect.NewResponse(out), nil
}

// StreamEvents subscribes to live events. The handler:
//   - registers a new EventBus subscription with the requested filter,
//   - rebuilds and pushes the agent-side aggregate filter to every
//     connected agent (so they switch on/refine ingress as needed),
//   - optionally replays the recent ring buffer first when
//     include_recent is set,
//   - drains the subscription channel into the response stream until ctx
//     is cancelled.
func (s *ControlService) StreamEvents(ctx context.Context, req *connect.Request[adminv1.StreamEventsRequest], stream *connect.ServerStream[adminv1.Event]) error {
	if s == nil || s.state == nil {
		return connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	body := req.Msg

	sub, cancel := s.state.EventBus.Subscribe(body.GetFilter(), body.GetSamplingRate(), s.EventChannelSize)
	defer cancel()

	// Recompute the agent-side aggregate Subscribe and push it to every
	// connected agent. The agent is responsible for replacing any
	// previous server-aggregate sub atomically (proto guarantees this on
	// duplicate subscription_id).
	s.pushAggregateToAgents()
	defer s.pushAggregateToAgents()

	if body.GetIncludeRecent() && s.state.Replay != nil {
		for _, ev := range s.state.Replay.Snapshot(body.GetFilter(), 0) {
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-sub.Ch():
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// GetSnapshot routes a snapshot request to the right agent and waits for
// the matching SnapshotResponse over its bidi stream.
func (s *ControlService) GetSnapshot(ctx context.Context, req *connect.Request[adminv1.GetSnapshotRequest]) (*connect.Response[adminv1.Snapshot], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	body := req.Msg
	if body.GetNodeId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("admin server: node_id is required"))
	}

	entry, ok := s.state.Nodes.Lookup(body.GetNodeId())
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("admin server: node %q is not connected", body.GetNodeId()))
	}

	id, ch, cancel, err := s.state.Snapshots.Begin()
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	frame := &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_SnapshotRequest{
					SnapshotRequest: &adminv1.SnapshotRequest{
						RequestId: id,
						Type:      body.GetType(),
					},
				},
			},
		},
	}
	if !nodes.TryEnqueue(entry, frame) {
		cancel()
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: agent send buffer full or stream closing"))
	}

	resp, err := routing.Wait(ch, cancel, s.SnapshotTimeout)
	if err != nil {
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if resp == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("admin server: empty snapshot response"))
	}
	if resp.Error != "" {
		return nil, connect.NewError(connect.CodeUnknown, errors.New(resp.Error))
	}

	return connect.NewResponse(&adminv1.Snapshot{
		NodeId:      body.GetNodeId(),
		Type:        body.GetType(),
		GeneratedAt: timestamppb.Now(),
		PayloadJson: resp.PayloadJson,
	}), nil
}

// pushAggregateToAgents recomputes the union filter every connected
// agent should apply, and pushes it to each one. When zero subscribers
// remain the function pushes an Unsubscribe to clear ingress.
func (s *ControlService) pushAggregateToAgents() {
	const aggregateID = "server-aggregate"

	// Build the union filter once.
	agg := s.state.EventBus.AggregateFilter()

	var frame *adminv1.Frame
	if agg == nil {
		frame = &adminv1.Frame{
			Body: &adminv1.Frame_Command{
				Command: &adminv1.Command{
					Body: &adminv1.Command_Unsubscribe{
						Unsubscribe: &adminv1.Unsubscribe{
							SubscriptionId: aggregateID,
						},
					},
				},
			},
		}
	} else {
		frame = &adminv1.Frame{
			Body: &adminv1.Frame_Command{
				Command: &adminv1.Command{
					Body: &adminv1.Command_Subscribe{
						Subscribe: &adminv1.Subscribe{
							SubscriptionId: aggregateID,
							Filter:         agg,
						},
					},
				},
			},
		}
	}

	s.state.Nodes.ForEach(func(e *nodes.Entry) {
		nodes.TryEnqueue(e, frame)
	})
}

// Compile-time assertion.
var _ adminv1connect.ControlServiceHandler = (*ControlService)(nil)

var _ = sync.Mutex{}
