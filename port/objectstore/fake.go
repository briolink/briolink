package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Store for tests of the runner and control plane.
type Fake struct {
	mu   sync.Mutex
	objs map[string]Object
}

// NewFake returns an empty Fake.
func NewFake() *Fake { return &Fake{objs: map[string]Object{}} }

func (f *Fake) Put(ctx context.Context, key string, body []byte, opts PutOptions) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if key == "" {
		return Info{}, ErrInvalid
	}
	sum := sha256.Sum256(body)
	info := Info{
		Key:          key,
		Size:         int64(len(body)),
		ETag:         hex.EncodeToString(sum[:16]),
		ContentType:  opts.ContentType,
		CacheControl: opts.CacheControl,
		Metadata:     maps.Clone(opts.Metadata),
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[key] = Object{Info: info, Body: slices.Clone(body)}
	return cloneInfo(info), nil
}

func (f *Fake) Get(ctx context.Context, key, ifNoneMatch string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objs[key]
	if !ok {
		return Object{}, ErrNotFound
	}
	if ifNoneMatch != "" && ifNoneMatch == o.ETag {
		return Object{}, ErrNotModified
	}
	return Object{Info: cloneInfo(o.Info), Body: slices.Clone(o.Body)}, nil
}

func (f *Fake) Head(ctx context.Context, key string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objs[key]
	if !ok {
		return Info{}, ErrNotFound
	}
	return cloneInfo(o.Info), nil
}

func (f *Fake) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, key)
	return nil
}

func (f *Fake) List(ctx context.Context, prefix string) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Info
	for _, k := range slices.Sorted(maps.Keys(f.objs)) {
		if strings.HasPrefix(k, prefix) {
			out = append(out, cloneInfo(f.objs[k].Info))
		}
	}
	return out, nil
}

func (f *Fake) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return presign(ctx, "get", key, ttl)
}

func (f *Fake) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return presign(ctx, "put", key, ttl)
}

func presign(ctx context.Context, verb, key string, ttl time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if key == "" || ttl <= 0 {
		return "", ErrInvalid
	}
	return "fake://" + verb + "/" + url.PathEscape(key) + "?ttl=" + ttl.String(), nil
}

func cloneInfo(i Info) Info {
	i.Metadata = maps.Clone(i.Metadata)
	return i
}
