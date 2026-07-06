//go:build !fleet

package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

// fleetOptions is a no-op without the "fleet" build tag; see fleet.go.
func fleetOptions() []nucleus.Option { return nil }
