# gitlapse

Replay a git repository's complexity history as a terminal timelapse.

`gitlapse` scrubs through a repo's commit timeline like a video player,
animating per-function [cyclomatic and cognitive complexity] so you can watch
hotspots ignite, spread, and cool as the codebase evolves.

```
 gitlapse codemetrics                         frame 87/240  ▶ 8x
 a1b2c3d  2025-03-14  Richard Wooding — Extract span queries
 files 42   functions 311   cognitive 894   max 38   mean 2.9

 ▁▁▂▂▃▃▃▄▄▅▅▅▆▆▆▇▇▇██████▇▇▆▆▆▅▅▅▅▄▄▄▄▄▄▅▅▅▅▆▆▆▆▇▇▇

 COG  CYC  Δ      HOTSPOT
 38   21   ▲ +6   buildLangState  treesitter/treesitter.go:86
 24   17          computeFrame    engine/engine.go:112
 ...
```

## How it works

Nothing is ever checked out. gitlapse reads historical file contents straight
from the git object database:

- `git log --reverse --first-parent` gives the commit timeline (sampled down
  to `--max-frames` evenly spaced frames)
- `git ls-tree -r` lists each frame's files **with blob SHAs**
- one persistent `git cat-file --batch` process streams blob contents
- parse results are **cached by blob SHA**, so a file is only re-parsed on
  frames where it actually changed — a full-history sweep is close to the
  cost of parsing each file version once

Metrics come from [richardwooding/codemetrics] (Go via `go/ast`, 16 more
languages via tree-sitter). Vendored and minified files are excluded via
[richardwooding/projectdetect].

## Install

```sh
go install github.com/richardwooding/gitlapse@latest
```

Requires `git` on your PATH.

## Usage

```sh
gitlapse                  # replay the repo you're in
gitlapse ~/src/somerepo   # replay another repo
gitlapse --max-frames 500 --hotspots 20
gitlapse --no-first-parent   # include merge side-branches in the timeline
```

| Key       | Action                        |
| --------- | ----------------------------- |
| `space`   | play / pause (replay at end)  |
| `←` / `→` | step one frame                |
| `[` / `]` | slower / faster (1–30 fps)    |
| `g` / `G` | jump to start / end           |
| `q`       | quit                          |

Frames compute in the background while you watch; playback pauses at the
computed frontier and resumes as new frames arrive.

## Export

Render the timelapse headlessly — same UI, no terminal needed:

```sh
gitlapse --export cast --out repo.cast ~/src/somerepo   # asciinema v2 cast
gitlapse --export gif  --out repo.gif  ~/src/somerepo   # animated GIF for READMEs
gitlapse --export cast --fps 15 --width 120 --height 40 # pacing and canvas size
```

Casts play with `asciinema play repo.cast` and embed with
[asciinema-player](https://docs.asciinema.org/manual/player/). GIF export
pipes the cast through [agg](https://github.com/asciinema/agg)
(`brew install agg`).

[cyclomatic and cognitive complexity]: https://github.com/richardwooding/codemetrics
[richardwooding/codemetrics]: https://github.com/richardwooding/codemetrics
[richardwooding/projectdetect]: https://github.com/richardwooding/projectdetect
