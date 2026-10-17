package objectstore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// Contract is the suite every Store must pass. newStore returns an empty store
// (a fresh bucket or prefix) per call.
func Contract(t *testing.T, newStore func(t *testing.T) Store) {
	ctx := context.Background()
	run := func(name string, fn func(t *testing.T, s Store)) {
		t.Run(name, func(t *testing.T) { fn(t, newStore(t)) })
	}

	run("put then get keeps body and properties", func(t *testing.T, s Store) {
		opts := PutOptions{ContentType: "text/html", CacheControl: "max-age=60", Metadata: map[string]string{"site": "acme"}}
		put, err := s.Put(ctx, "a/b.html", []byte("<p>hi</p>"), opts)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, "a/b.html", "")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Body, []byte("<p>hi</p>")) || got.Size != 9 {
			t.Fatalf("body/size: %q %d", got.Body, got.Size)
		}
		if got.ContentType != "text/html" || got.CacheControl != "max-age=60" || got.Metadata["site"] != "acme" {
			t.Fatalf("properties lost: %+v", got.Info)
		}
		if got.ETag == "" || got.ETag != put.ETag {
			t.Fatalf("etag: put %q get %q", put.ETag, got.ETag)
		}
	})

	run("put replaces and changes the etag", func(t *testing.T, s Store) {
		a, _ := s.Put(ctx, "k", []byte("one"), PutOptions{})
		b, err := s.Put(ctx, "k", []byte("two"), PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if a.ETag == b.ETag {
			t.Fatal("etag must change with the body")
		}
		got, _ := s.Get(ctx, "k", "")
		if string(got.Body) != "two" {
			t.Fatalf("got %q", got.Body)
		}
	})

	run("conditional get", func(t *testing.T, s Store) {
		info, _ := s.Put(ctx, "k", []byte("x"), PutOptions{})
		if _, err := s.Get(ctx, "k", info.ETag); !errors.Is(err, ErrNotModified) {
			t.Fatalf("matching etag: %v", err)
		}
		if _, err := s.Get(ctx, "k", "other"); err != nil {
			t.Fatalf("non-matching etag: %v", err)
		}
	})

	run("missing key", func(t *testing.T, s Store) {
		if _, err := s.Get(ctx, "nope", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("get: %v", err)
		}
		if _, err := s.Head(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("head: %v", err)
		}
	})

	run("head matches put", func(t *testing.T, s Store) {
		put, _ := s.Put(ctx, "k", []byte("abc"), PutOptions{ContentType: "text/plain"})
		head, err := s.Head(ctx, "k")
		if err != nil {
			t.Fatal(err)
		}
		if head.Size != 3 || head.ETag != put.ETag || head.ContentType != "text/plain" {
			t.Fatalf("head %+v, put %+v", head, put)
		}
	})

	run("delete is idempotent", func(t *testing.T, s Store) {
		s.Put(ctx, "k", []byte("x"), PutOptions{})
		for i := 0; i < 2; i++ {
			if err := s.Delete(ctx, "k"); err != nil {
				t.Fatalf("delete %d: %v", i, err)
			}
		}
		if _, err := s.Head(ctx, "k"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("still there: %v", err)
		}
	})

	run("list by prefix in key order", func(t *testing.T, s Store) {
		for _, k := range []string{"b/2", "a/2", "a/1", "c"} {
			s.Put(ctx, k, []byte(k), PutOptions{})
		}
		got, err := s.List(ctx, "a/")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Key != "a/1" || got[1].Key != "a/2" {
			t.Fatalf("got %+v", got)
		}
		all, _ := s.List(ctx, "")
		if len(all) != 4 {
			t.Fatalf("empty prefix lists all, got %d", len(all))
		}
		none, err := s.List(ctx, "zz")
		if err != nil || len(none) != 0 {
			t.Fatalf("no match: %v %v", none, err)
		}
	})

	run("returned bodies are copies", func(t *testing.T, s Store) {
		in := []byte("abc")
		s.Put(ctx, "k", in, PutOptions{})
		in[0] = 'X'
		got, _ := s.Get(ctx, "k", "")
		got.Body[1] = 'Y'
		again, _ := s.Get(ctx, "k", "")
		if string(again.Body) != "abc" {
			t.Fatalf("store aliased a caller's slice: %q", again.Body)
		}
	})

	run("presign", func(t *testing.T, s Store) {
		g, err := s.PresignGet(ctx, "k", time.Minute)
		if err != nil || g == "" {
			t.Fatalf("get: %q %v", g, err)
		}
		p, err := s.PresignPut(ctx, "k", time.Minute)
		if err != nil || p == "" || p == g {
			t.Fatalf("put: %q %v (get %q)", p, err, g)
		}
		if _, err := s.PresignGet(ctx, "k", 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("zero ttl: %v", err)
		}
		if _, err := s.PresignPut(ctx, "", time.Minute); !errors.Is(err, ErrInvalid) {
			t.Fatalf("empty key: %v", err)
		}
	})

	run("empty key is invalid", func(t *testing.T, s Store) {
		if _, err := s.Put(ctx, "", []byte("x"), PutOptions{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	run("cancelled context", func(t *testing.T, s Store) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.Put(c, "k", []byte("x"), PutOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
}
