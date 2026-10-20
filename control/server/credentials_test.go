package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/vault"
)

// secretValue is a secret every test sends and no answer or audit line may repeat.
const secretValue = "s3cr3t-VALUE-9f2c"

// seen collects every answer a test received, to look for what must not be in any.
type seen []string

func (r rig) send(t *testing.T, got *seen, method, path, bearer string, body any) (int, string) {
	t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, r.url+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if got != nil {
		*got = append(*got, resp.Status+"\n"+resp.Header.Get("Location")+"\n"+string(b))
	}
	return resp.StatusCode, string(b)
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not a JSON object: %q", body)
	}
	return out
}

func names(t *testing.T, body, key string) []string {
	t.Helper()
	var out []string
	for _, c := range decode(t, body)[key].([]any) {
		out = append(out, c.(map[string]any)["name"].(string))
	}
	return out
}

func TestSecretRoutes(t *testing.T) {
	r := newRig(t, 0)
	ctx := context.Background()
	var got seen
	expires := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	remind := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	if s, b := r.send(t, &got, "POST", "/api/v1/secrets", r.token, map[string]any{"name": "MIRROR_GH_TOKEN", "value": secretValue, "expires_on": expires, "remind_on": remind}); s != 204 || b != "" {
		t.Fatalf("put: %d %q", s, b)
	}
	// Stored: the value is what Use opens, under the name.
	if v, err := r.vault.Use(ctx, "test", "MIRROR_GH_TOKEN"); err != nil || string(v) != secretValue {
		t.Fatalf("stored value = %q, %v", v, err)
	}
	if s, b := r.send(t, &got, "POST", "/api/v1/secrets", r.token, map[string]any{"name": "MIRROR_GH_TOKEN", "value": "other"}); s != 409 {
		t.Errorf("put again: %d %s", s, b)
	}
	if s, b := r.send(t, &got, "POST", "/api/v1/secrets", r.token, map[string]any{"name": "NO_DATES", "value": secretValue}); s != 204 {
		t.Fatalf("put without dates: %d %s", s, b)
	}

	s, b := r.send(t, &got, "GET", "/api/v1/secrets", r.token, nil)
	if s != 200 || strings.Join(names(t, b, "secrets"), ",") != "MIRROR_GH_TOKEN,NO_DATES" {
		t.Fatalf("list: %d %s", s, b)
	}
	first := decode(t, b)["secrets"].([]any)[0].(map[string]any)
	if first["kind"] != "secret" || first["expires_on"] != expires || first["remind_on"] != remind || first["revoked"] != false || first["created_at"] == "" {
		t.Errorf("entry = %v", first)
	}
	// The rig's own token is a token, not a secret.
	if strings.Contains(b, "rig") {
		t.Errorf("a token in the list of secrets: %s", b)
	}
	s, b = r.send(t, &got, "GET", "/api/v1/secrets?expiring=true", r.token, nil)
	if s != 200 || strings.Join(names(t, b, "secrets"), ",") != "MIRROR_GH_TOKEN" {
		t.Errorf("expiring: %d %s", s, b)
	}

	newExpiry := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	if s, b := r.send(t, &got, "PUT", "/api/v1/secrets/MIRROR_GH_TOKEN", r.token, map[string]any{"value": "rotated-VALUE-77", "expires_on": newExpiry}); s != 204 || b != "" {
		t.Fatalf("rotate: %d %q", s, b)
	}
	if v, _ := r.vault.Use(ctx, "test", "MIRROR_GH_TOKEN"); string(v) != "rotated-VALUE-77" {
		t.Errorf("rotated value = %q", v)
	}
	_, b = r.send(t, &got, "GET", "/api/v1/secrets", r.token, nil)
	if e := decode(t, b)["secrets"].([]any)[0].(map[string]any); e["expires_on"] != newExpiry || e["remind_on"] != "" || e["rotated_at"] == "" {
		t.Errorf("after rotation: %v", e)
	}
	if s, _ := r.send(t, &got, "PUT", "/api/v1/secrets/NOPE", r.token, map[string]any{"value": "v"}); s != 404 {
		t.Errorf("rotate unknown: %d", s)
	}

	if s, b := r.send(t, &got, "DELETE", "/api/v1/secrets/MIRROR_GH_TOKEN", r.token, nil); s != 204 || b != "" {
		t.Fatalf("delete: %d %q", s, b)
	}
	if _, err := r.vault.Use(ctx, "test", "MIRROR_GH_TOKEN"); err == nil {
		t.Error("the secret is still there")
	}
	if s, _ := r.send(t, &got, "DELETE", "/api/v1/secrets/MIRROR_GH_TOKEN", r.token, nil); s != 404 {
		t.Errorf("delete again: %d", s)
	}

	// Refused before anything is written.
	for name, body := range map[string]any{
		"a date that is not one":      map[string]any{"name": "X1", "value": secretValue, "expires_on": "2027-02-31"},
		"the wrong date shape":        map[string]any{"name": "X2", "value": secretValue, "expires_on": "tomorrow!!"},
		"a reminder after the expiry": map[string]any{"name": "X3", "value": secretValue, "expires_on": "2027-01-01", "remind_on": "2027-02-01"},
		"a name with a slash":         map[string]any{"name": "a/b", "value": secretValue},
		"no value":                    map[string]any{"name": "X4"},
		"an empty value":              map[string]any{"name": "X5", "value": ""},
		"an unknown field":            map[string]any{"name": "X6", "value": secretValue, "password": secretValue},
		"not an object":               `["` + secretValue + `"]`,
		"not JSON":                    secretValue,
	} {
		if s, b := r.send(t, &got, "POST", "/api/v1/secrets", r.token, body); s < 400 || s >= 500 {
			t.Errorf("%s: %d %s", name, s, b)
		}
	}
	_, b = r.send(t, nil, "GET", "/api/v1/secrets", r.token, nil)
	if strings.Join(names(t, b, "secrets"), ",") != "NO_DATES" {
		t.Errorf("a refused request wrote: %s", b)
	}
	// Without a JSON content type nothing is read.
	req, _ := http.NewRequest("POST", r.url+"/api/v1/secrets", strings.NewReader(`{"name":"X7","value":"`+secretValue+`"}`))
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	got = append(got, string(body))
	if resp.StatusCode != 415 {
		t.Errorf("no content type: %d", resp.StatusCode)
	}

	for _, answer := range got {
		for _, value := range []string{secretValue, "rotated-VALUE-77"} {
			if strings.Contains(answer, value) {
				t.Errorf("an answer holds a secret value:\n%s", answer)
			}
		}
	}
	_, b = r.send(t, nil, "GET", "/api/v1/audit?limit=200", r.token, nil)
	if strings.Contains(b, secretValue) || strings.Contains(b, "rotated-VALUE-77") {
		t.Errorf("the audit log holds a secret value: %s", b)
	}
}

func TestTokenRoutes(t *testing.T) {
	r := newRig(t, 0)
	var got seen
	expires := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")

	s, b := r.send(t, &got, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "ci", "role": server.Role, "expires_on": expires})
	if s != 201 {
		t.Fatalf("issue: %d %s", s, b)
	}
	issued, _ := decode(t, b)["token"].(string)
	if !strings.HasPrefix(issued, "blk_") {
		t.Fatalf("issue answered %s", b)
	}
	// The new token works, as the role it was issued for.
	if s, _ := r.send(t, nil, "GET", "/api/v1/routing", issued, nil); s != 200 {
		t.Errorf("the issued token: %d", s)
	}
	if s, _ := r.send(t, &got, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "ci", "role": server.Role}); s != 409 {
		t.Errorf("issue again: %d", s)
	}
	if s, _ := r.send(t, &got, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "ghost", "role": "nobody"}); s != 422 {
		t.Errorf("issue for an unknown role: %d", s)
	}
	if s, _ := r.send(t, &got, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "late", "role": server.Role, "expires_on": "2027-13-40"}); s != 422 {
		t.Errorf("issue with a bad date: %d", s)
	}
	if s, _ := r.send(t, &got, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "eye", "role": "viewer"}); s != 201 {
		t.Errorf("issue for another declared role: %d", s)
	}

	s, b = r.send(t, &got, "GET", "/api/v1/tokens", r.token, nil)
	if s != 200 || strings.Join(names(t, b, "tokens"), ",") != "ci,eye,rig" {
		t.Fatalf("list: %d %s", s, b)
	}
	if strings.Contains(b, issued) {
		t.Errorf("the list holds an issued token")
	}
	for _, c := range decode(t, b)["tokens"].([]any) {
		if e := c.(map[string]any); e["name"] == "ci" && (e["kind"] != "token" || e["role"] != server.Role || e["expires_on"] != expires || e["revoked"] != false) {
			t.Errorf("entry = %v", e)
		}
	}
	if _, b := r.send(t, &got, "GET", "/api/v1/tokens?expiring=true", r.token, nil); strings.Join(names(t, b, "tokens"), ",") != "ci" {
		t.Errorf("expiring: %s", b)
	}

	if s, b := r.send(t, &got, "POST", "/api/v1/tokens/ci/revoke", r.token, nil); s != 204 || b != "" {
		t.Fatalf("revoke: %d %q", s, b)
	}
	if s, _ := r.send(t, nil, "GET", "/api/v1/routing", issued, nil); s != 401 {
		t.Errorf("the revoked token: %d", s)
	}
	if s, _ := r.send(t, &got, "POST", "/api/v1/tokens/nobody/revoke", r.token, nil); s != 404 {
		t.Errorf("revoke unknown: %d", s)
	}
	_, b = r.send(t, &got, "GET", "/api/v1/tokens", r.token, nil)
	for _, c := range decode(t, b)["tokens"].([]any) {
		if e := c.(map[string]any); e["name"] == "ci" && e["revoked"] != true {
			t.Errorf("a revoked token is not marked: %v", e)
		}
	}

	// Only the answer that issued a token holds it.
	for i, answer := range got {
		if strings.Contains(answer, issued) != (i == 0) {
			t.Errorf("answer %d holds the issued token: %v\n%s", i, i != 0, answer)
		}
	}
	_, b = r.send(t, nil, "GET", "/api/v1/audit?limit=200", r.token, nil)
	if strings.Contains(b, issued) {
		t.Errorf("the audit log holds an issued token")
	}
}

func TestCredentialRoutesAreAudited(t *testing.T) {
	r := newRig(t, 0)
	r.send(t, nil, "POST", "/api/v1/secrets", r.token, map[string]any{"name": "A", "value": secretValue})
	r.send(t, nil, "PUT", "/api/v1/secrets/A", r.token, map[string]any{"value": secretValue + "2"})
	r.send(t, nil, "DELETE", "/api/v1/secrets/A", r.token, nil)
	r.send(t, nil, "POST", "/api/v1/tokens", r.token, map[string]any{"name": "T", "role": server.Role})
	r.send(t, nil, "POST", "/api/v1/tokens/T/revoke", r.token, nil)
	r.send(t, nil, "DELETE", "/api/v1/secrets/A", r.token, nil) // refused: leaves no change line

	log, err := r.store.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var changes []string
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		if !strings.HasPrefix(e.Actor, "token:") {
			continue
		}
		if strings.HasPrefix(e.Action, "secret.") || strings.HasPrefix(e.Action, "token.create") || e.Action == "token.revoke" {
			changes = append(changes, e.Actor+" "+e.Action+" "+e.Subject)
		}
	}
	want := "token:rig secret.put A,token:rig secret.rotate A,token:rig secret.delete A,token:rig token.create T,token:rig token.revoke T"
	if strings.Join(changes, ",") != want {
		t.Errorf("changes = %q\nwant      %q", strings.Join(changes, ","), want)
	}
}

type route struct{ method, path string }

var credentialRoutes = []route{
	{"GET", "/api/v1/secrets"}, {"POST", "/api/v1/secrets"}, {"PUT", "/api/v1/secrets/KEEP"}, {"DELETE", "/api/v1/secrets/KEEP"},
	{"GET", "/api/v1/tokens"}, {"POST", "/api/v1/tokens"}, {"POST", "/api/v1/tokens/rig/revoke"},
}

func credentialBody(rt route) any {
	switch {
	case rt.method == "POST" && rt.path == "/api/v1/secrets":
		return map[string]any{"name": "BLOCKED", "value": secretValue}
	case rt.method == "PUT":
		return map[string]any{"value": secretValue}
	case rt.method == "POST" && rt.path == "/api/v1/tokens":
		return map[string]any{"name": "blocked", "role": server.Role}
	}
	return nil
}

func TestCredentialRoutesNeedTheOperatorRole(t *testing.T) {
	r := newRig(t, 0)
	ctx := context.Background()
	if err := r.vault.Put(ctx, "test", "KEEP", []byte(secretValue), vault.Dates{}); err != nil {
		t.Fatal(err)
	}
	other, err := r.vault.CreateToken(ctx, "test", "viewer-tok", "viewer", vault.Dates{})
	if err != nil {
		t.Fatal(err)
	}
	gone, _ := r.vault.CreateToken(ctx, "test", "gone-tok", server.Role, vault.Dates{})
	r.vault.RevokeToken(ctx, "test", "gone-tok")
	old, _ := r.vault.CreateToken(ctx, "test", "old-tok", server.Role, vault.Dates{ExpiresOn: "2026-10-16"})

	for _, rt := range credentialRoutes {
		for name, tc := range map[string]struct {
			bearer string
			want   int
		}{"no token": {"", 401}, "an unknown token": {"blk_unknown", 401}, "a token of another role": {other, 403}, "a revoked token": {gone, 401}, "an expired token": {old, 401}} {
			var got seen
			s, b := r.send(t, &got, rt.method, rt.path, tc.bearer, credentialBody(rt))
			if s != tc.want {
				t.Errorf("%s %s with %s: %d %s, want %d", rt.method, rt.path, name, s, b, tc.want)
			}
			if strings.Contains(b, secretValue) {
				t.Errorf("%s %s with %s repeats the value: %s", rt.method, rt.path, name, b)
			}
		}
	}
	// None of them changed anything.
	list, err := r.vault.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Name == "BLOCKED" || e.Name == "blocked" || (e.Name == "rig" && e.Revoked) {
			t.Errorf("a refused request changed %+v", e)
		}
	}
	if v, err := r.vault.Use(ctx, "test", "KEEP"); err != nil || string(v) != secretValue {
		t.Errorf("KEEP = %q, %v", v, err)
	}
}
