package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const usageText = `Usage: gripper <owner/repo|github-url>[#ref] [target-dir] [--force] [--dry-run]

Interactively pick files and folders from a GitHub repository.

Arguments:
  owner/repo[#ref]   Repo slug. Optional #ref (branch, tag, or SHA).
                     URL forms also accepted: https://github.com/owner/repo
  target-dir         Where files land. Default: ./<repo>/

Options:
  --force            Allow non-empty target dir; overwrite conflicting files.
  --dry-run          Print what would be downloaded and exit (after review).
  -h, --help         Show this help.

Auth:
  Uses gh auth login. Run gh auth login once with the repo scope.

Tree screen:
  ↑/↓ or j/k       Navigate
  →/l              Expand folder
  ←/h              Collapse folder / jump to parent
  space            Toggle selection (cascades to descendants)
  /                (TODO) Filter
  enter            Go to review screen
  q                Quit

Review screen:
  ↑/↓ or j/k       Navigate
  e or enter       Edit current row's target inline
  s                Strip longest common path prefix from all targets
  f                Flatten all targets to basename
  r                Reset all targets to source paths
  x                Drop the current row from the selection
  o                Edit the output base directory
  c                Confirm and download
  esc              Back to tree screen
  q                Quit
`

func main() {
	var (
		target      string
		force       bool
		dryRun      bool
		positionals []string
	)
	for _, a := range os.Args[1:] {
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(usageText)
			return
		case a == "--force":
			force = true
		case a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "--target="):
			target = strings.TrimPrefix(a, "--target=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(os.Stderr, "Unknown flag:", a)
			fmt.Fprint(os.Stderr, usageText)
			os.Exit(1)
		default:
			positionals = append(positionals, a)
		}
	}

	if len(positionals) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}

	spec := positionals[0]
	if len(positionals) > 1 && target == "" {
		target = positionals[1]
	}

	owner, repo, ref, err := parseSpec(spec)
	if err != nil {
		fail("invalid spec: " + err.Error())
	}

	token, err := authToken()
	if err != nil {
		fail("not authenticated to GitHub. Run: gh auth login")
	}

	client := NewClient(token, owner, repo, ref)
	if ref == "" {
		ref, err = client.DefaultBranch()
		if err != nil {
			fail("could not resolve default branch: " + err.Error())
		}
		client.Ref = ref
	}

	if target == "" {
		target = "./" + repo
	}
	target = SanitizeOutDir(target)

	if !dryRun && !force {
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			entries, _ := os.ReadDir(target)
			if len(entries) > 0 {
				fail(fmt.Sprintf("target %q is not empty. Use --force to allow overwriting.", target))
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Fetching tree for %s/%s @ %s ...\n", owner, repo, ref)
	entries, truncated, err := client.FetchTree()
	if err != nil {
		fail("fetch tree: " + err.Error())
	}
	if truncated {
		fail("tree exceeds GitHub's recursive limit (over 100k entries or 7MB). Lazy fetch is a future feature.")
	}

	root := BuildTree(entries)

	m := newModel(client, root, target, owner, repo, ref, dryRun)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		fail(err.Error())
	}

	fm := final.(*model)
	if fm.cancelled {
		return
	}
	if fm.err != nil {
		fail(fm.err.Error())
	}
	for _, line := range fm.resultLines {
		fmt.Println(line)
	}
}

func parseSpec(spec string) (owner, repo, ref string, err error) {
	spec = strings.TrimPrefix(spec, "https://github.com/")
	spec = strings.TrimPrefix(spec, "http://github.com/")
	spec = strings.TrimSuffix(spec, ".git")
	spec = strings.TrimSuffix(spec, "/")

	if i := strings.Index(spec, "#"); i >= 0 {
		ref = spec[i+1:]
		spec = spec[:i]
	}

	re := regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	if !re.MatchString(spec) {
		err = fmt.Errorf("expected owner/repo or https://github.com/owner/repo, got %q", spec)
		return
	}
	parts := strings.SplitN(spec, "/", 2)
	owner = parts[0]
	repo = parts[1]
	return
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "Error: "+msg)
	os.Exit(1)
}
