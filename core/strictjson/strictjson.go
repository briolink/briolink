// Package strictjson decodes model, spec and contract files strictly (M4):
// one JSON value, no unknown fields, no duplicate keys, keys spelled exactly
// as the Go type declares them. Types decoded here must not use omitempty, so
// that re-encoding them shows every field they know.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Decode decodes data into v, which must be a pointer to a struct.
func Decode(data []byte, v any) error {
	paths, err := keyPaths(data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	// The canonical re-encoding spells every known key exactly, so a key
	// missing from it is unknown or in the wrong case (encoding/json matches
	// case-insensitively); comparing paths also names where it is.
	canonical, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("re-encode: %w", err)
	}
	known, err := keyPaths(canonical)
	if err != nil {
		return fmt.Errorf("re-encode: %w", err)
	}
	var bad []string
	for p := range paths {
		if !known[p] {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("decode: unknown field %q (keys are exact and case-sensitive)", bad[0])
	}
	return nil
}

// keyPaths returns every object key path in data ("a.b[].c"), rejecting
// duplicate keys, invalid JSON and anything after the first value.
func keyPaths(data []byte) (map[string]bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	paths := map[string]bool{}
	if err := walk(dec, "", paths); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: data after the top-level value")
	}
	return paths, nil
}

func walk(dec *json.Decoder, path string, paths map[string]bool) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return fmt.Errorf("decode: %w", err)
			}
			key := kt.(string)
			if seen[key] {
				return fmt.Errorf("decode: duplicate key %q", join(path, key))
			}
			seen[key] = true
			p := join(path, key)
			paths[p] = true
			if err := walk(dec, p, paths); err != nil {
				return err
			}
		}
		_, err = dec.Token()
	case json.Delim('['):
		for dec.More() {
			if err := walk(dec, path+"[]", paths); err != nil {
				return err
			}
		}
		_, err = dec.Token()
	}
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + strings.ReplaceAll(key, ".", `\.`)
}
