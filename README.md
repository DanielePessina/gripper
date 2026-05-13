# gripper

Interactive picker for selectively downloading files and folders from a GitHub repository, without cloning the full git history.

Two front-ends, one job:

- **`gripper`** — collapsible-tree TUI (Go + Bubbletea). Multi-select with cascade, live file preview, path remapping with strip/flatten/per-row edit and collision warnings.
- **`gripper-fzf`** — flat-list fuzzy picker (shell + fzf). Fast daily driver when you just want to grab a few paths and run.

Solves what `degit` / `tiged` can't: picking multiple files and folders spread across the tree, without knowing every path in advance.

## Install

### Homebrew (recommended, macOS / Linux)

```sh
brew tap DanielePessina/tap
brew install DanielePessina/tap/gripper
```

This installs both binaries (`gripper` and `gripper-fzf`).

### From source

```sh
git clone https://github.com/DanielePessina/gripper
cd gripper
make install            # installs to ~/.local/bin
```

### Go install (TUI only)

```sh
go install github.com/DanielePessina/gripper/cmd/gripper-tui@latest
# binary lands at $GOBIN/gripper-tui — rename to `gripper` on PATH if you like
```

## Auth

Both tools use the GitHub CLI for authentication:

```sh
gh auth login
```

Choose the `repo` scope when prompted if you want access to private repositories. Tokens are stored in the macOS keychain (or the equivalent on your platform). gripper itself stores no credentials.

## Usage

```sh
gripper <owner/repo|github-url>[#ref] [target-dir] [--force] [--dry-run]
gripper-fzf <owner/repo|github-url>[#ref] [target-dir] [--force] [--dry-run]
```

Examples:

```sh
gripper torvalds/linux
gripper torvalds/linux#v6.5 ./kernel-stuff/
gripper https://github.com/sst/opentui examples/
gripper-fzf charmbracelet/glow --dry-run
```

Defaults:

- **Ref** — auto-detected (the repo's default branch) unless you pass `#ref`.
- **Target directory** — `./<repo>/`, with full repo-relative paths preserved.
- Use `--force` to allow a non-empty target directory.
- Use `--dry-run` to print what would be downloaded and exit.

### `gripper` (TUI) keys

**Tree screen** — pick what to download:

| Key | Action |
|---|---|
| ↑ / ↓ or j / k | Navigate |
| → / l | Expand folder |
| ← / h | Collapse folder, or jump to parent |
| pgup / pgdown, g / G | Page / home / end |
| space | Toggle selection (cascades to descendants) |
| enter | Go to review screen |
| q | Quit |

**Review screen** — see and remap target paths before download:

| Key | Action |
|---|---|
| ↑ / ↓ or j / k | Navigate rows |
| e or enter | Edit current row's target inline |
| s | Strip longest common prefix from all targets |
| f | Flatten all targets to basename |
| r | Reset all targets to source paths |
| x | Drop the current row from the selection |
| o | Edit the output base directory |
| c | Confirm and download |
| esc | Back to tree screen (selections + edits preserved) |
| q | Quit |

Target paths colliding with another row are highlighted; `c` refuses to confirm until collisions are resolved.

### `gripper-fzf` (shell)

A single fzf picker over a flat list of paths. Folder entries appear inline (e.g. `[D]   60.3 KB  ui/`); selecting one expands to every file beneath. Same auth, same `#ref` syntax, same `--force` / `--dry-run` flags.

| Key | Action |
|---|---|
| space | Toggle selection |
| ctrl-a | Select all matching the filter |
| ctrl-d | Deselect all |
| enter | Download |
| esc | Cancel |

## Implementation

- `gripper` (Go) — `net/http` for the GitHub API, `archive/tar` + `compress/gzip` for extract. No `curl`/`tar` runtime deps. Single ~10 MB static binary. Auth token obtained via `gh auth token`.
- `gripper-fzf` (shell) — pipeline of `gh api ... | jq | awk | fzf`, then `curl` for the tarball and `tar` for extract.

Both fetch the repo's recursive git tree once (via `git/trees/{ref}?recursive=1`), let you pick paths, then download the **tarball** of that ref and extract only the selected files. One HTTP request for the archive regardless of selection size.

### Limitations

- Truncated trees: GitHub's recursive endpoint caps at ~100k entries / 7MB. Very large monorepos hit this; both tools exit with a clear error in that case. Lazy per-folder fetch is on the roadmap.
- Tarball-based download means the full archive is fetched even when you only want a few files — usually fine, painful for huge repos.

## Roadmap

- `/` filter in the TUI tree screen for big repos.
- `?` help overlay.
- Spinner during the tarball download.
- Lazy per-folder fetch for truncated trees.
- Precompiled binary bottles in the brew tap so installs don't require a local Go toolchain.

See [PLAN.md](PLAN.md) for the original design notes and decision log.

## License

[MIT](LICENSE)
