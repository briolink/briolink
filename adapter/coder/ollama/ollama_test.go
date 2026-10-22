package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/briolink/briolink/port/coder"
)

// server answers /api/chat with content as the assistant message.
func server(t *testing.T, status int, content string, seen *chatRequest) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if seen != nil {
			json.NewDecoder(r.Body).Decode(seen)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(chatResponse{Model: "qwen3-coder:30b", Message: message{Role: "assistant", Content: content}})
	}))
	t.Cleanup(s.Close)
	return s
}

const good = `{"files":{"sum.go":"package sum\n"}}`

func TestContract(t *testing.T) {
	s := server(t, 200, good, nil)
	coder.Contract(t, func(*testing.T) coder.Coder { return &Coder{Model: "qwen3-coder:30b", BaseURL: s.URL} },
		coder.Request{Parts: []string{"write package sum"}})
}

func TestRequestKeepsPartsOrdered(t *testing.T) {
	var seen chatRequest
	s := server(t, 200, good, &seen)
	c := &Coder{Model: "m", BaseURL: s.URL + "/"}
	if _, err := c.Code(context.Background(), coder.Request{Parts: []string{"contract", "tests", "failure"}}); err != nil {
		t.Fatal(err)
	}
	if seen.Model != "m" || seen.Stream || len(seen.Messages) != 2 || seen.Messages[0].Role != "system" {
		t.Fatalf("%+v", seen)
	}
	if got := seen.Messages[1].Content; got != "contract\n\ntests\n\nfailure" {
		t.Errorf("user message %q", got)
	}
	if !strings.Contains(string(seen.Format), `"files"`) {
		t.Errorf("format %s", seen.Format)
	}
}

func TestAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		content string
		want    error
	}{
		"not json":      {200, "here is code", ErrAnswer},
		"no files":      {200, `{"files":{}}`, ErrAnswer},
		"absolute path": {200, `{"files":{"/etc/x":"a"}}`, ErrAnswer},
		"dot path":      {200, `{"files":{"./x.go":"a"}}`, ErrAnswer},
		"escaping path": {200, `{"files":{"../x.go":"a"}}`, ErrAnswer},
		"dotdot":        {200, `{"files":{"..":"a"}}`, ErrAnswer},
		"server error":  {500, "", ErrAPI},
	} {
		t.Run(name, func(t *testing.T) {
			s := server(t, tc.status, tc.content, nil)
			_, err := (&Coder{Model: "m", BaseURL: s.URL}).Code(context.Background(), coder.Request{Parts: []string{"x"}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	s := httptest.NewServer(nil)
	s.Close()
	_, err := (&Coder{Model: "m", BaseURL: s.URL}).Code(context.Background(), coder.Request{Parts: []string{"x"}})
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("got %v", err)
	}
}

func TestModelFallsBackToConfig(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"{\"files\":{\"a.go\":\"x\"}}"}}`))
	}))
	defer s.Close()
	resp, err := (&Coder{Model: "cfg", BaseURL: s.URL}).Code(context.Background(), coder.Request{Parts: []string{"x"}})
	if err != nil || resp.Model != "cfg" {
		t.Fatalf("%+v %v", resp, err)
	}
}
