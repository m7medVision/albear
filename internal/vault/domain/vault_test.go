package domain

import "testing"

func TestVaultStateTransitions(t *testing.T) {
	v := &Vault{State: StateLocked}
	if v.IsUnlocked() {
		t.Fatal("new vault must start locked")
	}
	e0 := v.Epoch
	v.Unlock()
	if !v.IsUnlocked() || v.Epoch != e0+1 {
		t.Fatal("unlock must set state and bump epoch")
	}
	v.Lock()
	if v.IsUnlocked() || v.Epoch != e0+2 {
		t.Fatal("lock must set state and bump epoch")
	}
}

func TestEpochMonotonic(t *testing.T) {
	v := &Vault{}
	last := v.Epoch
	for i := 0; i < 10; i++ {
		v.Unlock()
		if v.Epoch <= last {
			t.Fatal("epoch not monotonic")
		}
		last = v.Epoch
		v.Lock()
		if v.Epoch <= last {
			t.Fatal("epoch not monotonic")
		}
		last = v.Epoch
	}
}
