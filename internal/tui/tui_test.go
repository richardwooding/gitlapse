package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/richardwooding/gitlapse/internal/engine"
	"github.com/richardwooding/gitlapse/internal/history"
)

func testFrames(n int) []engine.Frame {
	frames := make([]engine.Frame, n)
	for i := range frames {
		frames[i] = engine.Frame{
			Index: i,
			Commit: history.Commit{
				SHA:     strings.Repeat("a", 40),
				Time:    time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC),
				Author:  "Richard Wooding",
				Subject: "commit subject",
			},
			Files:     3,
			Functions: 10 + i,
			TotalCog:  50 + i*7,
			MaxCog:    12,
			TotalCyc:  40 + i*5,
			Hotspots: []engine.Hotspot{
				{File: "a.go", Function: "Big", StartLine: 10, Cognitive: 12, Cyclomatic: 9, Delta: 2},
				{File: "b.go", Function: "Small", StartLine: 4, Cognitive: 3, Cyclomatic: 2, New: true},
			},
		}
	}
	return frames
}

func modelWith(t *testing.T, n int) Model {
	t.Helper()
	m := New("testrepo", n, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, f := range testFrames(n) {
		tm, _ = tm.Update(frameMsg(f))
	}
	return tm.(Model)
}

func TestViewRendersFrame(t *testing.T) {
	m := modelWith(t, 5)
	view := m.View()
	for _, want := range []string{"gitlapse", "testrepo", "frame 1/5", "aaaaaaa", "Richard Wooding", "functions 10", "cognitive 50", "Big", "a.go:10", "▲ +2", "new", "space play/pause"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(view, "computing first frame") {
		t.Error("view stuck on loading state")
	}
}

func TestPlaybackAdvancesOnTick(t *testing.T) {
	m := modelWith(t, 5)
	var tm tea.Model = m
	tm, _ = tm.Update(tickMsg(time.Now()))
	got := tm.(Model)
	if got.cur != 1 {
		t.Fatalf("cur = %d after tick, want 1", got.cur)
	}
}

func TestStepAndBounds(t *testing.T) {
	m := modelWith(t, 3)
	var tm tea.Model = m
	// step back at frame 0 stays at 0
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if tm.(Model).cur != 0 {
		t.Fatalf("cur = %d, want 0", tm.(Model).cur)
	}
	// G jumps to last computed frame
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if tm.(Model).cur != 2 {
		t.Fatalf("cur = %d after G, want 2", tm.(Model).cur)
	}
	// step forward at the frontier stays put
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if tm.(Model).cur != 2 {
		t.Fatalf("cur = %d, want 2", tm.(Model).cur)
	}
}

func TestReplayAfterEnd(t *testing.T) {
	m := modelWith(t, 3)
	var tm tea.Model = m
	tm, _ = tm.Update(doneMsg{})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	// simulate the auto-pause at end of history
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	got := tm.(Model)
	if got.cur != 0 || !got.playing {
		t.Fatalf("cur=%d playing=%v after replay, want 0/true", got.cur, got.playing)
	}
}

func TestChartHandlesPartialComputation(t *testing.T) {
	// 2 frames computed of an eventual 10 — chart must not panic and the
	// playhead must sit in the computed region.
	m := New("testrepo", 10, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	for _, f := range testFrames(2) {
		tm, _ = tm.Update(frameMsg(f))
	}
	view := tm.(Model).View()
	if !strings.Contains(view, "computing 2/10") {
		t.Errorf("view missing computation progress")
	}
}
