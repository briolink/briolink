package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/briolink/briolink/adapter/codehost/github"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/port/codehost"
)

// botLogin is the account GitHub shows for the App's writes.
const botLogin = "briolink-bot[bot]"

// TestGitHubLive runs the CodeHost contract against GitHub itself, wired as
// the service wires it: the bot's key comes from the installation's secret
// store (each mint audited), and every write is the bot's. Each case gets its
// own private throwaway repository in the github section's owner, made with
// the founder's token, since the bot has no administration permission (A22).
// After each case it checks that every issue, pull request, comment and
// milestone in it was authored by the bot.
//
// Skipped unless BRIOLINK_LIVE_GITHUB names an operator.json with a github
// section (CREDENTIALS_DIRECTORY set as for the service) and
// BRIOLINK_LIVE_FOUNDER_TOKEN holds a founder token with the repo scope. The
// repositories are deleted only with BRIOLINK_LIVE_DELETE=1 (a token with
// delete_repo); otherwise their names are logged for deletion by hand.
func TestGitHubLive(t *testing.T) {
	cfgPath, founder := os.Getenv("BRIOLINK_LIVE_GITHUB"), os.Getenv("BRIOLINK_LIVE_FOUNDER_TOKEN")
	if cfgPath == "" || founder == "" {
		t.Skip("BRIOLINK_LIVE_GITHUB and BRIOLINK_LIVE_FOUNDER_TOKEN are not both set")
	}
	ctx := context.Background()
	cfg, err := operator.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub == nil {
		t.Fatal(cfgPath + ": no github section")
	}
	owner, _, _ := strings.Cut(cfg.GitHub.Repo, "/")
	key, err := loadKey(os.Getenv, cfg.MasterKeyCredential)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, cfg.Database, events.New(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	vlt, err := vault.New(db.DB(), key, db, server.Roles, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	n := 0

	codehost.Contract(t, func(t *testing.T) codehost.Host {
		n++
		name := fmt.Sprintf("codehost-contract-%s-%02d", stamp, n)
		repo := owner + "/" + name
		err := api(ctx, founder, http.MethodPost, "/orgs/"+owner+"/repos", map[string]any{
			"name": name, "private": true, "auto_init": true,
			"description": "Throwaway: the CodeHost contract, " + t.Name(),
		}, nil)
		if err != nil {
			t.Fatalf("create %s: %v", repo, err)
		}
		t.Cleanup(func() { finish(t, founder, repo) })

		c := *cfg
		g := *cfg.GitHub
		g.Repo = repo
		c.GitHub = &g
		h, err := newGitHub(deps{cfg: &c, secret: vlt.Use})
		if err != nil {
			t.Fatal(err)
		}
		gh := h.(*github.Host)
		// The repository takes a moment to reach the installation and
		// to have its first commit.
		for i := 0; ; i++ {
			tok, err := gh.Tokens.Token(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = api(ctx, tok, http.MethodGet, "/repos/"+repo+"/git/ref/heads/main", nil, nil); err == nil {
				break
			} else if i == 30 {
				t.Fatalf("%s never reached the bot: %v", repo, err)
			}
			time.Sleep(time.Second)
		}
		return committing{gh}
	})
}

// committing pushes a commit onto every branch it creates, as the runner
// does before it opens a pull request: GitHub refuses one whose head has
// nothing its base lacks, and the port has no commit of its own.
type committing struct{ *github.Host }

func (c committing) CreateBranch(ctx context.Context, name, from string) error {
	if err := c.Host.CreateBranch(ctx, name, from); err != nil {
		return err
	}
	tok, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	return api(ctx, tok, http.MethodPut, "/repos/"+c.Repo+"/contents/contract.txt", map[string]any{
		"message": "contract: a commit on " + name,
		"content": base64.StdEncoding.EncodeToString([]byte(name + "\n")),
		"branch":  name,
	}, nil)
}

// finish checks that the bot wrote everything in repo, then deletes it or
// logs its name.
func finish(t *testing.T, founder, repo string) {
	ctx := context.Background()
	var writes []string
	must := func(err error) {
		if err != nil {
			t.Errorf("author check on %s: %v", repo, err)
		}
	}
	// Issues and pull requests are read one by one, 1 up to the first
	// missing number: GitHub's issue list lags writes by seconds.
	for n := 1; ; n++ {
		var is struct {
			Number int
			User   struct{ Login string }
		}
		if err := api(ctx, founder, http.MethodGet, fmt.Sprintf("/repos/%s/issues/%d", repo, n), nil, &is); err != nil {
			if !strings.Contains(err.Error(), "status 404") {
				must(err)
			}
			break
		}
		writes = append(writes, fmt.Sprintf("#%d by %s", is.Number, is.User.Login))
		var cs []struct{ User struct{ Login string } }
		must(api(ctx, founder, http.MethodGet, fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, is.Number), nil, &cs))
		for _, c := range cs {
			writes = append(writes, fmt.Sprintf("comment on #%d by %s", is.Number, c.User.Login))
		}
	}
	var ms []struct {
		Title   string
		Creator struct{ Login string }
	}
	must(api(ctx, founder, http.MethodGet, "/repos/"+repo+"/milestones?state=all&per_page=100", nil, &ms))
	for _, m := range ms {
		writes = append(writes, fmt.Sprintf("milestone %s by %s", m.Title, m.Creator.Login))
	}
	var commits []struct {
		Commit struct{ Message string }
		Author *struct{ Login string }
	}
	var branches []struct{ Name string }
	must(api(ctx, founder, http.MethodGet, "/repos/"+repo+"/branches?per_page=100", nil, &branches))
	for _, b := range branches {
		if b.Name == "main" {
			continue // its first commit is the founder's, made with the repository
		}
		commits = commits[:0]
		must(api(ctx, founder, http.MethodGet, "/repos/"+repo+"/commits?per_page=1&sha="+b.Name, nil, &commits))
		for _, c := range commits {
			login := "unknown"
			if c.Author != nil {
				login = c.Author.Login
			}
			writes = append(writes, fmt.Sprintf("commit on %s by %s", b.Name, login))
		}
	}
	for _, w := range writes {
		if !strings.HasSuffix(w, " by "+botLogin) {
			t.Errorf("%s: %s", repo, w)
		}
	}
	t.Logf("%s: %d writes checked, all by %s", repo, len(writes), botLogin)

	if os.Getenv("BRIOLINK_LIVE_DELETE") != "1" {
		t.Logf("left for deletion by hand: %s", repo)
		return
	}
	if err := api(ctx, founder, http.MethodDelete, "/repos/"+repo, nil, nil); err != nil {
		t.Logf("not deleted, delete by hand: %s (%v)", repo, err)
	}
}

// api calls GitHub with a token, outside the adapter: making and checking
// the throwaway repositories is not the port's job.
func api(ctx context.Context, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, github.DefaultBaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		var e struct{ Message string }
		json.Unmarshal(data, &e)
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, e.Message)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
