// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import "github.com/google/uuid"

// newID names a connection. Connection ids are not secrets — presence exposes
// them — but they must not collide, which is the whole requirement.
func newID() string { return uuid.NewString() }
