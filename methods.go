package kvdb

import (
	"time"

	. "github.com/tinywasm/fmt"
)

func (t *TinyDB) Get(key string) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, p := range t.data {
		if p.Key == key {
			return p.Value, nil
		}
	}
	return "", Err("key not found: ", key)
}

func (t *TinyDB) Set(key, value string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// search if it exists
	for i, p := range t.data {
		if p.Key == key {
			t.data[i].Value = value
			return t.schedulePersist()
		}
	}

	// insert new
	newPair := pair{Key: key, Value: value}
	t.data = append(t.data, newPair)
	return t.append(newPair)
}

// schedulePersist either writes immediately (no debounce) or defers the write.
// Must be called with t.mu held.
func (t *TinyDB) schedulePersist() error {
	if t.debounceDelay == 0 {
		return t.persist()
	}
	t.dirty = true
	if t.debounceTimer == nil {
		t.debounceTimer = time.AfterFunc(t.debounceDelay, func() {
			// Snapshot data under lock, then write to disk outside the lock.
			// This keeps the lock window minimal so Get/Set calls are not
			// blocked during the (potentially slow) disk I/O.
			t.mu.Lock()
			if !t.dirty {
				t.debounceTimer = nil
				t.mu.Unlock()
				return
			}
			data := t.snapshot()
			t.dirty = false
			t.debounceTimer = nil
			t.mu.Unlock()

			if err := t.store.SetFile(t.name, data); err != nil {
				t.log("error persisting:", err.Error())
			}
		})
	}
	return nil
}

func (t *TinyDB) append(p pair) error {
	t.raw.Reset()
	t.raw.Write(p.Key)
	t.raw.Write("=")
	t.raw.Write(p.Value)
	t.raw.Write("\n")

	if err := t.store.AddToFile(t.name, t.raw.Bytes()); err != nil {
		// log only on error
		t.log("error appending:", err.Error())
		return err
	}

	return nil
}

// snapshot builds the serialized bytes from current in-memory data.
// Must be called with t.mu held.
func (t *TinyDB) snapshot() []byte {
	t.raw.Reset()
	for _, p := range t.data {
		t.raw.Write(p.Key)
		t.raw.Write("=")
		t.raw.Write(p.Value)
		t.raw.Write("\n")
	}
	out := make([]byte, len(t.raw.Bytes()))
	copy(out, t.raw.Bytes())
	return out
}

func (t *TinyDB) persist() error {
	data := t.snapshot()
	if err := t.store.SetFile(t.name, data); err != nil {
		t.log("error persisting:", err.Error())
		return err
	}
	return nil
}
