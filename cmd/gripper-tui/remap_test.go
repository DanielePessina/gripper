package main

import "testing"

func TestSanitizeOutDir(t *testing.T) {
	cases := map[string]string{
		"":            ".",
		" ":           ".",
		".":           ".",
		"./":          ".",
		"./skills":    "skills",
		"./skills/":   "skills",
		"skills":      "skills",
		"skills/":     "skills",
		"./a/b/":      "a/b",
		"./a//b":      "a/b",
		"/tmp/foo":    "/tmp/foo",
		"/tmp/foo/":   "/tmp/foo",
		"~/x":         "~/x",
		"../sibling":  "../sibling",
		"./a/../b":    "b",
	}
	for in, want := range cases {
		got := SanitizeOutDir(in)
		if got != want {
			t.Errorf("SanitizeOutDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeTarget(t *testing.T) {
	okCases := map[string]string{
		"foo.txt":         "foo.txt",
		"a/b/c.txt":       "a/b/c.txt",
		"./a/b":           "a/b",
		"a//b":            "a/b",
		"a/./b":           "a/b",
		"a/b/../c":        "a/c",
		"/etc/passwd":     "etc/passwd",
	}
	for in, want := range okCases {
		got, err := SanitizeTarget(in)
		if err != nil {
			t.Errorf("SanitizeTarget(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("SanitizeTarget(%q) = %q, want %q", in, got, want)
		}
	}

	badCases := []string{
		"",
		" ",
		"..",
		"../foo",
		"./..",
		"a/b/../../../etc",
		".",
		"./",
	}
	for _, in := range badCases {
		if _, err := SanitizeTarget(in); err == nil {
			t.Errorf("SanitizeTarget(%q) accepted; want rejected", in)
		}
	}
}

func TestStripLCP(t *testing.T) {
	sels := []Selection{
		{Source: "a/b/x.txt", Target: "a/b/x.txt"},
		{Source: "a/b/c/y.txt", Target: "a/b/c/y.txt"},
		{Source: "a/b/c/d/z.txt", Target: "a/b/c/d/z.txt"},
	}
	StripLCP(sels)
	want := []string{"x.txt", "c/y.txt", "c/d/z.txt"}
	for i, w := range want {
		if sels[i].Target != w {
			t.Errorf("sels[%d].Target = %q, want %q", i, sels[i].Target, w)
		}
	}
}

func TestStripLCPNoCommon(t *testing.T) {
	sels := []Selection{
		{Source: "a/x", Target: "a/x"},
		{Source: "b/y", Target: "b/y"},
	}
	StripLCP(sels)
	if sels[0].Target != "a/x" || sels[1].Target != "b/y" {
		t.Errorf("expected no change, got %v", sels)
	}
}

func TestFlatten(t *testing.T) {
	sels := []Selection{
		{Target: "a/b/c.txt"},
		{Target: "x.go"},
	}
	Flatten(sels)
	if sels[0].Target != "c.txt" || sels[1].Target != "x.go" {
		t.Errorf("flatten failed: %v", sels)
	}
}

func TestCollisions(t *testing.T) {
	sels := []Selection{
		{Target: "a"},
		{Target: "b"},
		{Target: "a"},
		{Target: "c"},
		{Target: "b"},
	}
	col := Collisions(sels)
	want := map[int]bool{0: true, 1: true, 2: true, 4: true}
	if len(col) != len(want) {
		t.Fatalf("collisions count = %d, want %d (%v)", len(col), len(want), col)
	}
	for k := range want {
		if !col[k] {
			t.Errorf("expected index %d in collisions", k)
		}
	}
}
