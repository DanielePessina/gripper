package main

import (
	"sort"
	"strings"
)

type SelState int

const (
	SelNone SelState = iota
	SelFull
	SelPartial
)

type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Size     int64
	Children []*Node
	Parent   *Node

	Expanded bool
	Selected SelState
}

type VisibleNode struct {
	*Node
	Depth int
}

func BuildTree(entries []TreeEntry) *Node {
	root := &Node{IsDir: true, Expanded: true, Name: "", Path: ""}
	byPath := map[string]*Node{"": root}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	for _, e := range entries {
		parts := strings.Split(e.Path, "/")
		name := parts[len(parts)-1]
		parentPath := strings.Join(parts[:len(parts)-1], "/")
		parent, ok := byPath[parentPath]
		if !ok {
			continue
		}
		n := &Node{
			Name:   name,
			Path:   e.Path,
			IsDir:  e.Type == "tree",
			Parent: parent,
		}
		if !n.IsDir {
			n.Size = e.Size
		}
		parent.Children = append(parent.Children, n)
		byPath[e.Path] = n
	}

	root.aggregateSize()
	root.sortChildren()
	return root
}

func (n *Node) aggregateSize() int64 {
	if !n.IsDir {
		return n.Size
	}
	var s int64
	for _, c := range n.Children {
		s += c.aggregateSize()
	}
	n.Size = s
	return s
}

func (n *Node) sortChildren() {
	sort.Slice(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return a.Name < b.Name
	})
	for _, c := range n.Children {
		c.sortChildren()
	}
}

func (n *Node) Toggle() {
	target := SelFull
	if n.Selected == SelFull {
		target = SelNone
	}
	n.setSelected(target)
	if n.Parent != nil {
		n.Parent.recomputeSelection()
	}
}

func (n *Node) setSelected(s SelState) {
	n.Selected = s
	for _, c := range n.Children {
		c.setSelected(s)
	}
}

func (n *Node) recomputeSelection() {
	full := 0
	partial := 0
	none := 0
	for _, c := range n.Children {
		switch c.Selected {
		case SelFull:
			full++
		case SelPartial:
			partial++
		case SelNone:
			none++
		}
	}
	if full == len(n.Children) && partial == 0 {
		n.Selected = SelFull
	} else if none == len(n.Children) {
		n.Selected = SelNone
	} else {
		n.Selected = SelPartial
	}
	if n.Parent != nil {
		n.Parent.recomputeSelection()
	}
}

func (n *Node) SelectedBlobs() []*Node {
	var out []*Node
	n.walk(func(x *Node) {
		if !x.IsDir && x.Selected == SelFull {
			out = append(out, x)
		}
	})
	return out
}

func (n *Node) walk(fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		c.walk(fn)
	}
}

func (n *Node) Visible() []VisibleNode {
	var out []VisibleNode
	for _, c := range n.Children {
		c.collectVisible(&out, 0)
	}
	return out
}

func (n *Node) collectVisible(out *[]VisibleNode, depth int) {
	*out = append(*out, VisibleNode{n, depth})
	if n.IsDir && n.Expanded {
		for _, c := range n.Children {
			c.collectVisible(out, depth+1)
		}
	}
}

func (n *Node) Find(path string) *Node {
	if n.Path == path {
		return n
	}
	for _, c := range n.Children {
		if r := c.Find(path); r != nil {
			return r
		}
	}
	return nil
}
