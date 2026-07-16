// Package update performs the GitHub release check behind the CLI's passive
// update notice. It is cache-first so normal commands pay no network latency:
// a JSON cache under the config dir is read synchronously and refreshed by a
// short-timeout background goroutine at most once per TTL. Network failures
// never fail a command.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/system"
	"github.com/m7medVision/albear/internal/version"
)

// defaultRepo is the ONE place the GitHub repository slug lives in the Go
// code. ALBEAR_UPDATE_REPO overrides it at runtime.
const defaultRepo = "m7medVision/albear"

// HTTPTimeout bounds every release lookup, background or synchronous.
const HTTPTimeout = 2 * time.Second

const (
	cacheFile  = "update-check.json"
	cacheTTL   = 24 * time.Hour
	envRepo    = "ALBEAR_UPDATE_REPO"
	envDisable = "ALBEAR_NO_UPDATE_CHECK"
)

// Release is the newest published release as reported by GitHub.
type Release struct {
	Tag string
	URL string
}

// cacheState is the persisted check result.
type cacheState struct {
	CheckedAt time.Time `json:"checkedAt"`
	LatestTag string    `json:"latestTag"`
	HTMLURL   string    `json:"htmlURL"`
}

// Doer is the slice of *http.Client the checker needs; tests inject failures
// through it.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Checker looks up the latest GitHub release and caches the answer. Fields
// are exported so tests can inject a clock, transport, and cache location.
type Checker struct {
	Version   string
	Repo      string
	APIBase   string
	CachePath string
	Client    Doer
	Now       func() time.Time
}

// New builds a Checker for the running binary version with production
// defaults (GitHub API, 2s timeout, cache under the albear config dir).
func New(current string) *Checker {
	repo := os.Getenv(envRepo)
	if repo == "" {
		repo = defaultRepo
	}
	cachePath := ""
	if paths, err := system.ResolvePaths(); err == nil {
		cachePath = filepath.Join(paths.ConfigDir, cacheFile)
	}
	return &Checker{
		Version:   current,
		Repo:      repo,
		APIBase:   "https://api.github.com",
		CachePath: cachePath,
		Client:    &http.Client{Timeout: HTTPTimeout},
		Now:       time.Now,
	}
}

// Enabled reports whether update checks run at all: never for dev builds,
// never when ALBEAR_NO_UPDATE_CHECK is set, never without a cache location.
func (c *Checker) Enabled() bool {
	return os.Getenv(envDisable) == "" && c.Version != "dev" && c.CachePath != ""
}

// CheckNow queries GitHub synchronously and refreshes the cache. Callers own
// the timeout via ctx.
func (c *Checker) CheckNow(ctx context.Context) (Release, error) {
	url := c.APIBase + "/repos/" + c.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: GitHub responded %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, err
	}
	if body.TagName == "" {
		return Release{}, errors.New("update: release has no tag_name")
	}
	c.writeCache(cacheState{CheckedAt: c.Now(), LatestTag: body.TagName, HTMLURL: body.HTMLURL})
	return Release{Tag: body.TagName, URL: body.HTMLURL}, nil
}

// Handle tracks an optional in-flight background refresh.
type Handle struct {
	c    *Checker
	done chan struct{}
}

// Background starts a cache refresh goroutine when the cached check is older
// than the TTL, overlapping the network round-trip with the command's own
// work. It returns immediately.
func (c *Checker) Background() *Handle {
	h := &Handle{c: c, done: make(chan struct{})}
	st, ok := c.readCache()
	if !c.Enabled() || (ok && c.Now().Sub(st.CheckedAt) < cacheTTL) {
		close(h.done)
		return h
	}
	go func() {
		defer close(h.done)
		ctx, cancel := context.WithTimeout(context.Background(), HTTPTimeout)
		defer cancel()
		if _, err := c.CheckNow(ctx); err != nil {
			// Throttle: record the attempt so offline machines retry at most
			// once per TTL instead of stalling every command.
			st.CheckedAt = c.Now()
			c.writeCache(st)
		}
	}()
	return h
}

// Notice prints the passive update hint to w when the cache knows a release
// newer than the running version. It waits for any in-flight refresh, which
// HTTPTimeout bounds, so a stale-cache command exits at most ~2s late and
// every other command exits immediately.
func (h *Handle) Notice(w io.Writer) {
	<-h.done
	c := h.c
	if !c.Enabled() {
		return
	}
	st, ok := c.readCache()
	if !ok || !version.IsNewer(st.LatestTag, c.Version) {
		return
	}
	url := st.HTMLURL
	if url == "" {
		url = "https://github.com/" + c.Repo + "/releases/latest"
	}
	fmt.Fprintln(w, noticeLine(c.Version, st.LatestTag, url))
}

func noticeLine(current, latest, url string) string {
	return fmt.Sprintf("vault: update available %s -> %s — %s (set %s=1 to silence)",
		current, latest, url, envDisable)
}

func (c *Checker) readCache() (cacheState, bool) {
	raw, err := os.ReadFile(c.CachePath)
	if err != nil {
		return cacheState{}, false
	}
	var st cacheState
	if json.Unmarshal(raw, &st) != nil || st.CheckedAt.IsZero() {
		return cacheState{}, false
	}
	return st, true
}

// writeCache is best-effort: the cache is an optimization, never a failure.
func (c *Checker) writeCache(st cacheState) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.CachePath), 0o700); err != nil {
		return
	}
	os.WriteFile(c.CachePath, raw, 0o600)
}
