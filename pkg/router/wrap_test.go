// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Wrap is the only constructor of WrapResponseWriter (DEP-2026-023 removed
// NewWrapResponseWriter and its never-read second argument at v2.0.0): a
// fresh writer reports 200 until the handler writes another status, and it
// records the status and byte count of what the handler wrote.
func TestWrap_RecordsStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	ww := Wrap(rec)
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
}
