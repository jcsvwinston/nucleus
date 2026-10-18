// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

// The framework links no jobs provider that brings a client library with it:
// the asynq one ships as its own module (providers/tasks-asynq, DEP-2026-020)
// so an application that runs its jobs in-process or on its own database
// does not carry asynq and a Redis client. The CLI links it for the same
// reason it links every database engine: `nucleus doctor --check tasks`
// inspects whatever queue is in front of it, and an asynq deployment has to
// be inspectable without rebuilding the tool.
//
// The blank import registers the provider with pkg/tasks, which is how the
// doctor's tasks check resolves `jobs_provider: asynq`.

import (
	_ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
)
