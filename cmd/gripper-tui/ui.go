package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenTree screen = iota
	screenReview
	screenDownloading
	screenDone
)

type model struct {
	client  *Client
	root    *Node
	target  string
	dryRun  bool
	owner   string
	repo    string
	refName string

	visible []VisibleNode
	cursor  int
	offset  int

	previewPath string
	previewBody string

	selections []Selection
	revCursor  int
	revOffset  int
	editing    bool
	editInput  textinput.Model
	editingOut bool

	screen      screen
	err         error
	cancelled   bool
	resultLines []string

	statusMsg  string
	statusKind statusKind

	width, height int
}

type statusKind int

const (
	statusInfo statusKind = iota
	statusWarn
	statusError
)

func newModel(client *Client, root *Node, target, owner, repo, refName string, dryRun bool) *model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Width = 80
	return &model{
		client:    client,
		root:      root,
		target:    target,
		dryRun:    dryRun,
		owner:     owner,
		repo:      repo,
		refName:   refName,
		visible:   root.Visible(),
		screen:    screenTree,
		editInput: ti,
	}
}

func (m *model) Init() tea.Cmd { return nil }

type previewMsg struct {
	path string
	body string
}

type downloadDoneMsg struct {
	written int
	err     error
}

func (m *model) updatePreviewCmd() tea.Cmd {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		m.previewPath = ""
		m.previewBody = ""
		return nil
	}
	vn := m.visible[m.cursor]
	path := vn.Path
	if vn.IsDir {
		m.previewPath = path
		m.previewBody = folderListing(m.root, path)
		return nil
	}
	m.previewPath = path
	m.previewBody = "Loading..."
	client := m.client
	return func() tea.Msg {
		body, isBin, err := client.FetchFileHead(path, 8000)
		if err != nil {
			return previewMsg{path: path, body: "(error: " + err.Error() + ")"}
		}
		if isBin {
			return previewMsg{path: path, body: "(binary file)"}
		}
		return previewMsg{path: path, body: body}
	}
}

func folderListing(root *Node, path string) string {
	n := root.Find(path)
	if n == nil {
		return "(not found)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "DIR: %s/\n\n", path)
	for _, c := range n.Children {
		if c.IsDir {
			fmt.Fprintf(&sb, "  [D]  %10s  %s/\n", humanSize(c.Size), c.Name)
		} else {
			fmt.Fprintf(&sb, "  [F]  %10s  %s\n", humanSize(c.Size), c.Name)
		}
	}
	return sb.String()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.updatePreviewCmd()
	case previewMsg:
		if msg.path == m.previewPath {
			m.previewBody = msg.body
		}
		return m, nil
	case downloadDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.resultLines = append(m.resultLines, fmt.Sprintf("Wrote %d files to %s/", msg.written, m.target))
		m.screen = screenDone
		return m, tea.Quit
	case tea.KeyMsg:
		switch m.screen {
		case screenTree:
			return m.updateTree(msg)
		case screenReview:
			return m.updateReview(msg)
		case screenDownloading:
			return m, nil
		}
	}
	return m, nil
}

func (m *model) updateTree(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.updatePreviewCmd()
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
		return m, m.updatePreviewCmd()
	case "pgup":
		m.cursor -= 10
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, m.updatePreviewCmd()
	case "pgdown":
		m.cursor += 10
		if m.cursor > len(m.visible)-1 {
			m.cursor = len(m.visible) - 1
		}
		return m, m.updatePreviewCmd()
	case "home", "g":
		m.cursor = 0
		return m, m.updatePreviewCmd()
	case "end", "G":
		m.cursor = len(m.visible) - 1
		return m, m.updatePreviewCmd()
	case "right", "l":
		if m.cursor < len(m.visible) {
			n := m.visible[m.cursor].Node
			if n.IsDir && !n.Expanded {
				n.Expanded = true
				m.visible = m.root.Visible()
			}
		}
		return m, nil
	case "left", "h":
		if m.cursor < len(m.visible) {
			vn := m.visible[m.cursor]
			n := vn.Node
			if n.IsDir && n.Expanded {
				n.Expanded = false
				m.visible = m.root.Visible()
			} else if n.Parent != nil && n.Parent != m.root {
				for i, v := range m.visible {
					if v.Node == n.Parent {
						m.cursor = i
						break
					}
				}
				return m, m.updatePreviewCmd()
			}
		}
		return m, nil
	case " ":
		if m.cursor < len(m.visible) {
			m.visible[m.cursor].Node.Toggle()
		}
		return m, nil
	case "enter":
		blobs := m.root.SelectedBlobs()
		if len(blobs) == 0 {
			m.statusMsg = "Nothing selected — pick at least one file or folder with space."
			m.statusKind = statusWarn
			return m, nil
		}
		m.selections = make([]Selection, len(blobs))
		for i, b := range blobs {
			m.selections[i] = Selection{Source: b.Path, Target: b.Path, Size: b.Size}
		}
		sort.Slice(m.selections, func(i, j int) bool { return m.selections[i].Source < m.selections[j].Source })
		m.screen = screenReview
		m.revCursor = 0
		m.statusMsg = ""
		return m, nil
	}
	return m, nil
}

func (m *model) updateReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing {
		switch msg.String() {
		case "esc":
			m.editing = false
			m.editingOut = false
			return m, nil
		case "enter":
			val := m.editInput.Value()
			if m.editingOut {
				m.target = val
			} else {
				m.selections[m.revCursor].Target = val
			}
			m.editing = false
			m.editingOut = false
			return m, nil
		}
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	case "esc":
		m.screen = screenTree
		m.statusMsg = ""
		return m, m.updatePreviewCmd()
	case "up", "k":
		if m.revCursor > 0 {
			m.revCursor--
		}
		return m, nil
	case "down", "j":
		if m.revCursor < len(m.selections)-1 {
			m.revCursor++
		}
		return m, nil
	case "pgup":
		m.revCursor -= 10
		if m.revCursor < 0 {
			m.revCursor = 0
		}
		return m, nil
	case "pgdown":
		m.revCursor += 10
		if m.revCursor > len(m.selections)-1 {
			m.revCursor = len(m.selections) - 1
		}
		return m, nil
	case "g":
		m.revCursor = 0
		return m, nil
	case "G":
		m.revCursor = len(m.selections) - 1
		return m, nil
	case "s":
		before := allTargets(m.selections)
		StripLCP(m.selections)
		if equalSlices(before, allTargets(m.selections)) {
			m.statusMsg = "Strip: no common prefix to remove."
			m.statusKind = statusInfo
		} else {
			m.statusMsg = "Stripped longest common prefix."
			m.statusKind = statusInfo
		}
		return m, nil
	case "f":
		Flatten(m.selections)
		m.statusMsg = "Flattened all targets to basename."
		m.statusKind = statusInfo
		return m, nil
	case "r":
		ResetTargets(m.selections)
		m.statusMsg = "Targets reset to source paths."
		m.statusKind = statusInfo
		return m, nil
	case "x":
		if len(m.selections) == 0 {
			return m, nil
		}
		m.selections = append(m.selections[:m.revCursor], m.selections[m.revCursor+1:]...)
		if m.revCursor >= len(m.selections) && m.revCursor > 0 {
			m.revCursor--
		}
		if len(m.selections) == 0 {
			m.screen = screenTree
			return m, m.updatePreviewCmd()
		}
		return m, nil
	case "o":
		m.editing = true
		m.editingOut = true
		m.editInput.SetValue(m.target)
		m.editInput.CursorEnd()
		m.editInput.Focus()
		return m, nil
	case "e", "enter":
		if m.revCursor < len(m.selections) {
			m.editing = true
			m.editingOut = false
			m.editInput.SetValue(m.selections[m.revCursor].Target)
			m.editInput.CursorEnd()
			m.editInput.Focus()
		}
		return m, nil
	case "c":
		col := Collisions(m.selections)
		if len(col) > 0 {
			m.statusMsg = fmt.Sprintf("Refusing: %d row(s) collide on target paths. Edit them with 'e' or remove with 'x'.", len(col))
			m.statusKind = statusError
			return m, nil
		}
		if m.dryRun {
			m.resultLines = []string{
				fmt.Sprintf("Would download %d files to %s/:", len(m.selections), m.target),
			}
			for _, s := range m.selections {
				if s.Source == s.Target {
					m.resultLines = append(m.resultLines, "  "+s.Target)
				} else {
					m.resultLines = append(m.resultLines, "  "+s.Source+" -> "+s.Target)
				}
			}
			m.screen = screenDone
			return m, tea.Quit
		}
		m.screen = screenDownloading
		client := m.client
		target := m.target
		targets := map[string]string{}
		for _, s := range m.selections {
			targets[s.Source] = s.Target
		}
		return m, func() tea.Msg {
			written, err := client.DownloadAndExtract(targets, target)
			return downloadDoneMsg{written: written, err: err}
		}
	}
	return m, nil
}

func allTargets(sels []Selection) []string {
	out := make([]string, len(sels))
	for i, s := range sels {
		out[i] = s.Target
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- Rendering ----

var (
	cursorStyle    = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	dirStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	selectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	partialStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	infoStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	statusBarStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252"))
	headerStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Bold(true)
	collideStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	arrowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

func (m *model) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	switch m.screen {
	case screenTree:
		return m.viewTree()
	case screenReview:
		return m.viewReview()
	case screenDownloading:
		return centerBox(m.width, m.height, "Downloading tarball and extracting...")
	case screenDone:
		return ""
	}
	return ""
}

func centerBox(w, h int, msg string) string {
	pad := strings.Repeat("\n", h/2)
	return pad + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(msg)
}

func (m *model) viewTree() string {
	contentH := m.height - 3
	if contentH < 5 {
		contentH = 5
	}
	leftW := m.width / 2
	rightW := m.width - leftW - 1

	visibleLines := contentH
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visibleLines {
		m.offset = m.cursor - visibleLines + 1
	}
	end := m.offset + visibleLines
	if end > len(m.visible) {
		end = len(m.visible)
	}

	var leftLines []string
	for i := m.offset; i < end; i++ {
		vn := m.visible[i]
		line := formatTreeLine(vn, leftW-1)
		if i == m.cursor {
			line = cursorStyle.Render(padRight(line, leftW-1))
		}
		leftLines = append(leftLines, line)
	}
	for len(leftLines) < visibleLines {
		leftLines = append(leftLines, "")
	}

	leftPane := strings.Join(leftLines, "\n")

	// Preview pane (right)
	previewHeader := ""
	if m.previewPath != "" {
		previewHeader = headerStyle.Render(" " + truncate(m.previewPath, rightW-2) + " ")
	}
	rightLines := wrapAndLimit(m.previewBody, rightW, visibleLines-1)
	rightPane := previewHeader + "\n" + strings.Join(rightLines, "\n")

	// Combine
	leftCol := lipgloss.NewStyle().Width(leftW).Render(leftPane)
	rightCol := lipgloss.NewStyle().Width(rightW).Render(rightPane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", rightCol)

	// Top header
	hdrText := fmt.Sprintf(" gripper · %s/%s @ %s ", m.owner, m.repo, m.refName)
	header := headerStyle.Width(m.width).Render(hdrText)

	// Status bar
	blobs := m.root.SelectedBlobs()
	var totalSize int64
	for _, b := range blobs {
		totalSize += b.Size
	}
	statusLeft := fmt.Sprintf(" %d selected · %s", len(blobs), humanSize(totalSize))
	keys := " space=toggle · →=expand · ←=collapse/parent · enter=review · q=quit "
	status := statusBarStyle.Width(m.width).Render(padBetween(statusLeft, keys, m.width))

	// Status message line
	msgLine := ""
	if m.statusMsg != "" {
		switch m.statusKind {
		case statusError:
			msgLine = errorStyle.Render(" ⚠ " + m.statusMsg)
		case statusWarn:
			msgLine = warnStyle.Render(" ⚠ " + m.statusMsg)
		default:
			msgLine = infoStyle.Render(" " + m.statusMsg)
		}
	}

	return header + "\n" + body + "\n" + msgLine + "\n" + status
}

func formatTreeLine(vn VisibleNode, maxW int) string {
	n := vn.Node
	indent := strings.Repeat("  ", vn.Depth)

	// expand glyph
	var glyph string
	if n.IsDir {
		if n.Expanded {
			glyph = "▾"
		} else {
			glyph = "▸"
		}
	} else {
		glyph = " "
	}

	// checkbox
	var box string
	switch n.Selected {
	case SelFull:
		box = selectedStyle.Render("[x]")
	case SelPartial:
		box = partialStyle.Render("[~]")
	default:
		box = "[ ]"
	}

	name := n.Name
	if n.IsDir {
		name = dirStyle.Render(name + "/")
	}
	size := dimStyle.Render(humanSize(n.Size))

	line := fmt.Sprintf("%s%s %s %s %s", indent, glyph, box, name, size)
	return truncate(line, maxW)
}

func (m *model) viewReview() string {
	contentH := m.height - 5
	if contentH < 5 {
		contentH = 5
	}

	collisions := Collisions(m.selections)

	// Header
	hdrText := fmt.Sprintf(" gripper · review %d selection(s) · output: %s ", len(m.selections), m.target)
	header := headerStyle.Width(m.width).Render(truncate(hdrText, m.width))

	// Compute column widths
	colArrow := " → "
	srcMaxW := 0
	tgtMaxW := 0
	for _, s := range m.selections {
		if len(s.Source) > srcMaxW {
			srcMaxW = len(s.Source)
		}
		if len(s.Target) > tgtMaxW {
			tgtMaxW = len(s.Target)
		}
	}
	available := m.width - len(colArrow) - 14 // leave room for size + collision marker
	if srcMaxW+tgtMaxW > available {
		// split evenly
		half := available / 2
		srcMaxW = half
		tgtMaxW = available - half
	}

	// Scroll
	if m.revCursor < m.revOffset {
		m.revOffset = m.revCursor
	}
	if m.revCursor >= m.revOffset+contentH {
		m.revOffset = m.revCursor - contentH + 1
	}
	end := m.revOffset + contentH
	if end > len(m.selections) {
		end = len(m.selections)
	}

	var lines []string
	for i := m.revOffset; i < end; i++ {
		s := m.selections[i]
		isEditing := m.editing && !m.editingOut && i == m.revCursor
		src := padRight(truncate(s.Source, srcMaxW), srcMaxW)
		var tgt string
		if isEditing {
			tgt = padRight(m.editInput.View(), tgtMaxW)
		} else {
			tgt = padRight(truncate(s.Target, tgtMaxW), tgtMaxW)
			if collisions[i] {
				tgt = collideStyle.Render(tgt)
			}
		}
		sz := dimStyle.Render(padLeft(humanSize(s.Size), 10))
		line := fmt.Sprintf("%s%s%s  %s", src, arrowStyle.Render(colArrow), tgt, sz)
		if collisions[i] && !isEditing {
			line += " " + collideStyle.Render("← collision")
		}
		if i == m.revCursor && !isEditing {
			line = cursorStyle.Render(padRight(line, m.width-1))
		}
		lines = append(lines, line)
	}
	for len(lines) < contentH {
		lines = append(lines, "")
	}
	body := strings.Join(lines, "\n")

	// Edit prompt (when editing output dir)
	if m.editing && m.editingOut {
		body = body + "\n\n " + headerStyle.Render(" output dir > ") + m.editInput.View()
	}

	// Status
	var totalSize int64
	for _, s := range m.selections {
		totalSize += s.Size
	}
	statusLeft := fmt.Sprintf(" %d files · %s", len(m.selections), humanSize(totalSize))
	if len(collisions) > 0 {
		statusLeft += errorStyle.Render(fmt.Sprintf(" · %d collisions", len(collisions)))
	}
	keys := " e=edit  s=strip  f=flatten  r=reset  x=remove  o=outdir  c=confirm  esc=back  q=quit "
	status := statusBarStyle.Width(m.width).Render(padBetween(statusLeft, keys, m.width))

	msgLine := ""
	if m.statusMsg != "" {
		switch m.statusKind {
		case statusError:
			msgLine = errorStyle.Render(" ⚠ " + m.statusMsg)
		case statusWarn:
			msgLine = warnStyle.Render(" ⚠ " + m.statusMsg)
		default:
			msgLine = infoStyle.Render(" " + m.statusMsg)
		}
	}

	return header + "\n" + body + "\n" + msgLine + "\n" + status
}

// ---- helpers ----

func humanSize(s int64) string {
	const (
		k = 1024
		m = 1024 * k
		g = 1024 * m
	)
	switch {
	case s < k:
		return fmt.Sprintf("%d B", s)
	case s < m:
		return fmt.Sprintf("%.1f KB", float64(s)/k)
	case s < g:
		return fmt.Sprintf("%.1f MB", float64(s)/m)
	default:
		return fmt.Sprintf("%.1f GB", float64(s)/g)
	}
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// crude byte-truncate; ANSI-safe truncation would be nicer
	if w < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	return string(runes[:w-1]) + "…"
}

func padRight(s string, w int) string {
	width := lipgloss.Width(s)
	if width >= w {
		return s
	}
	return s + strings.Repeat(" ", w-width)
}

func padLeft(s string, w int) string {
	width := lipgloss.Width(s)
	if width >= w {
		return s
	}
	return strings.Repeat(" ", w-width) + s
}

func padBetween(left, right string, w int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := w - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func wrapAndLimit(s string, w, maxLines int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for lipgloss.Width(line) > w {
			runes := []rune(line)
			cut := w
			if cut > len(runes) {
				cut = len(runes)
			}
			out = append(out, string(runes[:cut]))
			line = string(runes[cut:])
			if len(out) >= maxLines {
				return out
			}
		}
		out = append(out, line)
		if len(out) >= maxLines {
			return out
		}
	}
	return out
}
