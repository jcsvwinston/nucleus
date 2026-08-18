// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"os"
)

// defaultOutboxLeaseOwner derives a per-instance outbox lease owner
// (QCD-FW-5): "nucleus-<hostname>-<pid>". The pid keeps two processes on the
// same host distinct; leases expire, so identity does not need to survive a
// restart. Operators who want a stable identity (e.g. a k8s pod name) set
// outbox.lease_owner explicitly.
func defaultOutboxLeaseOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("nucleus-%s-%d", host, os.Getpid())
}
