package export

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWriteCastFormat(t *testing.T) {
	var sb strings.Builder
	views := []string{"frame one\nline two", "frame two\nline two"}
	now := time.Unix(1_700_000_000, 0)
	if err := WriteCast(&sb, "test title", views, 80, 24, 8, now); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))

	// Header line
	if !scanner.Scan() {
		t.Fatal("missing header line")
	}
	var header map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("header is not valid JSON: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("version = %v, want 2", header["version"])
	}
	if header["width"].(float64) != 80 || header["height"].(float64) != 24 {
		t.Errorf("size = %vx%v, want 80x24", header["width"], header["height"])
	}
	if header["title"] != "test title" {
		t.Errorf("title = %q", header["title"])
	}

	// Event lines: 2 frames + cursor-restore trailer
	var events [][]any
	for scanner.Scan() {
		var ev []any
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("event is not valid JSON: %v: %s", err, scanner.Text())
		}
		events = append(events, ev)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	first := events[0][2].(string)
	if !strings.Contains(first, "\x1b[2J") || !strings.Contains(first, "\x1b[?25l") {
		t.Error("first event must clear screen and hide cursor")
	}
	if !strings.Contains(first, "frame one\x1b[K\r\nline two") {
		t.Errorf("first frame content wrong: %q", first)
	}
	if events[1][0].(float64) != 0.125 {
		t.Errorf("second frame at t=%v, want 0.125 (8 fps)", events[1][0])
	}
	if events[1][1] != "o" {
		t.Errorf("event type = %v, want o", events[1][1])
	}
	last := events[len(events)-1][2].(string)
	if !strings.Contains(last, "\x1b[?25h") {
		t.Error("final event must restore the cursor")
	}
}

func TestWriteCastEmpty(t *testing.T) {
	var sb strings.Builder
	if err := WriteCast(&sb, "t", nil, 80, 24, 8, time.Unix(0, 0)); err == nil {
		t.Fatal("want error for zero frames")
	}
}
