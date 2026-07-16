package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// errDoer fails every request, simulating an offline machine.
type errDoer struct{ calls atomic.Int32 }

func (d *errDoer) Do(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, errors.New("no network")
}

func newTestChecker(t *testing.T, current string) (*Checker, *atomic.Int32) {
	t.Helper()
	t.Setenv(envDisable, "")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/owner/repo/releases/tag/v0.2.0",
		})
	}))
	t.Cleanup(srv.Close)

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	c := &Checker{
		Version:   current,
		Repo:      "owner/repo",
		APIBase:   srv.URL,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		Client:    srv.Client(),
		Now:       func() time.Time { return now },
	}
	return c, &hits
}

func TestCheckNowUpdatesCache(t *testing.T) {
	c, _ := newTestChecker(t, "v0.1.0")
	rel, err := c.CheckNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.0" || !strings.Contains(rel.URL, "/releases/tag/v0.2.0") {
		t.Fatalf("release %+v", rel)
	}
	st, ok := c.readCache()
	if !ok || st.LatestTag != "v0.2.0" || !st.CheckedAt.Equal(c.Now()) {
		t.Fatalf("cache %+v ok=%v", st, ok)
	}
}

func TestBackgroundSkipsFreshCache(t *testing.T) {
	c, hits := newTestChecker(t, "v0.1.0")
	c.writeCache(cacheState{CheckedAt: c.Now().Add(-cacheTTL / 2), LatestTag: "v0.1.0"})
	c.Background().Notice(io.Discard)
	if hits.Load() != 0 {
		t.Fatalf("fresh cache still hit the network %d times", hits.Load())
	}
}

func TestBackgroundRefreshesStaleCache(t *testing.T) {
	c, hits := newTestChecker(t, "v0.1.0")
	c.writeCache(cacheState{CheckedAt: c.Now().Add(-cacheTTL - time.Minute), LatestTag: "v0.1.0"})
	var out strings.Builder
	c.Background().Notice(&out)
	if hits.Load() != 1 {
		t.Fatalf("stale cache hit the network %d times, want 1", hits.Load())
	}
	st, _ := c.readCache()
	if st.LatestTag != "v0.2.0" || !st.CheckedAt.Equal(c.Now()) {
		t.Fatalf("cache not refreshed: %+v", st)
	}
	if !strings.Contains(out.String(), "v0.1.0 -> v0.2.0") {
		t.Fatalf("notice %q", out.String())
	}
}

func TestBackgroundMissingCacheRefreshes(t *testing.T) {
	c, hits := newTestChecker(t, "v0.1.0")
	c.Background().Notice(io.Discard)
	if hits.Load() != 1 {
		t.Fatalf("network hits = %d, want 1", hits.Load())
	}
}

func TestBackgroundNetworkErrorThrottles(t *testing.T) {
	c, _ := newTestChecker(t, "v0.1.0")
	d := &errDoer{}
	c.Client = d
	old := cacheState{CheckedAt: c.Now().Add(-2 * cacheTTL), LatestTag: "v0.0.9", HTMLURL: "u"}
	c.writeCache(old)

	var out strings.Builder
	c.Background().Notice(&out)
	if out.String() != "" {
		t.Fatalf("notice on stale data: %q", out.String())
	}
	st, ok := c.readCache()
	if !ok || !st.CheckedAt.Equal(c.Now()) || st.LatestTag != "v0.0.9" {
		t.Fatalf("failed refresh should touch checkedAt and keep the old tag: %+v", st)
	}
	// The touched cache is now fresh: the next command skips the network.
	c.Background().Notice(io.Discard)
	if d.calls.Load() != 1 {
		t.Fatalf("network attempts = %d, want 1 per TTL", d.calls.Load())
	}
}

func TestNoticeFormat(t *testing.T) {
	c, _ := newTestChecker(t, "v0.1.0")
	c.writeCache(cacheState{
		CheckedAt: c.Now(),
		LatestTag: "v0.2.0",
		HTMLURL:   "https://github.com/owner/repo/releases/tag/v0.2.0",
	})
	var out strings.Builder
	c.Background().Notice(&out)
	want := "vault: update available v0.1.0 -> v0.2.0 — " +
		"https://github.com/owner/repo/releases/tag/v0.2.0 (set ALBEAR_NO_UPDATE_CHECK=1 to silence)\n"
	if out.String() != want {
		t.Fatalf("notice\n got %q\nwant %q", out.String(), want)
	}
}

func TestNoticeFallbackURL(t *testing.T) {
	c, _ := newTestChecker(t, "v0.1.0")
	c.writeCache(cacheState{CheckedAt: c.Now(), LatestTag: "v0.2.0"})
	var out strings.Builder
	c.Background().Notice(&out)
	if !strings.Contains(out.String(), "https://github.com/owner/repo/releases/latest") {
		t.Fatalf("notice %q", out.String())
	}
}

func TestNoticeSilentWhenUpToDate(t *testing.T) {
	c, _ := newTestChecker(t, "v0.2.0")
	c.writeCache(cacheState{CheckedAt: c.Now(), LatestTag: "v0.2.0"})
	var out strings.Builder
	c.Background().Notice(&out)
	if out.String() != "" {
		t.Fatalf("unexpected notice %q", out.String())
	}
}

func TestDisabledForDevAndEnv(t *testing.T) {
	c, hits := newTestChecker(t, "dev")
	var out strings.Builder
	c.Background().Notice(&out)
	if hits.Load() != 0 || out.String() != "" {
		t.Fatalf("dev build checked for updates (hits=%d, out=%q)", hits.Load(), out.String())
	}

	c, hits = newTestChecker(t, "v0.1.0")
	t.Setenv(envDisable, "1")
	c.Background().Notice(&out)
	if hits.Load() != 0 || out.String() != "" {
		t.Fatalf("%s=1 checked for updates (hits=%d, out=%q)", envDisable, hits.Load(), out.String())
	}
}

func TestNewDefaults(t *testing.T) {
	t.Setenv(envRepo, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := New("v0.1.0")
	if c.Repo != defaultRepo {
		t.Fatal(c.Repo)
	}
	if filepath.Base(c.CachePath) != cacheFile || !strings.Contains(c.CachePath, "albear") {
		t.Fatal(c.CachePath)
	}
	t.Setenv(envRepo, "other/repo")
	if c = New("v0.1.0"); c.Repo != "other/repo" {
		t.Fatal(c.Repo)
	}
}

func TestReadCacheRejectsGarbage(t *testing.T) {
	c, _ := newTestChecker(t, "v0.1.0")
	os.WriteFile(c.CachePath, []byte("not json"), 0o600)
	if _, ok := c.readCache(); ok {
		t.Fatal("garbage cache accepted")
	}
	os.WriteFile(c.CachePath, []byte("{}"), 0o600)
	if _, ok := c.readCache(); ok {
		t.Fatal("zero checkedAt accepted")
	}
}
