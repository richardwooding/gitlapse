package engine

import (
	"testing"
	"time"

	"github.com/richardwooding/gitlapse/internal/history"
)

func commitsN(n int) []history.Commit {
	out := make([]history.Commit, n)
	for i := range out {
		out[i] = history.Commit{SHA: string(rune('a' + i%26)), Time: time.Unix(int64(i), 0)}
	}
	return out
}

func TestSampleKeepsAllWhenUnderMax(t *testing.T) {
	c := commitsN(10)
	got := Sample(c, 20)
	if len(got) != 10 {
		t.Fatalf("got %d commits, want 10", len(got))
	}
}

func TestSampleKeepsEndpoints(t *testing.T) {
	c := commitsN(1000)
	got := Sample(c, 240)
	if len(got) != 240 {
		t.Fatalf("got %d commits, want 240", len(got))
	}
	if got[0] != c[0] {
		t.Errorf("first sampled commit is not the first commit")
	}
	if got[len(got)-1] != c[len(c)-1] {
		t.Errorf("last sampled commit is not the last commit")
	}
	for i := 1; i < len(got); i++ {
		if !got[i].Time.After(got[i-1].Time) {
			t.Fatalf("sampled commits not strictly increasing at %d", i)
		}
	}
}

func TestSampleZeroMaxMeansAll(t *testing.T) {
	c := commitsN(5)
	if got := Sample(c, 0); len(got) != 5 {
		t.Fatalf("got %d commits, want 5", len(got))
	}
}
