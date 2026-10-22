package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
)

// Command runs a program and returns what it printed, standard output and
// error together, and its exit status. The error is for a program that could
// not be started.
type Command func(ctx context.Context, argv []string) (out []byte, code int, err error)

func execCommand(ctx context.Context, argv []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return buf.Bytes(), ee.ExitCode(), nil
	}
	return buf.Bytes(), 0, err
}

// Container is the Workspaces of the offline build worker (issue #4): a git
// worktree per task, its commands run in a container with no network
// (runner/container/README.md). Files are read and written on the host, in
// the worktree.
type Container struct {
	Repo  string // the project's git repository
	Base  string // the ref to branch worktrees from; HEAD when empty
	Dir   string // where worktrees go; the system temp directory when empty
	Image string // the build worker image, built from runner/container/Containerfile

	// Optional host directories, all mounted into the container.
	Tools    string // read-only: the prebuilt generate and mutate binaries, put first on PATH
	ModCache string // read-only: the pre-seeded module cache (seed-modcache.sh)
	Cache    string // read-write: the build cache

	// Push publishes a pass: the remote to push to ("origin" when empty) and
	// the identity of the commit (git's own config when empty).
	Remote   string
	GitName  string
	GitEmail string

	Engine string  // the container engine; "docker" when empty
	User   string  // uid:gid the container runs as; the current user's when empty
	Cmd    Command // runs host commands; os/exec when nil
}

func (c *Container) run(ctx context.Context, argv ...string) ([]byte, int, error) {
	if c.Cmd != nil {
		return c.Cmd(ctx, argv)
	}
	return execCommand(ctx, argv)
}

// Create adds a detached worktree named for the task.
func (c *Container) Create(ctx context.Context, name string) (Workspace, error) {
	parent := c.Dir
	if parent == "" {
		parent = os.TempDir()
	}
	dir, err := os.MkdirTemp(parent, "briolink-"+name+"-")
	if err != nil {
		return nil, err
	}
	base := c.Base
	if base == "" {
		base = "HEAD"
	}
	out, code, err := c.run(ctx, "git", "-C", c.Repo, "worktree", "add", "--detach", dir, base)
	if err != nil || code != 0 {
		os.Remove(dir)
		return nil, fmt.Errorf("git worktree add: %v: %s", errOr(err, code), out)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &worktree{c: c, dir: dir, root: root}, nil
}

func errOr(err error, code int) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("exit status %d", code)
}

type worktree struct {
	c    *Container
	dir  string
	root *os.Root
}

// dockerArgs is the command that runs argv in the worker image: no network,
// the worktree at /workspace, the module cache offline (GOPROXY=off).
func (w *worktree) dockerArgs(argv []string) []string {
	c := w.c
	engine := c.Engine
	if engine == "" {
		engine = "docker"
	}
	user := c.User
	if user == "" {
		user = strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	}
	a := []string{engine, "run", "--rm", "--network", "none", "--user", user,
		"-v", w.dir + ":/workspace", "-w", "/workspace",
		"-e", "HOME=/tmp", "-e", "GOPROXY=off",
		"-e", "PATH=/briolink/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"}
	if c.Tools != "" {
		a = append(a, "-v", c.Tools+":/briolink/bin:ro")
	}
	if c.ModCache != "" {
		a = append(a, "-v", c.ModCache+":/go/pkg/mod:ro")
	}
	if c.Cache != "" {
		a = append(a, "-v", c.Cache+":/tmp/gocache", "-e", "GOCACHE=/tmp/gocache")
	} else {
		a = append(a, "-e", "GOCACHE=/tmp/gocache")
	}
	a = append(a, c.Image)
	return append(a, argv...)
}

func (w *worktree) Exec(ctx context.Context, argv []string) (Output, error) {
	out, code, err := w.c.run(ctx, w.dockerArgs(argv)...)
	if err != nil {
		return Output{}, err
	}
	return Output{Code: code, Text: string(out)}, nil
}

func (w *worktree) Read(p string) ([]byte, error) { return w.root.ReadFile(filepath.FromSlash(p)) }

func (w *worktree) Write(p string, data []byte) error {
	p = filepath.FromSlash(p)
	if d := filepath.Dir(p); d != "." {
		if err := w.root.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return w.root.WriteFile(p, data, 0o644)
}

func (w *worktree) Remove(p string) error {
	if err := w.root.Remove(filepath.FromSlash(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (w *worktree) List(dir string) ([]string, error) {
	f, err := w.root.Open(filepath.FromSlash(path.Clean(dir)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Push commits paths in the worktree and pushes the commit as branch of the
// remote, replacing a branch of that name an earlier run left. It runs on the
// host, not in the container: it is the one step that needs the network.
func (w *worktree) Push(ctx context.Context, branch, message string, paths []string) error {
	c := w.c
	remote := c.Remote
	if remote == "" {
		remote = "origin"
	}
	git := []string{"git", "-C", w.dir}
	if c.GitName != "" {
		git = append(git, "-c", "user.name="+c.GitName)
	}
	if c.GitEmail != "" {
		git = append(git, "-c", "user.email="+c.GitEmail)
	}
	steps := [][]string{
		append([]string{"add", "-A", "--"}, paths...),
		{"commit", "-q", "-m", message},
		{"push", "-q", "--force", remote, "HEAD:refs/heads/" + branch},
	}
	for _, step := range steps {
		out, code, err := c.run(ctx, append(append([]string(nil), git...), step...)...)
		if err != nil || code != 0 {
			return fmt.Errorf("git %s: %v: %s", step[0], errOr(err, code), out)
		}
	}
	return nil
}

func (w *worktree) Close() error {
	w.root.Close()
	out, code, err := w.c.run(context.Background(), "git", "-C", w.c.Repo, "worktree", "remove", "--force", w.dir)
	if err != nil || code != 0 {
		return fmt.Errorf("git worktree remove: %v: %s", errOr(err, code), out)
	}
	return nil
}
