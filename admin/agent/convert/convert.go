// Package convert translates the framework's in-process observability
// events into proto messages on the wire. It is the only place in the
// agent that knows about both data shapes; the connection and stream
// layers operate on proto types only, the bus layer operates on Go types
// only.
//
// Conversion is allocation-aware: it reuses the destination proto message
// when possible (for repeated emissions on the same stream) but does not
// pool the proto messages — Connect-RPC retains references to sent
// messages until they're serialized, and the contract for sync.Pool
// requires single-owner semantics that we cannot guarantee at the wire
// boundary.
package convert

import (
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// EventToProto converts an in-process observability.Event to a proto
// adminv1.Event. The returned message is a fresh allocation; the input is
// untouched (the caller still owns it and must Release it).
//
// Returns nil if the event kind is unknown — the agent should never see
// such an event, but the safe behaviour is to drop rather than crash.
func EventToProto(in observability.Event) *adminv1.Event {
	if in == nil {
		return nil
	}

	out := &adminv1.Event{
		Timestamp: timestamppb.New(in.EmittedAt()),
		NodeId:    in.NodeID(),
	}

	switch e := in.(type) {
	case *observability.HTTPRequestEvent:
		out.Body = &adminv1.Event_HttpRequest{
			HttpRequest: httpEventToProto(e),
		}
	case *observability.SQLStatementEvent:
		out.Body = &adminv1.Event_SqlStatement{
			SqlStatement: sqlEventToProto(e),
		}
	case *observability.SessionChangeEvent:
		out.Body = &adminv1.Event_SessionChange{
			SessionChange: sessionEventToProto(e),
		}
	case *observability.CustomEvent:
		out.Body = &adminv1.Event_Custom{
			Custom: customEventToProto(e),
		}
	default:
		return nil
	}

	return out
}

func httpEventToProto(e *observability.HTTPRequestEvent) *adminv1.HttpRequestEvent {
	return &adminv1.HttpRequestEvent{
		Method:         e.Method,
		Path:           e.Path,
		Status:         uint32(e.Status),
		Duration:       durationpb.New(e.Duration),
		RequestId:      e.RequestID,
		TraceId:        e.TraceID,
		UserId:         e.UserID,
		RemoteIp:       e.RemoteIP,
		UserAgent:      e.UserAgent,
		PayloadPreview: e.PayloadPreview,
	}
}

func sqlEventToProto(e *observability.SQLStatementEvent) *adminv1.SqlStatementEvent {
	args := make([]string, len(e.Args))
	copy(args, e.Args)
	return &adminv1.SqlStatementEvent{
		ModelName: e.ModelName,
		Operation: e.Operation,
		Query:     e.Query,
		Args:      args,
		Duration:  durationpb.New(e.Duration),
		Error:     e.Err,
		RequestId: e.RequestID,
		TraceId:   e.TraceID,
		UserId:    e.UserID,
	}
}

func sessionEventToProto(e *observability.SessionChangeEvent) *adminv1.SessionChangeEvent {
	var kind adminv1.SessionChangeEvent_Kind
	switch e.Change {
	case observability.SessionChangeCreated:
		kind = adminv1.SessionChangeEvent_KIND_CREATED
	case observability.SessionChangeTouched:
		kind = adminv1.SessionChangeEvent_KIND_TOUCHED
	case observability.SessionChangeDestroyed:
		kind = adminv1.SessionChangeEvent_KIND_DESTROYED
	default:
		kind = adminv1.SessionChangeEvent_KIND_UNSPECIFIED
	}
	return &adminv1.SessionChangeEvent{
		Kind:       kind,
		TokenShort: e.TokenShort,
		UserId:     e.UserID,
		Ip:         e.IP,
		UserAgent:  e.UserAgent,
		LastRoute:  e.LastRoute,
		TraceId:    e.TraceID,
	}
}

func customEventToProto(e *observability.CustomEvent) *adminv1.CustomEvent {
	var labels map[string]string
	if len(e.Labels) > 0 {
		labels = make(map[string]string, len(e.Labels))
		for k, v := range e.Labels {
			labels[k] = v
		}
	}
	payload := make([]byte, len(e.Payload))
	copy(payload, e.Payload)
	return &adminv1.CustomEvent{
		Name:        e.Name,
		Labels:      labels,
		Payload:     payload,
		ContentType: e.ContentType,
	}
}

// KindToProto maps an in-process EventKind to its proto enum.
func KindToProto(k observability.EventKind) adminv1.EventType {
	switch k {
	case observability.KindHTTPRequest:
		return adminv1.EventType_EVENT_TYPE_HTTP_REQUEST
	case observability.KindSQLStatement:
		return adminv1.EventType_EVENT_TYPE_SQL_STATEMENT
	case observability.KindSessionChange:
		return adminv1.EventType_EVENT_TYPE_SESSION_CHANGE
	case observability.KindCustom:
		return adminv1.EventType_EVENT_TYPE_CUSTOM
	default:
		return adminv1.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

// KindFromProto maps a proto event type back to the in-process EventKind.
// Returns KindUnknown for unrecognised values.
func KindFromProto(et adminv1.EventType) observability.EventKind {
	switch et {
	case adminv1.EventType_EVENT_TYPE_HTTP_REQUEST:
		return observability.KindHTTPRequest
	case adminv1.EventType_EVENT_TYPE_SQL_STATEMENT:
		return observability.KindSQLStatement
	case adminv1.EventType_EVENT_TYPE_SESSION_CHANGE:
		return observability.KindSessionChange
	case adminv1.EventType_EVENT_TYPE_CUSTOM:
		return observability.KindCustom
	default:
		return observability.KindUnknown
	}
}

// FilterFromProto translates a proto Filter (the Subscribe payload) to an
// in-process observability.Filter. NodeIDs and Kinds are propagated; the
// HTTP/SQL-specific dimensions are forwarded via SubscriptionFilter to the
// stream layer (because the bus's Filter is intentionally narrow).
func FilterFromProto(in *adminv1.Filter) observability.Filter {
	if in == nil {
		return observability.Filter{}
	}
	out := observability.Filter{}
	if len(in.Types) > 0 {
		out.Kinds = make([]observability.EventKind, 0, len(in.Types))
		for _, t := range in.Types {
			k := KindFromProto(t)
			if k == observability.KindUnknown {
				continue
			}
			out.Kinds = append(out.Kinds, k)
		}
	}
	if len(in.NodeIds) > 0 {
		out.NodeIDs = append([]string(nil), in.NodeIds...)
	}
	return out
}
