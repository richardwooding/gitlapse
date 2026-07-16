// Package history reads a git repository's commit timeline and per-commit
// file trees without ever checking anything out. It shells out to the system
// git binary (like richardwooding/gitmeta does): `git log` for the commit
// list, `git ls-tree -r` for the tree at a commit, and a persistent
// `git cat-file --batch` process for fast blob reads.
package history

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Commit is one point on the timeline, oldest-first.
type Commit struct {
	SHA     string
	Time    time.Time
	Author  string
	Subject string
}

// FileEntry is one blob in a commit's tree.
type FileEntry struct {
	Path    string
	BlobSHA string
}

// ErrNotARepo is returned by RepoRoot when path is not inside a git work tree.
var ErrNotARepo = errors.New("not a git repository")

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// RepoRoot resolves path to the top level of its git work tree.
func RepoRoot(ctx context.Context, path string) (string, error) {
	out, err := git(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotARepo, path)
	}
	return strings.TrimSpace(string(out)), nil
}

// Commits lists the repository's history oldest-first. With firstParent set,
// side branches of merges are collapsed so the timeline follows the trunk.
func Commits(ctx context.Context, root string, firstParent bool) ([]Commit, error) {
	args := []string{"log", "--reverse", "--format=%H%x1f%ct%x1f%an%x1f%s"}
	if firstParent {
		args = append(args, "--first-parent")
	}
	args = append(args, "HEAD")
	out, err := git(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) != 4 {
			continue
		}
		epoch, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		commits = append(commits, Commit{
			SHA:     parts[0],
			Time:    time.Unix(epoch, 0),
			Author:  parts[2],
			Subject: parts[3],
		})
	}
	if len(commits) == 0 {
		return nil, errors.New("no commits found")
	}
	return commits, nil
}

// Tree lists every blob in the tree of the given commit.
func Tree(ctx context.Context, root, sha string) ([]FileEntry, error) {
	out, err := git(ctx, root, "ls-tree", "-r", "-z", sha)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		// <mode> SP <type> SP <object> TAB <path>
		meta, path, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok {
			continue
		}
		fields := bytes.Fields(meta)
		if len(fields) != 3 || string(fields[1]) != "blob" {
			continue
		}
		entries = append(entries, FileEntry{Path: string(path), BlobSHA: string(fields[2])})
	}
	return entries, nil
}

// BlobReader reads blob contents through one long-lived `git cat-file --batch`
// process, avoiding a subprocess per file. Safe for concurrent use.
type BlobReader struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// NewBlobReader starts the batch process for the repository at root.
func NewBlobReader(ctx context.Context, root string) (*BlobReader, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &BlobReader{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}, nil
}

// Read returns the full content of the blob with the given SHA.
func (b *BlobReader) Read(sha string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := io.WriteString(b.stdin, sha+"\n"); err != nil {
		return nil, err
	}
	header, err := b.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(strings.TrimSpace(header))
	// <sha> SP <type> SP <size>, or "<sha> missing"
	if len(fields) != 3 {
		return nil, fmt.Errorf("blob %s: %s", sha, strings.TrimSpace(header))
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, fmt.Errorf("blob %s: bad size in %q", sha, header)
	}
	buf := make([]byte, size+1) // content + trailing newline
	if _, err := io.ReadFull(b.stdout, buf); err != nil {
		return nil, err
	}
	return buf[:size], nil
}

// Close shuts down the batch process.
func (b *BlobReader) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cerr := b.stdin.Close()
	werr := b.cmd.Wait()
	if werr != nil {
		return werr
	}
	return cerr
}
