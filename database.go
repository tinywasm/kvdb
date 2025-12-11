package kvdb

import (
	. "github.com/tinywasm/fmt"
)

type pair struct {
	Key   string
	Value string
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
}

// New creates or loads a database
func New(name string, log LoggerFunc, store Store) (*TinyDB, error) {
	if log == nil {
		log = func(...any) {}
	}

	db := &TinyDB{
		name:  name,
		data:  make([]pair, 0),
		log:   log,
		store: store,
		raw:   Convert(),
	}

	// try to load DB from Store
	raw, err := store.GetFile(name)
	if err == nil && len(raw) > 0 {
		lines := Convert(string(raw)).Split("\n")
		for _, line := range lines {
			if Convert(line).TrimSpace().String() == "" {
				continue
			}
			kv := Convert(line).Split("=")
			if len(kv) == 2 {
				db.data = append(db.data, pair{
					Key:   kv[0],
					Value: kv[1],
				})
			}
		}
	}

	return db, nil
}
