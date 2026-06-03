package kvdb

import (
	"testing"
	"time"
)

// countingStore wraps mockStore and counts how many times SetFile is called.
type countingStore struct {
	mockStore
	setFileCount int
}

func (c *countingStore) SetFile(path string, data []byte) error {
	c.setFileCount++
	return c.mockStore.SetFile(path, data)
}

// TestDebounceCoalescesRapidWrites verifies that with debounce enabled, N rapid
// Set() calls produce only 1 disk write instead of N.
func TestDebounceCoalescesRapidWrites(t *testing.T) {
	cs := &countingStore{mockStore: *newMockStore()}
	cs.SetFile("test.env", []byte("browser_position=0,0\nbrowser_size=900,700\ndev_mode=true\n"))
	db, _ := New("test.env", nil, cs)
	cs.setFileCount = 0

	// 3 rapid Set() calls — same pattern as devbrowser.SaveGeometry per tick.
	db.Set("browser_position", "300,400")
	db.Set("browser_size", "1024,768")
	db.Set("browser_position", "300,400")

	// In-memory values must be immediately correct (no wait needed).
	pos, _ := db.Get("browser_position")
	if pos != "300,400" {
		t.Errorf("in-memory position: got %q, want %q", pos, "300,400")
	}

	// No disk write should have happened yet (debounce window still open).
	if cs.setFileCount != 0 {
		t.Errorf("premature write: got %d writes before debounce fired, want 0", cs.setFileCount)
	}

	// Wait for the default debounce (150ms) to flush.
	time.Sleep(250 * time.Millisecond)

	if cs.setFileCount != 1 {
		t.Errorf("debounce: got %d disk writes for 3 rapid Set() calls, want 1", cs.setFileCount)
	}

	// Values must be persisted after flush.
	size, _ := db.Get("browser_size")
	if size != "1024,768" {
		t.Errorf("browser_size after flush: got %q, want %q", size, "1024,768")
	}
}

// TestFlushWritesPendingState verifies that Flush() forces an immediate disk
// write even if the debounce timer has not fired yet.
func TestFlushWritesPendingState(t *testing.T) {
	cs := &countingStore{mockStore: *newMockStore()}
	cs.SetFile("test.env", []byte("browser_position=0,0\nbrowser_size=900,700\n"))
	db, _ := New("test.env", nil, cs)
	cs.setFileCount = 0

	db.debounceDelay = 5 * time.Second // override for test: long delay so timer won't fire

	db.Set("browser_position", "500,200")
	db.Set("browser_size", "1920,1080")

	if cs.setFileCount != 0 {
		t.Errorf("unexpected write before Flush: %d", cs.setFileCount)
	}

	if err := db.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	if cs.setFileCount != 1 {
		t.Errorf("Flush(): got %d writes, want 1", cs.setFileCount)
	}

	pos, _ := db.Get("browser_position")
	if pos != "500,200" {
		t.Errorf("position after Flush: got %q, want %q", pos, "500,200")
	}
}

// TestRapidSetsPreserveAllValues verifies that all values are correctly
// persisted after rapid consecutive Set() calls (no debounce).
func TestRapidSetsPreserveAllValues(t *testing.T) {
	cs := &countingStore{mockStore: *newMockStore()}
	cs.SetFile("test.env", []byte("browser_position=0,0\nbrowser_size=900,700\ndev_mode=true\n"))
	db, _ := New("test.env", nil, cs)
	cs.setFileCount = 0

	db.Set("browser_position", "300,400")
	db.Set("browser_size", "1024,768")
	db.Set("browser_position", "300,400")

	pos, err := db.Get("browser_position")
	if err != nil || pos != "300,400" {
		t.Errorf("browser_position: got %q (err %v), want %q", pos, err, "300,400")
	}

	size, err := db.Get("browser_size")
	if err != nil || size != "1024,768" {
		t.Errorf("browser_size: got %q (err %v), want %q", size, err, "1024,768")
	}

	t.Logf("INFO: %d writes for 3 Set() calls without debounce (performance issue, not a bug).", cs.setFileCount)
}
