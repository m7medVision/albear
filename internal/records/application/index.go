package application

import (
	"sort"
	"strings"
	"sync"

	domain "github.com/m7medVision/albear/internal/records/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// IndexEntry is one record's decrypted, searchable metadata.
type IndexEntry struct {
	ID       shared.ID
	Type     domain.RecordType
	Revision uint64
	Metadata domain.RecordMetadata
}

// Index is the in-memory metadata index built at unlock and destroyed at lock
// (PRD 17.5). Secrets never enter it.
type Index struct {
	mu      sync.RWMutex
	entries map[shared.ID]*IndexEntry
	byHost  map[string][]shared.ID // registrable domain → record IDs
}

func NewIndex() *Index {
	return &Index{
		entries: make(map[shared.ID]*IndexEntry),
		byHost:  make(map[string][]shared.ID),
	}
}

func (ix *Index) Put(e *IndexEntry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if old, ok := ix.entries[e.ID]; ok {
		ix.removeHostsLocked(old)
	}
	ix.entries[e.ID] = e
	for _, u := range e.Metadata.URLs {
		d := u.Origin.RegistrableDomain()
		ix.byHost[d] = append(ix.byHost[d], e.ID)
	}
}

func (ix *Index) Remove(id shared.ID) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if e, ok := ix.entries[id]; ok {
		ix.removeHostsLocked(e)
		delete(ix.entries, id)
	}
}

func (ix *Index) removeHostsLocked(e *IndexEntry) {
	for _, u := range e.Metadata.URLs {
		d := u.Origin.RegistrableDomain()
		ids := ix.byHost[d]
		for i, id := range ids {
			if id == e.ID {
				ix.byHost[d] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(ix.byHost[d]) == 0 {
			delete(ix.byHost, d)
		}
	}
}

func (ix *Index) Get(id shared.ID) (*IndexEntry, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, ok := ix.entries[id]
	return e, ok
}

func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.entries)
}

// All returns every entry sorted by name.
func (ix *Index) All() []*IndexEntry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]*IndexEntry, 0, len(ix.entries))
	for _, e := range ix.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

// Search does case-insensitive token matching over names, usernames, tags,
// services, and hosts. Secrets are not indexed and cannot match.
func (ix *Index) Search(query string) []*IndexEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ix.All()
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []*IndexEntry
	for _, e := range ix.entries {
		if matchesQuery(e, q) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

func matchesQuery(e *IndexEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.Metadata.Name), q) ||
		strings.Contains(strings.ToLower(e.Metadata.Username), q) ||
		strings.Contains(strings.ToLower(e.Metadata.Service), q) {
		return true
	}
	for _, tag := range e.Metadata.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	for _, u := range e.Metadata.URLs {
		if strings.Contains(u.Origin.Host, q) {
			return true
		}
	}
	return false
}

// Match returns entries whose URLs match the page origin under the canonical
// policy. The host index narrows candidates to the registrable domain first,
// then the full origin check runs (exact host match target: p95 < 10 ms).
func (ix *Index) Match(page domain.CanonicalOrigin) []*IndexEntry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []*IndexEntry
	for _, id := range ix.byHost[page.RegistrableDomain()] {
		e := ix.entries[id]
		if e == nil {
			continue
		}
		for _, u := range e.Metadata.URLs {
			if u.Origin.Matches(page) {
				out = append(out, e)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

// Clear destroys the index contents on lock.
func (ix *Index) Clear() {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	clear(ix.entries)
	clear(ix.byHost)
}
