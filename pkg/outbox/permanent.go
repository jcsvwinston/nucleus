// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"errors"
	"fmt"
)

// ErrPermanent is what IsPermanent finds in an error built with Permanent.
var ErrPermanent = errors.New("outbox: permanent delivery failure")

// Permanent marks a delivery failure that another attempt cannot fix — the
// receiver refused the message itself, not the moment it was sent. A
// bridge (or a HandlerFunc) returns it, and the dispatcher fails the
// message at once instead of retrying it: it goes to the dead letter (the
// "failed" state, which RequeueFailed reverses) with its reason in
// last_error, without spending the rest of its attempts on an answer that
// will not change.
//
// Every other error keeps the outbox's ordinary semantics: retry with
// backoff until MaxAttempts, then the dead letter. A nil err stays nil.
//
// When a message is routed to several bridges, it fails at once only if
// every bridge that failed failed permanently; one retriable failure among
// them retries the message.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err was marked with Permanent.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrPermanent)
}

type permanentError struct{ err error }

func (e *permanentError) Error() string   { return e.err.Error() }
func (e *permanentError) Unwrap() []error { return []error{e.err, ErrPermanent} }

// dispatchErrors is what a delivery through several bridges returns when
// some of them failed. Its text is the one the dispatcher has always
// written into last_error; it keeps the individual errors so the
// dispatcher can tell whether all of them were permanent.
type dispatchErrors struct{ errs []error }

func (e *dispatchErrors) Error() string   { return fmt.Sprintf("dispatch errors: %v", e.errs) }
func (e *dispatchErrors) Unwrap() []error { return e.errs }

// failsPermanently reports whether a delivery error should send the message
// to the dead letter without another attempt: a Permanent error, or a
// fan-out in which every failure was one.
func failsPermanently(err error) bool {
	var fanout *dispatchErrors
	if errors.As(err, &fanout) {
		if len(fanout.errs) == 0 {
			return false
		}
		for _, e := range fanout.errs {
			if !IsPermanent(e) {
				return false
			}
		}
		return true
	}
	return IsPermanent(err)
}
