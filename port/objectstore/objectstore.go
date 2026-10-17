// Package objectstore is the ObjectStore port: the S3 subset the platform
// relies on (docs/2-architecture.md § Roles and adapters). Objects are small
// (baked pages, uploads, evidence), so bodies are byte slices. An adapter must
// pass Contract before it can be bound.
package objectstore

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound: the key does not exist.
	ErrNotFound = errors.New("objectstore: not found")
	// ErrNotModified: a conditional Get matched the caller's ETag.
	ErrNotModified = errors.New("objectstore: not modified")
	// ErrInvalid: an empty key, a non-positive expiry or similar misuse.
	ErrInvalid = errors.New("objectstore: invalid argument")
)

// PutOptions are the writable properties of an object.
type PutOptions struct {
	ContentType  string
	CacheControl string
	Metadata     map[string]string
}

// Info describes an object without its body.
type Info struct {
	Key          string
	Size         int64
	ETag         string // changes whenever the body changes
	ContentType  string
	CacheControl string
	Metadata     map[string]string
}

// Object is an Info with its body.
type Object struct {
	Info
	Body []byte
}

// Store is the port.
type Store interface {
	// Put writes the object, replacing any existing one, and returns its Info.
	Put(ctx context.Context, key string, body []byte, opts PutOptions) (Info, error)
	// Get returns the object. A non-empty ifNoneMatch equal to the current
	// ETag gives ErrNotModified; a missing key gives ErrNotFound.
	Get(ctx context.Context, key, ifNoneMatch string) (Object, error)
	// Head returns the Info of an existing key, or ErrNotFound.
	Head(ctx context.Context, key string) (Info, error)
	// Delete removes the key; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// List returns the objects whose key starts with prefix, in key order.
	List(ctx context.Context, prefix string) ([]Info, error)
	// PresignGet and PresignPut return a URL valid for ttl that allows one
	// kind of access to the key without credentials. ttl must be positive.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error)
}
