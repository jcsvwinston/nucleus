// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import "io"

// SetPrintRoutesOutputForTest points the NUCLEUS_PRINT_ROUTES document at w
// for an external test and returns the restore function.
func SetPrintRoutesOutputForTest(w io.Writer) func() {
	prev := printRoutesOut
	printRoutesOut = w
	return func() { printRoutesOut = prev }
}
