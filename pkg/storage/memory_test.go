// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := New(Config{Provider: "memory"}, nil)
	if err != nil {
		t.Fatalf("New(memory): %v", err)
	}
	if _, err := s.Put(ctx, "uploads/a.txt", strings.NewReader("alpha"), PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, "uploads/b.txt", strings.NewReader("beta"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, "other/c.txt", strings.NewReader("gamma"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	rc, info, err := s.Get(ctx, "uploads/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "alpha" || info.ContentType != "text/plain" || info.Size != 5 {
		t.Fatalf("got %q %+v", body, info)
	}
	list, err := s.List(ctx, ListOptions{Prefix: "uploads/"})
	if err != nil || len(list.Objects) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if ok, _ := s.Exists(ctx, "other/c.txt"); !ok {
		t.Fatal("Exists lost an object")
	}
	if err := s.Delete(ctx, "other/c.txt"); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Get(ctx, "other/c.txt")
	var nf ErrNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("a missing key answers %v, want ErrNotFound", err)
	}
	if _, err := s.Copy(ctx, "uploads/a.txt", "copies/a.txt"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Exists(ctx, "copies/a.txt"); !ok {
		t.Fatal("Copy did not create the destination")
	}
}
