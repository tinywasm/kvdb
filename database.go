package kvdb

import (
	"sync"

	. "github.com/tinywasm/fmt"
	. "github.com/tinywasm/time"
)

type pair struct {
	Key   string
	Value string
}

// splitOnFirstEquals splits a string on the first '=' character.
// Returns (key, value). If no '=' is found, returns ("", "").
func splitOnFirstEquals(s string) (key, value string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:]
		}
	}
	return "", ""
}

// LoggerFunc is a simple logger that accepts any values (like fmt.Println).
// Use a nil LoggerFunc when you want no-op logging; New will set a safe default.
type LoggerFunc func(...any)

type TinyDB struct {
	name  string
	data  []pair
	log   LoggerFunc
	store Store

	raw *Conv
	mu  sync.RWMutex

	debounceDelay int   // milliseconds
	debounceTimer Timer // from github.com/tinywasm/time
	dirty         bool
}

// defaultDebounce is the write delay applied automatically (milliseconds).
// Consecutive Set() calls within this window are coalesced into a single disk write.
const defaultDebounce = 150

// Flush writes any pending debounced state to disk immediately.
// Call before process exit when debounce is enabled.
func (t *TinyDB) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.dirty {
		return nil
	}
	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
		t.debounceTimer = nil
	}
	t.dirty = false
	return t.persist()
}

// New creates or loads a database
func New(name string, log LoggerFunc, store Store) (*TinyDB, error) {
	if log == nil {
		log = func(...any) {}
	}

	db := &TinyDB{
		name:          name,
		data:          make([]pair, 0),
		log:           log,
		store:         store,
		raw:           Convert(),
		debounceDelay: defaultDebounce,
	}

	// try to load DB from Store
	raw, err := store.GetFile(name)
	if err == nil && len(raw) > 0 {
		lines := Convert(string(raw)).Split("\n")
		for _, line := range lines {
			if Convert(line).TrimSpace().String() == "" {
				continue
			}
			// Split only on the first '=' to handle values containing '='
			// (e.g. POSTGRES_DSN with query parameters like ?sslmode=disable).
			trimmed := Convert(line).TrimSpace().String()
			key, val := splitOnFirstEquals(trimmed)
			if key != "" {
				db.data = append(db.data, pair{
					Key:   key,
					Value: val,
				})
			}
		}
	}

	return db, nil
}
