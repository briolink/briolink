// Package github is the GitHub binding of the CodeHost port. This file is its
// authentication: briolink-bot is a GitHub App (A22), and every write the
// bot makes carries a one-hour installation token minted from the App's
// private key. TokenSource mints and caches it; the key is read through Key
// on each mint (so the secret store records every use) and never kept.
//
// No error produced here holds the key, a signed JWT or a token.
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
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the GitHub REST API.
const DefaultBaseURL = "https://api.github.com"

// refreshBefore is how long before expiry a cached token is replaced, so a
// request started with it does not run into its end.
const refreshBefore = 5 * time.Minute

// jwtLifetime is the App JWT's validity; GitHub allows at most 10 minutes.
const jwtLifetime = 9 * time.Minute

// jwtBackdate covers clock drift between this host and GitHub.
const jwtBackdate = time.Minute

var (
	// ErrKey: the stored key is missing or is not an RSA private key in PEM.
	ErrKey = errors.New("github: the App key is unusable")
	// ErrRefused: GitHub refused to mint a token (wrong App or installation
	// ID, a key that is not the App's, a revoked installation).
	ErrRefused = errors.New("github: token request refused")
)

// TokenSource mints installation tokens for one App installation.
type TokenSource struct {
	AppID          int64
	InstallationID int64
	// Key returns the App's private key, PEM encoded (PKCS#1 or PKCS#8). In
	// the control plane it is the secret store's Use.
	Key func(ctx context.Context) ([]byte, error)

	BaseURL string           // DefaultBaseURL when empty
	Client  *http.Client     // http.DefaultClient when nil
	Now     func() time.Time // time.Now when nil

	mu      sync.Mutex
	token   string
	expires time.Time
}

func (s *TokenSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Token returns a valid installation token, minting a new one when there is
// none or the cached one ends within five minutes.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Add(refreshBefore).Before(s.expires) {
		return s.token, nil
	}
	tok, exp, err := s.mint(ctx)
	if err != nil {
		return "", err
	}
	s.token, s.expires = tok, exp
	return tok, nil
}

func (s *TokenSource) mint(ctx context.Context) (string, time.Time, error) {
	if s.AppID <= 0 || s.InstallationID <= 0 || s.Key == nil {
		return "", time.Time{}, fmt.Errorf("%w: App ID, installation ID and key source are required", ErrKey)
	}
	pemKey, err := s.Key(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %v", ErrKey, err)
	}
	key, err := parseKey(pemKey)
	if err != nil {
		return "", time.Time{}, err
	}
	jwt, err := signJWT(key, s.AppID, s.now())
	if err != nil {
		return "", time.Time{}, err
	}

	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", base, s.InstallationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: token response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("%w: status %d", ErrRefused, resp.StatusCode)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" || out.ExpiresAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("%w: unreadable answer", ErrRefused)
	}
	return out.Token, out.ExpiresAt, nil
}

// parseKey reads an RSA private key from PEM, PKCS#1 or PKCS#8.
func parseKey(p []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(p)
	if block == nil {
		return nil, fmt.Errorf("%w: not PEM", ErrKey)
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
	}
	return nil, fmt.Errorf("%w: not an RSA private key", ErrKey)
}

// signJWT builds the App's RS256 JWT: issued a minute in the past, valid nine
// minutes, issuer the App ID.
func signJWT(key *rsa.PrivateKey, appID int64, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtBackdate).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("%w: signing: %v", ErrKey, err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}
