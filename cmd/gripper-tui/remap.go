package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizeOutDir cleans an output directory string. Empty input becomes ".".
func SanitizeOutDir(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "."
	}
	return filepath.Clean(s)
}

// SanitizeTarget normalises a per-row target path. It must be relative and
// must not escape the output directory via `..`. Returns the cleaned path,
// or an error describing why the input was rejected.
func SanitizeTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty target")
	}
	cleaned := filepath.Clean(s)
	if filepath.IsAbs(cleaned) {
		cleaned = strings.TrimLeft(cleaned, string(filepath.Separator))
		cleaned = filepath.Clean(cleaned)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target escapes output dir: %s", s)
	}
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("target resolves to output dir itself")
	}
	return cleaned, nil
}

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
