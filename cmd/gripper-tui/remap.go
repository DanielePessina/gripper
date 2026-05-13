package main

import (
	"path/filepath"
	"strings"
)

type Selection struct {
	Source string
	Target string
	Size   int64
}

// StripLCP strips the longest common path-component prefix from all
// selections' Target paths (in place).
func StripLCP(sels []Selection) {
	if len(sels) < 2 {
		return
	}
	prefix := commonDirPrefix(sels)
	if prefix == "" {
		return
	}
	prefix += "/"
	for i := range sels {
		sels[i].Target = strings.TrimPrefix(sels[i].Target, prefix)
	}
}

func commonDirPrefix(sels []Selection) string {
	parts0 := strings.Split(sels[0].Target, "/")
	if len(parts0) <= 1 {
		return ""
	}
	common := parts0[:len(parts0)-1]
	for _, s := range sels[1:] {
		p := strings.Split(s.Target, "/")
		if len(p) <= 1 {
			return ""
		}
		p = p[:len(p)-1]
		n := len(common)
		if len(p) < n {
			n = len(p)
		}
		i := 0
		for i < n && common[i] == p[i] {
			i++
		}
		common = common[:i]
		if len(common) == 0 {
			return ""
		}
	}
	return strings.Join(common, "/")
}

func Flatten(sels []Selection) {
	for i := range sels {
		sels[i].Target = filepath.Base(sels[i].Target)
	}
}

func ResetTargets(sels []Selection) {
	for i := range sels {
		sels[i].Target = sels[i].Source
	}
}

// Collisions returns the set of indices whose Target is shared by another
// selection.
func Collisions(sels []Selection) map[int]bool {
	by := map[string][]int{}
	for i, s := range sels {
		by[s.Target] = append(by[s.Target], i)
	}
	out := map[int]bool{}
	for _, ixs := range by {
		if len(ixs) > 1 {
			for _, i := range ixs {
				out[i] = true
			}
		}
	}
	return out
}
