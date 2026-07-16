// Package export writes the timelapse headlessly: an asciinema v2 .cast file
// rendered through the same TUI code path as live playback, or an animated
// GIF produced by piping that cast through agg (asciinema gif generator).
package export

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// castHeader is the asciinema v2 header line.
// Spec: https://docs.asciinema.org/manual/asciicast/v2/
type castHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp,omitempty"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// WriteCast writes views (one full-screen render per frame) as an asciinema
// v2 cast playing at fps frames per second.
func WriteCast(w io.Writer, title string, views []string, width, height, fps int, now time.Time) error {
	if len(views) == 0 {
		return fmt.Errorf("no frames to export")
	}
	if fps <= 0 {
		fps = 8
	}
	enc := json.NewEncoder(w)
	header := castHeader{
		Version:   2,
		Width:     width,
		Height:    height,
		Timestamp: now.Unix(),
		Title:     title,
		Env:       map[string]string{"TERM": "xterm-256color", "SHELL": "/bin/sh"},
	}
	if err := enc.Encode(header); err != nil {
		return err
	}
	for i, view := range views {
		t := float64(i) / float64(fps)
		data := "\x1b[H" + terminalize(view)
		if i == 0 {
			// First frame: hide the cursor and clear the screen.
			data = "\x1b[?25l\x1b[2J" + data
		}
		if err := enc.Encode([]any{round(t), "o", data}); err != nil {
			return err
		}
	}
	// Hold the final frame for a beat so players and GIFs don't cut off
	// abruptly, then restore the cursor.
	end := float64(len(views))/float64(fps) + 2
	return enc.Encode([]any{round(end), "o", "\x1b[?25h"})
}

// terminalize prepares a rendered view for raw terminal playback: erase to
// end-of-line before every newline (so shorter lines fully overdraw longer
// predecessors) and use CRLF line endings.
func terminalize(view string) string {
	lines := strings.Split(view, "\n")
	return strings.Join(lines, "\x1b[K\r\n") + "\x1b[K"
}

// round keeps cast timestamps to millisecond precision so the JSON stays tidy.
func round(t float64) float64 {
	return float64(int64(t*1000)) / 1000
}

// GIF converts a cast file to an animated GIF by shelling out to agg.
func GIF(castPath, gifPath string, fps int) error {
	agg, err := exec.LookPath("agg")
	if err != nil {
		return fmt.Errorf("GIF export needs agg (asciinema gif generator) on PATH — install with `brew install agg` or see https://github.com/asciinema/agg")
	}
	cmd := exec.Command(agg, "--fps-cap", fmt.Sprint(fps), castPath, gifPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
