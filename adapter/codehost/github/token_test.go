package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pkcs1(k *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

func pkcs8(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	b, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
}

// fakeGitHub answers installation token requests, checking the JWT it is
// given against pub. Each answer carries a new token valid for an hour.
type fakeGitHub struct {
	srv    *httptest.Server
	calls  atomic.Int32
	status int // 201 when zero
	now    func() time.Time
}

func newFakeGitHub(t *testing.T, pub *rsa.PublicKey, app, inst int64, now func() time.Time) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{now: now}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/"+itoa(inst)+"/access_tokens" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if err := checkJWT(jwt, pub, app, f.now()); err != nil {
			t.Errorf("bad JWT: %v", err)
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_token" + itoa(int64(n)),
			"expires_at": f.now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func checkJWT(jwt string, pub *rsa.PublicKey, app int64, now time.Time) error {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return errors.New("not three parts")
	}
	enc := base64.RawURLEncoding
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return err
	}
	hb, _ := enc.DecodeString(parts[0])
	if string(hb) != `{"alg":"RS256","typ":"JWT"}` {
		return errors.New("header " + string(hb))
	}
	cb, _ := enc.DecodeString(parts[1])
	var c struct {
		Iat, Exp, Iss int64
	}
	if err := json.Unmarshal(cb, &c); err != nil {
		return err
	}
	if c.Iss != app {
		return errors.New("wrong issuer")
	}
	if c.Iat != now.Add(-time.Minute).Unix() || c.Exp != now.Add(9*time.Minute).Unix() {
		return errors.New("wrong times")
	}
	if c.Exp-c.Iat > 600 {
		return errors.New("longer than GitHub allows")
	}
	return nil
}

func source(key []byte, f *fakeGitHub, clock *time.Time) *TokenSource {
	return &TokenSource{
		AppID:          5307371,
		InstallationID: 171151281,
		Key:            func(context.Context) ([]byte, error) { return key, nil },
		BaseURL:        f.srv.URL,
		Now:            func() time.Time { return *clock },
	}
}

func TestTokenMintsWithASignedJWT(t *testing.T) {
	k := testKey(t)
	clock := epoch
	f := newFakeGitHub(t, &k.PublicKey, 5307371, 171151281, func() time.Time { return clock })
	got, err := source(pkcs1(k), f, &clock).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "ghs_token1" {
		t.Errorf("token = %q", got)
	}
}

func TestTokenReadsPKCS8(t *testing.T) {
	k := testKey(t)
	clock := epoch
	f := newFakeGitHub(t, &k.PublicKey, 5307371, 171151281, func() time.Time { return clock })
	if _, err := source(pkcs8(t, k), f, &clock).Token(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTokenIsCachedUntilFiveMinutesBeforeExpiry(t *testing.T) {
	k := testKey(t)
	clock := epoch
	f := newFakeGitHub(t, &k.PublicKey, 5307371, 171151281, func() time.Time { return clock })
	s := source(pkcs1(k), f, &clock)
	ctx := context.Background()
	first, _ := s.Token(ctx)

	clock = epoch.Add(54 * time.Minute) // 6 minutes left: still cached
	if got, _ := s.Token(ctx); got != first || f.calls.Load() != 1 {
		t.Errorf("want the cached token, got %q after %d calls", got, f.calls.Load())
	}

	clock = epoch.Add(56 * time.Minute) // 4 minutes left: replaced
	got, err := s.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got == first || f.calls.Load() != 2 {
		t.Errorf("want a new token, got %q after %d calls", got, f.calls.Load())
	}
}

func TestTokenRefusedByGitHub(t *testing.T) {
	k := testKey(t)
	clock := epoch
	f := newFakeGitHub(t, &k.PublicKey, 5307371, 171151281, func() time.Time { return clock })
	f.status = http.StatusUnauthorized
	_, err := source(pkcs1(k), f, &clock).Token(context.Background())
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "Bad credentials") {
		t.Errorf("error repeats the response body: %v", err)
	}
}

func TestTokenFailureIsNotCached(t *testing.T) {
	k := testKey(t)
	clock := epoch
	f := newFakeGitHub(t, &k.PublicKey, 5307371, 171151281, func() time.Time { return clock })
	f.status = http.StatusInternalServerError
	s := source(pkcs1(k), f, &clock)
	if _, err := s.Token(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	f.status = 0
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatalf("a failure must not stick: %v", err)
	}
}

func TestTokenKeyProblems(t *testing.T) {
	ecdsaLike := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("junk")})
	secret := "SECRET-KEY-MATERIAL"
	for name, key := range map[string][]byte{
		"empty":   nil,
		"not PEM": []byte(secret),
		"not RSA": ecdsaLike,
	} {
		t.Run(name, func(t *testing.T) {
			clock := epoch
			f := newFakeGitHub(t, &testKey(t).PublicKey, 5307371, 171151281, func() time.Time { return clock })
			_, err := source(key, f, &clock).Token(context.Background())
			if !errors.Is(err, ErrKey) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error repeats the key: %v", err)
			}
			if f.calls.Load() != 0 {
				t.Errorf("GitHub was called with an unusable key")
			}
		})
	}
}

func TestTokenKeySourceError(t *testing.T) {
	s := &TokenSource{AppID: 1, InstallationID: 2,
		Key: func(context.Context) ([]byte, error) { return nil, errors.New("vault: not found") }}
	if _, err := s.Token(context.Background()); !errors.Is(err, ErrKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenNeedsIDsAndKeySource(t *testing.T) {
	for name, s := range map[string]*TokenSource{
		"no app":          {InstallationID: 2, Key: func(context.Context) ([]byte, error) { return nil, nil }},
		"no installation": {AppID: 1, Key: func(context.Context) ([]byte, error) { return nil, nil }},
		"no key source":   {AppID: 1, InstallationID: 2},
	} {
		if _, err := s.Token(context.Background()); !errors.Is(err, ErrKey) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTokenUnreadableAnswer(t *testing.T) {
	k := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":""}`))
	}))
	defer srv.Close()
	s := &TokenSource{AppID: 1, InstallationID: 2, BaseURL: srv.URL,
		Key: func(context.Context) ([]byte, error) { return pkcs1(k), nil }}
	if _, err := s.Token(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}
}
