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
		info, body, tag, err := parseContainer(data)
		if err != nil {
			return
		}
		if uint64(len(body))+uint64(len(tag)) != uint64(len(data)) {
			t.Fatal("accepted container with inconsistent framing")
		}
		if info.SnapshotLen > uint64(len(data)) {
			t.Fatal("accepted snapshot length larger than input")
		}
	})
}
