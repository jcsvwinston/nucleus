// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package interceptor

import (
	"net/http"

	"github.com/jcsvwinston/nucleus/internal/errreport"
)

// ErrorReporter is implemented by the http.ResponseWriter an interceptor
// hands down the chain when the interceptor wants the error behind a 500.
//
// An interceptor sees every request and the status it was answered with. It
// does not see why: a handler that returns an error the framework cannot
// classify — neither a DomainError nor an HTTPError — is answered "internal
// server error", and the error itself goes to the log and nowhere else. An
// interceptor that reports errors (to an error tracker, say) needs the error,
// so the router hands it over: when a handler's error is answered as an
// internal one, ReportError is called with the request as the handler saw
// it — its context carries the request id and the user the framework
// attributes the request to — and the error, before the 500 is written.
//
// The interceptor wraps the writer it receives in one of its own that
// implements this method, and passes that writer to the next handler; the
// framework notices it there. It is an optional method on the writer, the
// way http.Flusher is, rather than a function to register, for two reasons:
// the interceptor that installed it is the one http_interceptors selected
// for this application, and a module that implements it compiles against a
// framework release that predates this interface — it simply is not called
// there.
//
// ReportError runs on the handler's goroutine, which is not always the one
// the interceptor runs on (a route with its own Timeout runs its handler on
// another), so an implementation that keeps state guards it. It must not
// write the response, and must not call the ReportError of the writer it
// wraps: every reporter in the chain is told once. A panic in ReportError is
// recovered and does not change the 500.
//
// A panic is not reported this way: it unwinds through the interceptor,
// which recovers it — and re-panics, so the framework still answers 500 and
// logs the stack.
type ErrorReporter interface {
	ReportError(r *http.Request, err error)
}

// reportingErrors wraps a built interceptor so that a writer it hands down
// which implements ErrorReporter is installed in the request's context, where
// the router finds it.
func reportingErrors(ic Interceptor) Interceptor {
	return func(next http.Handler) http.Handler {
		return ic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rep, ok := w.(ErrorReporter); ok {
				r = r.WithContext(errreport.With(r.Context(), rep))
			}
			next.ServeHTTP(w, r)
		}))
	}
}
