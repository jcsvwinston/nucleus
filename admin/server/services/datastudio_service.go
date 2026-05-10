package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/admin/server/nodes"
	"github.com/jcsvwinston/nucleus/admin/server/routing"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// DataStudioService implements adminv1connect.DataStudioServiceHandler.
//
// Every method routes the call to a connected agent that knows the
// requested model, sends a DataStudioRequest down its bidi stream, and
// blocks on the matching DataStudioResponse for at most Timeout.
type DataStudioService struct {
	state   *State
	Timeout time.Duration
}

// NewDataStudioService constructs the handler. timeout <= 0 defaults
// to 10s.
func NewDataStudioService(state *State, timeout time.Duration) *DataStudioService {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &DataStudioService{state: state, Timeout: timeout}
}

// ListModels: returns the union of every connected agent's registered
// model set. When include_counts is true the call is routed to ONE
// agent (the first connected one) so the counts are coherent.
func (s *DataStudioService) ListModels(_ context.Context, req *connect.Request[adminv1.ListModelsRequest]) (*connect.Response[adminv1.ListModelsResponse], error) {
	body := req.Msg
	if !body.GetIncludeCounts() && body.GetNodeId() == "" {
		// Fast path: synthesize the response from the registry without
		// hitting any agent. The UI uses this to populate its sidebar.
		names := s.state.Nodes.AggregateModels()
		out := &adminv1.ListModelsResponse{Models: make([]*adminv1.ModelInfo, 0, len(names))}
		for _, n := range names {
			out.Models = append(out.Models, &adminv1.ModelInfo{
				Name:        n,
				RecordCount: -1,
			})
		}
		return connect.NewResponse(out), nil
	}

	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_ListModels{ListModels: body}}
	resp, err := s.dispatch(body.GetNodeId(), "", wrapped)
	if err != nil {
		return nil, err
	}
	if list := resp.GetListModels(); list != nil {
		return connect.NewResponse(list), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty list_models response"))
}

func (s *DataStudioService) GetSchema(_ context.Context, req *connect.Request[adminv1.GetSchemaRequest]) (*connect.Response[adminv1.ModelSchema], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_GetSchema{GetSchema: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if schema := resp.GetSchema(); schema != nil {
		return connect.NewResponse(schema), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty get_schema response"))
}

func (s *DataStudioService) ListRecords(_ context.Context, req *connect.Request[adminv1.ListRecordsRequest]) (*connect.Response[adminv1.PaginatedRecords], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_ListRecords{ListRecords: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if page := resp.GetRecordsPage(); page != nil {
		return connect.NewResponse(page), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty list_records response"))
}

func (s *DataStudioService) GetRecord(_ context.Context, req *connect.Request[adminv1.GetRecordRequest]) (*connect.Response[adminv1.Record], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_GetRecord{GetRecord: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeNotFound, errors.New("admin server: record not found"))
}

func (s *DataStudioService) CreateRecord(_ context.Context, req *connect.Request[adminv1.CreateRecordRequest]) (*connect.Response[adminv1.Record], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_CreateRecord{CreateRecord: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty create response"))
}

func (s *DataStudioService) UpdateRecord(_ context.Context, req *connect.Request[adminv1.UpdateRecordRequest]) (*connect.Response[adminv1.Record], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_UpdateRecord{UpdateRecord: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty update response"))
}

func (s *DataStudioService) DeleteRecord(_ context.Context, req *connect.Request[adminv1.DeleteRecordRequest]) (*connect.Response[adminv1.DeleteRecordResponse], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_DeleteRecord{DeleteRecord: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if del := resp.GetDeleteRecord(); del != nil {
		return connect.NewResponse(del), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty delete response"))
}

func (s *DataStudioService) BulkAction(_ context.Context, req *connect.Request[adminv1.BulkActionRequest]) (*connect.Response[adminv1.BulkActionResponse], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_BulkAction{BulkAction: body}}
	resp, err := s.dispatch(body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if bulk := resp.GetBulkAction(); bulk != nil {
		return connect.NewResponse(bulk), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty bulk response"))
}

// dispatch picks an agent, allocates a request_id, and sends the
// pre-built request over the agent's bidi stream. Blocks on the
// matching DataStudioResponse for at most s.Timeout.
func (s *DataStudioService) dispatch(nodeID, modelName string, req *adminv1.DataStudioRequest) (*adminv1.DataStudioResponse, error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}

	entry, ok := s.pickAgent(nodeID, modelName)
	if !ok {
		if nodeID != "" {
			return nil, connect.NewError(connect.CodeNotFound,
				fmt.Errorf("admin server: node %q is not connected", nodeID))
		}
		if modelName != "" {
			return nil, connect.NewError(connect.CodeNotFound,
				fmt.Errorf("admin server: no connected agent registered model %q", modelName))
		}
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: no agents connected"))
	}

	id, ch, cancel, err := s.state.DataStudio.Begin()
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}
	req.RequestId = id

	frame := &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_DataStudio{DataStudio: req},
			},
		},
	}
	if !nodes.TryEnqueue(entry, frame) {
		cancel()
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: agent send buffer full or stream closing"))
	}

	resp, err := routing.WaitDataStudio(ch, cancel, s.Timeout)
	if err != nil {
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if resp == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("admin server: empty data studio response"))
	}
	if resp.Error != "" {
		return nil, connect.NewError(connect.CodeUnknown, errors.New(resp.Error))
	}
	return resp, nil
}

// pickAgent returns the entry that should serve the request:
//   - If nodeID is set, that exact node (error when not connected).
//   - Else if modelName is set, any connected agent that has the model.
//   - Else any connected agent (used by include_counts=false ListModels).
func (s *DataStudioService) pickAgent(nodeID, modelName string) (*nodes.Entry, bool) {
	if id := strings.TrimSpace(nodeID); id != "" {
		return s.state.Nodes.Lookup(id)
	}
	if m := strings.TrimSpace(modelName); m != "" {
		return s.state.Nodes.AnyWithModel(m)
	}
	// No constraint: first entry wins.
	var pick *nodes.Entry
	s.state.Nodes.ForEach(func(e *nodes.Entry) {
		if pick == nil {
			pick = e
		}
	})
	return pick, pick != nil
}

// Compile-time assertion.
var _ adminv1connect.DataStudioServiceHandler = (*DataStudioService)(nil)
