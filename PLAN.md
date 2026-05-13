# gripper

Interactive TUI for selectively downloading files and folders from a GitHub repository, without cloning the full git history. Solves what `degit` / `tiged` can't: multi-select files and folders spread across the tree, with a browser-like picker instead of pre-known paths.

## Phase 1: shell wrapper (ship first)

### CLI surface

```
gripper <owner/repo|github-url>[#ref] [target-dir] [--force] [--dry-run]
```

Examples:

```
gripper torvalds/linux
gripper torvalds/linux#v6.5 ./kernel-stuff/
gripper https://github.com/sst/opentui examples/
gripper torvalds/linux --force
```

- `#ref` accepts branch, tag, or SHA. Defaults to the repo's default branch (auto-detected).
- `target-dir` defaults to `./<repo>/`. Paths preserved verbatim relative to repo root.
- `--force` allows non-empty target dirs (clobbers conflicting files only; rest of target untouched).
- `--dry-run` prints what would be downloaded and exits.

### Pipeline

1. **Parse args.** Split `owner/repo`, optional `#ref`, optional target. Accept slug or `https://github.com/...` (strip `.git`, trailing slash).
2. **Preflight checks.** Verify `gh`, `fzf`, `jq`, `tar` on PATH. Verify `gh auth status` (or `GITHUB_TOKEN` set — see Auth). Verify target dir doesn't conflict unless `--force`.
3. **Resolve ref.** If not provided: `gh api repos/$o/$r --jq .default_branch`.
4. **Fetch tree.** `gh api "repos/$o/$r/git/trees/$ref?recursive=1"` → JSON with every entry (`type`, `path`, `size`, `sha`).
5. **Handle truncation.** If `.truncated == true` (>100k entries / >7MB tree), error with a clear message. Lazy per-folder fetch is a future feature. (Affects <0.1% of real repos.)
6. **Build display list.** One line per entry:
   ```
   [D]   12.4 MB  folder1/
         2.1 KB   folder1/README.md
   ```
   - Folder lines aggregate blob sizes for everything under their prefix (one `jq` pass).
   - Human-readable sizes (B/KB/MB/GB) aligned in a fixed-width column.
   - Sort: depth-first, folders before files at each level.
7. **fzf picker** with these bindings:
   - `--multi`
   - `--bind 'space:toggle'`
   - `--bind 'ctrl-a:select-all,ctrl-d:deselect-all'`
   - `--header` line with keys + selection count
   - `--preview` pane:
     - For file entries: head of file content via `gh api .../contents/PATH`, base64-decoded. Skip with `(binary file)` if content fails the printability check.
     - For folder entries: list of direct children with sizes, computed in-memory from `tree.json` (no extra API call).
8. **Expand selection.** For each selected line that is a folder, replace it with every blob whose path starts with that prefix; dedupe.
9. **Download tarball.** `gh api repos/$o/$r/tarball/$ref > $tmp/repo.tgz`. Print `Downloading tarball (~XX MB)...` upfront.
10. **Extract selected files.** Peek tarball's top-level dir via `tar -tzf $tmp/repo.tgz | head -1`, build `files-from.txt` by prepending that prefix to each selected path, then:
    ```bash
    tar -xzf $tmp/repo.tgz -C "$target" --strip-components=1 -T files-from.txt
    ```
11. **Cleanup.** `rm -rf $tmp` via `trap EXIT`. Print `Wrote N files to $target/`.

### Error handling

- Missing dependency → exit 1 with `Install with: brew install gh fzf jq`.
- `gh auth status` fails (and no `GITHUB_TOKEN`) → exit 1 with `Run: gh auth login`.
- Repo not found / network error → propagate `gh`'s error.
- Empty fzf selection (user hit esc) → exit 0 silently.
- Truncated tree → exit 1 with clear message.

### Implementation notes

- Single bash script, ~150–200 lines, no external lib files.
- `set -euo pipefail` at the top.
- `mktemp -d` for tmp dir; `trap 'rm -rf "$tmp"' EXIT`.
- Quote all paths.

## Auth & secrets

gripper delegates all auth to `gh`. `gh auth login` stores the token in the **macOS keychain** by default — already a secrets manager — and gripper reads it via `gh auth token` when downloading the tarball.

For private repos: `gh auth login` with the `repo` scope. No gripper-specific setup.

gripper does not store tokens, does not prompt for tokens, and does not implement its own OAuth flow. A future version may add a `GITHUB_TOKEN` env-var override if a use case appears for keeping tokens in a different secrets manager.

## Phase 1 deliverables

| Order | Output |
|---|---|
| 1 | `gripper` script with the pipeline above, no preview pane |
| 2 | `homebrew-tap` repo with formula → `brew install <you>/tap/gripper` |
| 3 | README with examples and the `#ref` syntax cheatsheet |
| 4 (v1.1) | fzf `--preview` pane showing file head via `gh api .../contents/PATH` |
| 5 (v1.2) | Lazy per-folder fetch fallback for truncated trees |

Order of work: build locally → symlink to `~/.local/bin` → use for a week → if it sticks, publish + tap formula.

## Phase 2: real TUI with collapse + path remapping

### Stack

- Go + Bubbletea (single static binary, brew-friendly).
- `lipgloss` for styling, `bubbles/textinput` and `bubbles/viewport` for inline edit + scrollable preview. No third-party tree widget — tree rendering is ~150 LOC of lipgloss.
- Native `net/http` to GitHub API; auth token via `exec("gh auth token")` once at startup. Stdlib `archive/tar` + `compress/gzip` for extraction. No `curl`/`tar` runtime deps.

### Naming

- Go binary becomes **`gripper`** (the headline tool).
- Shell script is renamed **`gripper-sh`** at ship time (kept as fallback).
- During development they coexist: shell at `bin/gripper`, Go binary at `cmd/gripper-tui/`. Rename happens when v2 is verified.

### Two screens

**Tree screen** (entry point):
- Left pane: collapsible tree with checkboxes. `[ ]` unchecked, `[x]` checked, `[~]` partial (some descendants checked).
- Right pane: same preview pane as v1 — file head for blobs, direct-children listing for folders.
- Bottom statusbar: `Selected: N files, X MB | / filter  ? help  enter confirm  q quit`.
- Keys: `↑/↓` nav, `→` expand, `←` collapse, `space` toggle (cascades to descendants; updates ancestor partial state), `enter` go to review screen, `/` filter, `q` quit.

**Review screen** (after enter on tree):
- Rows: `SOURCE → TARGET   SIZE`. Each blob in the selection has an editable target path.
- Collision warnings inline: row turns red, suffix `← collides with row N`.
- Top-of-screen field: output base dir (default `./<repo>/`, editable).
- Keys:
  - `e` or `enter` — edit the current row's target inline (textinput pops in).
  - `s` — strip longest common prefix from all targets (tar `--strip-components` style).
  - `f` — flatten: replace every target with its basename.
  - `r` — reset all targets to source paths.
  - `x` — drop the current row from the selection.
  - `o` — edit output base dir.
  - `c` — confirm and download (refuses if collisions present and `--force-collisions` not set).
  - `esc` — back to tree screen, preserving selections + edits.

### Repo layout

```
cmd/gripper-tui/main.go        # entry; ~30 LOC
cmd/gripper-tui/gh.go          # GitHub API + tarball download/extract
cmd/gripper-tui/tree.go        # tree model + selection cascade
cmd/gripper-tui/remap.go       # LCP strip, flatten, collision detection
cmd/gripper-tui/ui.go          # Bubbletea Model + both screens
go.mod / go.sum
```

### What we still defer to v2.1+

- Lazy per-folder fetch for truncated trees.
- Mouse support.
- Saved profiles (named selection presets).
- Re-run with lockfile for updates.

## Out of scope (and why)

- **SSH `git@` URL parsing** — not a paste-from-browser shape.
- **Selection prefix stripping** — surprising behavior with multi-root selections.
- **Built-in pre-download confirmation** — `--dry-run` covers the cautious case.
- **Submodules** — GitHub tarballs don't include them.
- **A "re-run to update" lockfile** — different tool (tiged scratches that itch for whole-repo cases).
- **Own OAuth flow / credential storage** — delegated to `gh` and the user's secrets manager.

## Open questions

1. **Preview pane in v1.0 or push to v1.1?** Adds a per-scroll `gh api contents` call. Lean: v1.1.
2. **`gripper --list owner/repo`** (dump tree without fzf, for piping into other tools)? Cheap to add — defer until there's a concrete use.
3. **Repo visibility for the tool itself:** private during development, public when ready to share.
