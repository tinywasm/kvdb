package kvdb

import (
	"os"
	"testing"
)

// mockStore is a mock implementation of the Store interface for testing.
type mockStore struct {
	files map[string][]byte
}

func newMockStore() *mockStore {
	return &mockStore{
		files: make(map[string][]byte),
	}
}

func (m *mockStore) GetFile(filePath string) ([]byte, error) {
	data, ok := m.files[filePath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *mockStore) SetFile(filePath string, data []byte) error {
	m.files[filePath] = data
	return nil
}

func (m *mockStore) AddToFile(filePath string, data []byte) error {
	m.files[filePath] = append(m.files[filePath], data...)
	return nil
}

func TestNew(t *testing.T) {
	t.Run("creates a new database if one does not exist", func(t *testing.T) {
		store := newMockStore()
		db, err := New("test.db", nil, store)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if db == nil {
			t.Fatal("expected db to be non-nil")
		}
	})

	t.Run("loads an existing database from the store", func(t *testing.T) {
		store := newMockStore()
		store.SetFile("test.db", []byte("foo=bar\nbaz=qux"))
		db, err := New("test.db", nil, store)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		val, err := db.Get("foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "bar" {
			t.Errorf("expected value 'bar', got '%s'", val)
		}

		val, err = db.Get("baz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "qux" {
			t.Errorf("expected value 'qux', got '%s'", val)
		}
	})

	t.Run("handles empty or malformed lines when loading", func(t *testing.T) {
		store := newMockStore()
		store.SetFile("test.db", []byte("foo=bar\n\nmalformed\nbaz=qux"))
		db, err := New("test.db", nil, store)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		val, err := db.Get("foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "bar" {
			t.Errorf("expected value 'bar', got '%s'", val)
		}

		val, err = db.Get("baz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "qux" {
			t.Errorf("expected value 'qux', got '%s'", val)
		}

		_, err = db.Get("malformed")
		if err == nil {
			t.Error("expected error for malformed key, got nil")
		}
	})
}
