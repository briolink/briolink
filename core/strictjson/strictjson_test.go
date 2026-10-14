package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

type inner struct {
	Name string `json:"name"`
}

type outer struct {
	Format  string          `json:"format"`
	Items   []inner         `json:"items"`
	Options json.RawMessage `json:"options"`
	Tags    map[string]int  `json:"tags"`
}

func TestDecodeValid(t *testing.T) {
	var v outer
	err := Decode([]byte(`{"format":"v0","items":[{"name":"a"},{"name":"b"}],"options":{"Mixed":1},"tags":{"X":1}}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 2 || v.Items[1].Name != "b" {
		t.Fatalf("got %+v", v)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"unknown field":        {`{"format":"v0","extra":1}`, `unknown field "extra"`},
		"unknown nested field": {`{"items":[{"name":"a","x":1}]}`, `unknown field "items[].x"`},
		"wrong case":           {`{"Format":"v0"}`, `unknown field "Format"`},
		"wrong case nested":    {`{"items":[{"NAME":"a"}]}`, `unknown field "items[].NAME"`},
		"duplicate key":        {`{"format":"v0","format":"v1"}`, `duplicate key "format"`},
		"duplicate nested key": {`{"items":[{"name":"a","name":"b"}]}`, `duplicate key "items[].name"`},
		"duplicate in raw":     {`{"options":{"a":1,"a":2}}`, `duplicate key "options.a"`},
		"trailing value":       {`{"format":"v0"} {}`, "data after the top-level value"},
		"wrong type":           {`{"format":1}`, "cannot unmarshal number"},
		"invalid json":         {`{"format":`, "decode:"},
		"not an object":        {`[1]`, "cannot unmarshal array"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var v outer
			err := Decode([]byte(c.in), &v)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want error containing %q", err, c.want)
			}
		})
	}
}
