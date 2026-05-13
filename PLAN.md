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
   - No preview pane in v1.0 (added in v1.1).
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

### Default path: `gh auth login`

`gh` stores its OAuth/PAT token in the **macOS keychain** by default — already a secrets manager. For most users, `gh auth login` is the full setup.

For private repos: same flow. `gh auth login` with the `repo` scope grants access to private repositories.

### Override path: `GITHUB_TOKEN` env var

For users who want to manage their PAT in a specific secrets manager (1Password, `pass`, Bitwarden, Vault, etc.):

- gripper checks `GITHUB_TOKEN` first. If set, it's exported as `GH_TOKEN` before invoking `gh` (gh respects this env var and uses it instead of stored creds).
- Populate it from your manager of choice in your shell:

  ```bash
  # 1Password CLI
  export GITHUB_TOKEN="$(op read 'op://Personal/GitHub PAT/credential')"

  # macOS keychain (manual entry)
  export GITHUB_TOKEN="$(security find-generic-password -a "$USER" -s github-pat -w)"

  # pass
  export GITHUB_TOKEN="$(pass show github/pat)"

  # Bitwarden
  export GITHUB_TOKEN="$(bw get password github-pat)"
  ```

- Recommended scopes for the PAT: `repo` (private repo read), nothing else for read-only use.

### What gripper does NOT do

- Does not store tokens itself.
- Does not prompt for tokens interactively.
- Does not implement its own OAuth flow.

This keeps gripper free of credential-handling code; all secrets logic delegates to `gh` or the user's shell.

## Phase 1 deliverables

| Order | Output |
|---|---|
| 1 | `gripper` script with the pipeline above, no preview pane |
| 2 | `homebrew-tap` repo with formula → `brew install <you>/tap/gripper` |
| 3 | README with examples and the `#ref` syntax cheatsheet |
| 4 (v1.1) | fzf `--preview` pane showing file head via `gh api .../contents/PATH` |
| 5 (v1.2) | Lazy per-folder fetch fallback for truncated trees |

Order of work: build locally → symlink to `~/.local/bin` → use for a week → if it sticks, publish + tap formula.

## Phase 2: real TUI with collapse (later)

- **Language:** Go + Bubbletea — single static binary, brew-friendly, no runtime dependency on user's machine.
- **Alternative considered:** TypeScript + OpenTUI + trees.software. Polished tree widget but distribution requires Bun or bundling.
- **Same backend** as Phase 1 (gh API tree fetch, tarball extract).
- **Tree widget behavior:**
  - Hierarchical, arrow-key nav, right-arrow to expand, left to collapse, space to toggle.
  - Folder toggle propagates to all descendants.
  - Parent shows partial-selection marker `[~]` when only some children are selected.
- **Trigger to build:** the moment fzf's flat list becomes friction on a real task. Don't pre-build.
- **Coexistence:** brew tap ships both `gripper` (shell) and `gripper-tui` (binary).

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
