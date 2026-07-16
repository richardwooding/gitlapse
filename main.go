// gitlapse scrubs through a git repository's history like a video, animating
// per-function complexity metrics (via richardwooding/codemetrics) so you can
// watch hotspots ignite and cool as the codebase evolves. Nothing is ever
// checked out: historical file contents are read straight from the object
// database.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/richardwooding/gitlapse/internal/engine"
	"github.com/richardwooding/gitlapse/internal/history"
	"github.com/richardwooding/gitlapse/internal/tui"
)

// version is overridden at build time via -ldflags.
var version = "dev"

var cli struct {
	Path        string           `arg:"" optional:"" default:"." help:"Path inside the git repository to replay."`
	MaxFrames   int              `default:"240" help:"Sample the timeline down to at most this many commits."`
	Hotspots    int              `default:"15" help:"Hotspot functions tracked per frame."`
	FirstParent bool             `default:"true" negatable:"" help:"Follow only the first parent of merges."`
	Dump        bool             `help:"No TUI: print one TSV line of metrics per frame to stdout."`
	Version     kong.VersionFlag `help:"Print the version and exit."`
}

func main() {
	kong.Parse(&cli,
		kong.Name("gitlapse"),
		kong.Description("Replay a repository's complexity history as a terminal timelapse."),
		kong.Vars{"version": version})

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitlapse:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root, err := history.RepoRoot(ctx, cli.Path)
	if err != nil {
		return err
	}
	commits, err := history.Commits(ctx, root, cli.FirstParent)
	if err != nil {
		return err
	}

	opts := engine.Defaults()
	opts.MaxFrames = cli.MaxFrames
	opts.HotspotCount = cli.Hotspots
	sampled := engine.Sample(commits, opts.MaxFrames)

	frames, err := engine.Run(ctx, root, sampled, opts)
	if err != nil {
		return err
	}

	if cli.Dump {
		return dump(frames)
	}

	model := tui.New(filepath.Base(root), len(sampled), frames)
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

func dump(frames <-chan engine.Frame) error {
	fmt.Println("sha\tdate\tfiles\tfunctions\tcognitive\tmax_cognitive\tcyclomatic")
	for f := range frames {
		if f.Err != nil {
			fmt.Fprintf(os.Stderr, "gitlapse: frame %d (%s): %v\n", f.Index, f.Commit.SHA, f.Err)
			continue
		}
		fmt.Printf("%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
			f.Commit.SHA[:7], f.Commit.Time.Format("2006-01-02"),
			f.Files, f.Functions, f.TotalCog, f.MaxCog, f.TotalCyc)
	}
	return nil
}
