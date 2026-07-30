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
	mu          sync.RWMutex
	entries     map[shared.ID]*IndexEntry
	byHost      map[string][]shared.ID // registrable domain → record IDs
	byProjectID map[string][]shared.ID // ProjectID → record IDs
}

func NewIndex() *Index {
	return &Index{
		entries:     make(map[shared.ID]*IndexEntry),
		byHost:      make(map[string][]shared.ID),
		byProjectID: make(map[string][]shared.ID),
	}
}

func (ix *Index) Put(e *IndexEntry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if old, ok := ix.entries[e.ID]; ok {
		ix.removeIndexesLocked(old)
	}
	ix.entries[e.ID] = e
	for _, u := range e.Metadata.URLs {
		d := u.Origin.RegistrableDomain()
		ix.byHost[d] = append(ix.byHost[d], e.ID)
	}
	if e.Metadata.ProjectID != "" {
		ix.byProjectID[e.Metadata.ProjectID] = append(ix.byProjectID[e.Metadata.ProjectID], e.ID)
	}
}

func (ix *Index) Remove(id shared.ID) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if e, ok := ix.entries[id]; ok {
		ix.removeIndexesLocked(e)
		delete(ix.entries, id)
	}
}

// removeIndexesLocked drops e from every secondary lookup (byHost,
// byProjectID) ahead of a Put replacing it or a Remove deleting it outright.
func (ix *Index) removeIndexesLocked(e *IndexEntry) {
	for _, u := range e.Metadata.URLs {
		removeFromMultimap(ix.byHost, u.Origin.RegistrableDomain(), e.ID)
	}
	if e.Metadata.ProjectID != "" {
		removeFromMultimap(ix.byProjectID, e.Metadata.ProjectID, e.ID)
	}
}

// removeFromMultimap drops id from m[key], deleting the key entirely once its
// slice empties out — the shape shared by every secondary index here.
func removeFromMultimap(m map[string][]shared.ID, key string, id shared.ID) {
	ids := m[key]
	for i, existing := range ids {
		if existing == id {
			m[key] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(m[key]) == 0 {
		delete(m, key)
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

// MatchReason says why an entry was returned by Match — the real, verified
// origin, or (loopback-only) a page-supplied project tag. It is computed here
// from the record's own data, never taken from the caller, so the UI can show
// the two apart without the daemon trusting anything it wasn't already sure of.
type MatchReason string

const (
	MatchedByOrigin  MatchReason = "origin"
	MatchedByProject MatchReason = "project"
)

// MatchResult pairs an entry with why it matched. A record matching both ways
// reports MatchedByOrigin — the strictly verified signal takes priority over
// the loopback-only carve-out.
type MatchResult struct {
	Entry  *IndexEntry
	Reason MatchReason
}

// Match returns entries that match the page origin, plus — on a loopback
// origin with a project id supplied — entries whose ProjectID matches it too.
// The host index narrows origin candidates to the registrable domain first,
// then the full origin check runs (exact host match target: p95 < 10 ms). A
// record is reported once even if it matches both ways.
func (ix *Index) Match(page domain.CanonicalOrigin, projectID string) []MatchResult {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	seen := make(map[shared.ID]bool)
	var out []MatchResult
	for _, id := range ix.byHost[page.RegistrableDomain()] {
		e := ix.entries[id]
		if e == nil || seen[id] {
			continue
		}
		for _, u := range e.Metadata.URLs {
			if u.Matches(page) {
				out = append(out, MatchResult{Entry: e, Reason: MatchedByOrigin})
				seen[id] = true
				break
			}
		}
	}
	if projectID != "" {
		for _, id := range ix.byProjectID[projectID] {
			if seen[id] {
				continue
			}
			e := ix.entries[id]
			if e == nil {
				continue
			}
			rec := &domain.Record{ID: e.ID, Type: e.Type, Revision: e.Revision, Metadata: e.Metadata}
			if rec.MatchesProjectID(page, projectID) {
				out = append(out, MatchResult{Entry: e, Reason: MatchedByProject})
				seen[id] = true
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.Metadata.Name < out[j].Entry.Metadata.Name })
	return out
}

// Clear destroys the index contents on lock.
func (ix *Index) Clear() {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	clear(ix.entries)
	clear(ix.byHost)
	clear(ix.byProjectID)
}
