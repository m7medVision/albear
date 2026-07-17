package application

import "testing"

// FuzzParseContainer: backup parsing must never panic and must reject any
// container whose declared lengths disagree with the actual bytes (PRD 26.4).
func FuzzParseContainer(f *testing.F) {
	valid := append([]byte("ALBEARBK"), make([]byte, 100)...)
	f.Add(valid)
	f.Add([]byte("ALBEARBK"))
	f.Add([]byte{})
	f.Add(make([]byte, 200))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := parseContainer(data)
		if err != nil {
			return
		}
		if uint64(len(c.body))+uint64(len(c.tag)) != uint64(len(data)) {
			t.Fatal("accepted container with inconsistent framing")
		}
		if c.info.SnapshotLen > uint64(len(data)) {
			t.Fatal("accepted snapshot length larger than input")
		}
		// The snapshot must sit inside the bytes the HMAC covers: anything
		// outside body is unauthenticated, and restore installs this slice.
		if uint64(len(c.snapshot)) != c.info.SnapshotLen {
			t.Fatal("snapshot length disagrees with the header")
		}
		if len(c.body) != containerHeaderLen+len(c.snapshot) {
			t.Fatal("snapshot is not wholly inside the authenticated body")
		}
	})
}
