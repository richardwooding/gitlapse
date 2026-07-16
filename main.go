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
	"time"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/richardwooding/gitlapse/internal/engine"
	"github.com/richardwooding/gitlapse/internal/export"
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
	Export      string           `enum:",cast,gif" default:"" help:"No TUI: export the timelapse as an asciinema cast or an animated GIF (GIF needs agg on PATH)."`
	Out         string           `help:"Output path for --export (default gitlapse.cast / gitlapse.gif)."`
	Fps         int              `default:"8" help:"Playback speed for --export, frames per second."`
	Width       int              `default:"100" help:"Terminal width for --export."`
	Height      int              `default:"30" help:"Terminal height for --export."`
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
	if cli.Export != "" {
		return runExport(filepath.Base(root), frames, len(sampled))
	}

	model := tui.New(filepath.Base(root), len(sampled), frames)
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

// runExport drains the engine, renders every frame off-screen, and writes a
// cast file — then converts it to a GIF via agg when requested.
func runExport(repoName string, frames <-chan engine.Frame, total int) error {
	tui.ForceColors()
	var all []engine.Frame
	for f := range frames {
		all = append(all, f)
		if len(all)%20 == 0 || len(all) == total {
			fmt.Fprintf(os.Stderr, "gitlapse: computed %d/%d frames\n", len(all), total)
		}
	}
	views := tui.RenderFrames(repoName, all, cli.Width, cli.Height, cli.Fps)

	castPath := cli.Out
	if cli.Export == "gif" {
		gifPath := cli.Out
		if gifPath == "" {
			gifPath = "gitlapse.gif"
		}
		tmp, err := os.CreateTemp("", "gitlapse-*.cast")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if err := writeCastFile(tmp.Name(), repoName, views); err != nil {
			return err
		}
		if err := export.GIF(tmp.Name(), gifPath, cli.Fps); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "gitlapse: wrote", gifPath)
		return nil
	}
	if castPath == "" {
		castPath = "gitlapse.cast"
	}
	if err := writeCastFile(castPath, repoName, views); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "gitlapse: wrote", castPath)
	return nil
}

func writeCastFile(path, repoName string, views []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	title := "gitlapse — " + repoName
	if err := export.WriteCast(f, title, views, cli.Width, cli.Height, cli.Fps, time.Now()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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
