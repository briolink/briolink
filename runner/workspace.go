package runner

import "context"

// Workspaces creates one isolated workspace per task: a git worktree whose
// commands run with no network (docs/2-architecture.md § Runner, step 1).
type Workspaces interface {
	Create(ctx context.Context, name string) (Workspace, error)
}

// Workspace is one task's checkout. Paths are slash-separated and relative to
// its root; a path that leaves the root is an error.
type Workspace interface {
	// Read returns a file's content; fs.ErrNotExist when there is none.
	Read(path string) ([]byte, error)
	// Write replaces a file, creating its directories.
	Write(path string, data []byte) error
	// Remove deletes a file; a missing one is not an error.
	Remove(path string) error
	// List returns the sorted names of the regular files directly in dir.
	List(dir string) ([]string, error)
	// Exec runs argv at the workspace root, without a shell and without
	// network. The error is for a command that could not be run at all; a
	// command that ran and failed is a non-zero Output.Code.
	Exec(ctx context.Context, argv []string) (Output, error)
	// Close removes the workspace.
	Close() error
}

// Output is what a command printed (standard output and error together) and
// the status it exited with.
type Output struct {
	Code int
	Text string
}
