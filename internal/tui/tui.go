// Package tui is the Bubble Tea front end: a video-player-style scrubber over
// the metric frames the engine streams in.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/richardwooding/gitlapse/internal/engine"
)

var (
	accent    = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}
	dimAccent = lipgloss.AdaptiveColor{Light: "#c4b5fd", Dark: "#4c1d95"}
	subtle    = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	hot       = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"}
	warm      = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#fbbf24"}
	good      = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34d399"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle = lipgloss.NewStyle().Foreground(subtle)
	shaStyle    = lipgloss.NewStyle().Foreground(warm)
	headStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	barStyle    = lipgloss.NewStyle().Foreground(dimAccent)
	playStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	hotStyle    = lipgloss.NewStyle().Foreground(hot)
	warmStyle   = lipgloss.NewStyle().Foreground(warm)
	goodStyle   = lipgloss.NewStyle().Foreground(good)
)

var speeds = []int{1, 2, 4, 8, 15, 30} // frames per second

type frameMsg engine.Frame
type doneMsg struct{}
type tickMsg time.Time

// Model is the Bubble Tea model for one gitlapse session.
type Model struct {
	repoName string
	total    int
	framesCh <-chan engine.Frame
	churn    func(file string) int // HEAD-anchored commit count per file; nil hides the column

	frames  []engine.Frame
	cur     int
	playing bool
	speed   int // index into speeds
	done    bool

	width, height int
}

// New builds the initial model. total is the number of frames the engine will
// eventually deliver on ch.
func New(repoName string, total int, ch <-chan engine.Frame) Model {
	return Model{repoName: repoName, total: total, framesCh: ch, playing: true, speed: 3}
}

// WithChurn attaches a per-file commit-count lookup (HEAD-anchored, from
// gitmeta); the hotspot table then shows a CHN column. High churn on a high-
// complexity function is the classic refactor-priority signal.
func (m Model) WithChurn(churn func(file string) int) Model {
	m.churn = churn
	return m
}

// RenderFrames renders every frame as the full UI at the given size, as if
// the playhead were on it with computation complete — the offline twin of
// live playback, used by cast/GIF export. churn may be nil.
func RenderFrames(repoName string, frames []engine.Frame, width, height, fps int, churn func(string) int) []string {
	m := New(repoName, len(frames), nil).WithChurn(churn)
	m.width, m.height = width, height
	m.frames = frames
	m.done = true
	// Show the nearest playback speed in the header's ▶ indicator.
	for i, s := range speeds {
		if s <= fps {
			m.speed = i
		}
	}
	views := make([]string, len(frames))
	for i := range frames {
		m.cur = i
		views[i] = m.View()
	}
	return views
}

// ForceColors makes lipgloss emit truecolor ANSI (with dark-background
// palette choices) even when stdout is not a terminal. Rendered output goes
// into cast files, not the current terminal, so detection would wrongly
// strip all color.
func ForceColors() {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.waitForFrame(), m.tick())
}

func (m Model) waitForFrame() tea.Cmd {
	return func() tea.Msg {
		f, ok := <-m.framesCh
		if !ok {
			return doneMsg{}
		}
		return frameMsg(f)
	}
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(speeds[m.speed]), func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case frameMsg:
		m.frames = append(m.frames, engine.Frame(msg))
		return m, m.waitForFrame()
	case doneMsg:
		m.done = true
	case tickMsg:
		if m.playing && m.cur < len(m.frames)-1 {
			m.cur++
		}
		if m.playing && m.done && m.cur == len(m.frames)-1 {
			m.playing = false // reached the end of history
		}
		return m, m.tick()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case " ":
			if !m.playing && m.done && m.cur == len(m.frames)-1 {
				m.cur = 0 // replay from the start
			}
			m.playing = !m.playing
		case "right", "l":
			m.playing = false
			if m.cur < len(m.frames)-1 {
				m.cur++
			}
		case "left", "h":
			m.playing = false
			if m.cur > 0 {
				m.cur--
			}
		case "g", "home":
			m.playing = false
			m.cur = 0
		case "G", "end":
			m.playing = false
			if len(m.frames) > 0 {
				m.cur = len(m.frames) - 1
			}
		case "]":
			if m.speed < len(speeds)-1 {
				m.speed++
			}
		case "[":
			if m.speed > 0 {
				m.speed--
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	if len(m.frames) == 0 {
		return titleStyle.Render(" gitlapse") + subtleStyle.Render("  computing first frame…")
	}

	f := m.frames[m.cur]
	var b strings.Builder

	// Header
	status := fmt.Sprintf("frame %d/%d", m.cur+1, m.total)
	if !m.done {
		status += fmt.Sprintf("  computing %d/%d", len(m.frames), m.total)
	}
	mode := "⏸"
	if m.playing {
		mode = fmt.Sprintf("▶ %dx", speeds[m.speed])
	}
	left := titleStyle.Render(" gitlapse ") + subtleStyle.Render(m.repoName)
	right := subtleStyle.Render(status+"  ") + playStyle.Render(mode) + " "
	b.WriteString(padBetween(left, right, m.width) + "\n")

	// Commit line
	commit := " " + shaStyle.Render(short(f.Commit.SHA)) +
		subtleStyle.Render("  "+f.Commit.Time.Format("2006-01-02")+"  ") +
		f.Commit.Author + subtleStyle.Render(" — ") + f.Commit.Subject
	b.WriteString(truncate(commit, m.width) + "\n")

	// Stats line
	mean := 0.0
	if f.Functions > 0 {
		mean = float64(f.TotalCog) / float64(f.Functions)
	}
	stats := fmt.Sprintf(" files %d   functions %d   cognitive %d   max %d   mean %.1f",
		f.Files, f.Functions, f.TotalCog, f.MaxCog, mean)
	b.WriteString(headStyle.Render(stats) + "\n\n")

	// Chart
	b.WriteString(m.chart(6) + "\n\n")

	// Hotspots
	b.WriteString(m.hotspots(f) + "\n")

	// Footer
	help := " space play/pause   ←/→ step   [/] speed   g/G start/end   q quit"
	b.WriteString(subtleStyle.Render(truncate(help, m.width)))
	return b.String()
}

// chart renders total cognitive complexity across all computed frames as a
// bar chart of the given height, with the playhead column highlighted.
func (m Model) chart(chartHeight int) string {
	cols := min(max(m.width-2, 10), m.total)

	// Bucket frames into columns (max value per bucket).
	vals := make([]int, cols) // -1 = not yet computed
	for i := range vals {
		vals[i] = -1
	}
	maxVal := 1
	for i, f := range m.frames {
		col := i * cols / m.total
		if f.TotalCog > vals[col] {
			vals[col] = f.TotalCog
		}
		if f.TotalCog > maxVal {
			maxVal = f.TotalCog
		}
	}
	playCol := m.cur * cols / m.total

	blocks := []rune(" ▁▂▃▄▅▆▇█")
	rows := make([]strings.Builder, chartHeight)
	for col := range cols {
		v := vals[col]
		var cells [8]rune // one per row, bottom-up eighths
		if v >= 0 {
			eighths := v * chartHeight * 8 / maxVal
			if v > 0 && eighths == 0 {
				eighths = 1
			}
			for r := range chartHeight {
				e := eighths - r*8
				switch {
				case e >= 8:
					cells[r] = '█'
				case e > 0:
					cells[r] = blocks[e]
				default:
					cells[r] = ' '
				}
			}
		} else {
			for r := range chartHeight {
				cells[r] = ' '
			}
		}
		style := barStyle
		if col == playCol {
			style = playStyle
		}
		for r := range chartHeight {
			ch := string(cells[r])
			if col == playCol && ch == " " {
				ch = "·"
			}
			rows[chartHeight-1-r].WriteString(style.Render(ch))
		}
	}
	lines := make([]string, chartHeight)
	for i := range rows {
		lines[i] = " " + rows[i].String()
	}
	return strings.Join(lines, "\n")
}

func (m Model) hotspots(f engine.Frame) string {
	var b strings.Builder
	churnHdr := ""
	if m.churn != nil {
		churnHdr = " CHN "
	}
	b.WriteString(subtleStyle.Render(fmt.Sprintf(" %-4s %-4s%s %-6s %s", "COG", "CYC", churnHdr, "Δ", "HOTSPOT")) + "\n")

	maxRows := max(m.height-16, 1)
	n := min(len(f.Hotspots), maxRows)
	for _, h := range f.Hotspots[:n] {
		b.WriteString(truncate(m.hotspotRow(h), m.width) + "\n")
	}
	return b.String()
}

// hotspotRow renders one table line: complexity, churn (when available),
// movement, and location.
func (m Model) hotspotRow(h engine.Hotspot) string {
	cogStyle := goodStyle
	switch {
	case h.Cognitive >= 15:
		cogStyle = hotStyle
	case h.Cognitive >= 7:
		cogStyle = warmStyle
	}

	churnCol := ""
	if m.churn != nil {
		c := m.churn(h.File)
		st := subtleStyle
		switch {
		case c >= 20:
			st = hotStyle
		case c >= 8:
			st = warmStyle
		}
		cell := "-"
		if c > 0 {
			cell = fmt.Sprintf("%d", c)
		}
		churnCol = " " + st.Render(fmt.Sprintf("%-4s", cell))
	}

	delta := ""
	switch {
	case h.New:
		delta = warmStyle.Render("new")
	case h.Delta > 0:
		delta = hotStyle.Render(fmt.Sprintf("▲ +%d", h.Delta))
	case h.Delta < 0:
		delta = goodStyle.Render(fmt.Sprintf("▼ %d", h.Delta))
	}
	loc := fmt.Sprintf("%s  %s", h.Function, subtleStyle.Render(fmt.Sprintf("%s:%d", h.File, h.StartLine)))
	return fmt.Sprintf(" %s %-4d%s %-6s %s",
		cogStyle.Render(fmt.Sprintf("%-4d", h.Cognitive)), h.Cyclomatic,
		churnCol, delta, loc)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func padBetween(left, right string, width int) string {
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width-1, "…")
}
