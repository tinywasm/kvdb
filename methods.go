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
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.dirty {
				t.persist() //nolint
				t.dirty = false
			}
			t.debounceTimer = nil
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

func (t *TinyDB) persist() error {
	t.raw.Reset()
	for _, p := range t.data {
		t.raw.Write(p.Key)
		t.raw.Write("=")
		t.raw.Write(p.Value)
		t.raw.Write("\n")
	}

	if err := t.store.SetFile(t.name, t.raw.Bytes()); err != nil {
		// log only on error
		t.log("error persisting:", err.Error())
		return err
	}

	return nil
}
