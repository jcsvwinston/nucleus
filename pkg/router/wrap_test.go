// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// DEP-2026-023: Wrap is the constructor; NewWrapResponseWriter keeps working
// until v2.0.0 with its never-read second argument. Both record the same
// status and byte count, so a caller that switches sees no difference.
func TestWrap_RecordsStatusAndBytes(t *testing.T) {
	for name, mk := range map[string]func(http.ResponseWriter) *WrapResponseWriter{
		"Wrap":                  Wrap,
		"NewWrapResponseWriter": func(w http.ResponseWriter) *WrapResponseWriter { return NewWrapResponseWriter(w, 1) }, //nolint:staticcheck // the deprecated form is what this test pins until v2.0.0
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ww := mk(rec)
			if ww.Status() != http.StatusOK || ww.WroteHeader() {
				t.Fatalf("fresh writer: Status=%d WroteHeader=%v, want 200/false", ww.Status(), ww.WroteHeader())
			}
			ww.WriteHeader(http.StatusCreated)
			n, err := ww.Write([]byte("created"))
			if err != nil || n != 7 {
				t.Fatalf("Write = %d, %v", n, err)
			}
			if ww.Status() != http.StatusCreated || ww.BytesWritten() != 7 || rec.Code != http.StatusCreated {
				t.Fatalf("Status=%d BytesWritten=%d rec.Code=%d, want 201/7/201", ww.Status(), ww.BytesWritten(), rec.Code)
			}
		})
	}
}
