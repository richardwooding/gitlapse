// Package engine turns a sampled slice of commits into playable frames of
// code-complexity metrics. Frames are computed oldest-first in a background
// goroutine and streamed over a channel; parse results are cached by blob SHA,
// so a file is only re-parsed on frames where it actually changed.
package engine

import (
	"context"
	"runtime"
	"sort"
	"sync"

	codemetrics "github.com/richardwooding/codemetrics"
	"github.com/richardwooding/codemetrics/treesitter"
	"github.com/richardwooding/gitlapse/internal/history"
	"github.com/richardwooding/projectdetect"
)

// Hotspot is one function's complexity at a frame, with its movement since
// the previous frame.
type Hotspot struct {
	File       string
	Function   string
	StartLine  int
	Cognitive  int
	Cyclomatic int
	Delta      int  // cognitive change vs previous frame
	New        bool // function absent in previous frame
}

// Frame is the full metric snapshot for one commit.
type Frame struct {
	Index     int
	Commit    history.Commit
	Files     int // source files analysed
	Functions int
	TotalCog  int
	MaxCog    int
	TotalCyc  int
	Hotspots  []Hotspot
	Err       error // tree could not be read; zero-valued metrics
}

// Options tunes the engine.
type Options struct {
	MaxFrames    int // sample the timeline down to at most this many commits
	HotspotCount int // hotspots kept per frame
	MaxFileBytes int // blobs larger than this are skipped
}

// Defaults returns sensible options.
func Defaults() Options {
	return Options{MaxFrames: 240, HotspotCount: 15, MaxFileBytes: 1 << 20}
}

// Sample picks at most max commits evenly across the timeline, always keeping
// the first and last.
func Sample(commits []history.Commit, max int) []history.Commit {
	n := len(commits)
	if max <= 0 || n <= max {
		return commits
	}
	out := make([]history.Commit, 0, max)
	for i := 0; i < max; i++ {
		out = append(out, commits[i*(n-1)/(max-1)])
	}
	return out
}

// fileMetrics is the cached parse result for one blob.
type fileMetrics struct {
	funcs []codemetrics.FunctionMetrics
	skip  bool // unreadable, minified, or failed to parse
}

// Run computes frames for the sampled commits and streams them, in order, on
// the returned channel. The channel closes when every frame has been sent or
// ctx is cancelled.
func Run(ctx context.Context, root string, commits []history.Commit, opts Options) (<-chan Frame, error) {
	blobs, err := history.NewBlobReader(ctx, root)
	if err != nil {
		return nil, err
	}

	supported := map[string]bool{"go": true}
	for _, l := range treesitter.SupportedLanguages() {
		supported[l] = true
	}

	frames := make(chan Frame)
	go func() {
		defer close(frames)
		defer blobs.Close()

		cache := map[string]fileMetrics{} // blob SHA -> parse result
		var prev map[string]int           // "file\x00func" -> cognitive at the previous frame

		for i, c := range commits {
			frame, scores := computeFrame(ctx, root, blobs, supported, cache, prev, i, c, opts)
			prev = scores
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	return frames, nil
}

func computeFrame(ctx context.Context, root string, blobs *history.BlobReader,
	supported map[string]bool, cache map[string]fileMetrics, prev map[string]int,
	index int, c history.Commit, opts Options) (Frame, map[string]int) {

	frame := Frame{Index: index, Commit: c}

	entries, err := history.Tree(ctx, root, c.SHA)
	if err != nil {
		frame.Err = err
		return frame, prev
	}

	type job struct {
		entry history.FileEntry
		lang  string
	}
	var jobs []job
	for _, e := range entries {
		if projectdetect.IsVendored(e.Path) {
			continue
		}
		lang := projectdetect.LanguageForPath(e.Path)
		if lang == "" || !supported[lang] {
			continue
		}
		jobs = append(jobs, job{entry: e, lang: lang})
	}

	// Parse blobs not already in the cache, in parallel.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, j := range jobs {
		mu.Lock()
		_, cached := cache[j.entry.BlobSHA]
		mu.Unlock()
		if cached {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			fm := parseBlob(blobs, j.entry.BlobSHA, j.lang, opts.MaxFileBytes)
			mu.Lock()
			cache[j.entry.BlobSHA] = fm
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	scores := make(map[string]int, len(prev))
	var all []Hotspot
	for _, j := range jobs {
		fm := cache[j.entry.BlobSHA]
		if fm.skip {
			continue
		}
		frame.Files++
		for _, f := range fm.funcs {
			frame.Functions++
			cog := f.Cyclomatic
			if f.Cognitive != nil {
				cog = *f.Cognitive
			}
			frame.TotalCog += cog
			frame.TotalCyc += f.Cyclomatic
			if cog > frame.MaxCog {
				frame.MaxCog = cog
			}
			key := j.entry.Path + "\x00" + f.QualifiedName()
			scores[key] = cog
			all = append(all, Hotspot{
				File:       j.entry.Path,
				Function:   f.QualifiedName(),
				StartLine:  f.StartLine,
				Cognitive:  cog,
				Cyclomatic: f.Cyclomatic,
			})
		}
	}

	sort.Slice(all, func(a, b int) bool {
		if all[a].Cognitive != all[b].Cognitive {
			return all[a].Cognitive > all[b].Cognitive
		}
		if all[a].File != all[b].File {
			return all[a].File < all[b].File
		}
		return all[a].Function < all[b].Function
	})
	if len(all) > opts.HotspotCount {
		all = all[:opts.HotspotCount]
	}
	for i := range all {
		key := all[i].File + "\x00" + all[i].Function
		if was, ok := prev[key]; ok {
			all[i].Delta = all[i].Cognitive - was
		} else if prev != nil {
			all[i].New = true
		}
	}
	frame.Hotspots = all
	return frame, scores
}

func parseBlob(blobs *history.BlobReader, sha, lang string, maxBytes int) fileMetrics {
	src, err := blobs.Read(sha)
	if err != nil || (maxBytes > 0 && len(src) > maxBytes) || projectdetect.IsMinified(src) {
		return fileMetrics{skip: true}
	}
	var funcs []codemetrics.FunctionMetrics
	if lang == "go" {
		funcs, err = codemetrics.ParseGo(src)
	} else {
		funcs, err = treesitter.Parse(lang, src)
	}
	if err != nil {
		return fileMetrics{skip: true}
	}
	return fileMetrics{funcs: funcs}
}
