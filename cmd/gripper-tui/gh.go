package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	Token string
	Owner string
	Repo  string
	Ref   string
	http  *http.Client
}

func NewClient(token, owner, repo, ref string) *Client {
	return &Client{
		Token: token,
		Owner: owner,
		Repo:  repo,
		Ref:   ref,
		http:  &http.Client{Timeout: 5 * time.Minute},
	}
}

type TreeEntry struct {
	Path string
	Type string
	Size int64
}

func authToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("could not run `gh auth token`: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest("GET", "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) DefaultBranch() (string, error) {
	var resp struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.get(fmt.Sprintf("/repos/%s/%s", c.Owner, c.Repo), &resp); err != nil {
		return "", err
	}
	return resp.DefaultBranch, nil
}

func (c *Client) FetchTree() ([]TreeEntry, bool, error) {
	var resp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	p := fmt.Sprintf("/repos/%s/%s/git/trees/%s?recursive=1", c.Owner, c.Repo, url.PathEscape(c.Ref))
	if err := c.get(p, &resp); err != nil {
		return nil, false, err
	}
	out := make([]TreeEntry, 0, len(resp.Tree))
	for _, e := range resp.Tree {
		out = append(out, TreeEntry{Path: e.Path, Type: e.Type, Size: e.Size})
	}
	return out, resp.Truncated, nil
}

func (c *Client) FetchFileHead(path string, maxBytes int) (body string, isBinary bool, err error) {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	escaped := strings.Join(parts, "/")
	p := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", c.Owner, c.Repo, escaped, url.PathEscape(c.Ref))
	var resp struct {
		Content string `json:"content"`
		Size    int64  `json:"size"`
	}
	if err := c.get(p, &resp); err != nil {
		return "", false, err
	}
	if resp.Content == "" {
		return "(file too large or inaccessible via contents API)", false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", false, err
	}
	if isBinaryContent(raw) {
		return "", true, nil
	}
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), false, nil
}

func isBinaryContent(b []byte) bool {
	n := len(b)
	if n > 4096 {
		n = 4096
	}
	nonPrint := 0
	for i := 0; i < n; i++ {
		c := b[i]
		if c == 0 {
			return true
		}
		if (c < 32 && c != '\t' && c != '\n' && c != '\r') || c == 127 {
			nonPrint++
		}
	}
	return nonPrint > 50
}

// DownloadAndExtract streams the tarball and writes the selected files to disk.
// targets maps source path (within the repo) -> target relative path (within outDir).
func (c *Client) DownloadAndExtract(targets map[string]string, outDir string) (int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tarball/%s", c.Owner, c.Repo, c.Ref)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("tarball: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return 0, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	written := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return written, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		src := parts[1]
		tgt, ok := targets[src]
		if !ok {
			continue
		}
		full := filepath.Join(outDir, tgt)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return written, err
		}
		f, err := os.Create(full)
		if err != nil {
			return written, err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return written, err
		}
		f.Close()
		written++
	}
	if written != len(targets) {
		return written, fmt.Errorf("extracted %d files, expected %d (missing in archive)", written, len(targets))
	}
	return written, nil
}
