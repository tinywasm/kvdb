package kvdb

import (
	"bytes"
	"testing"
)

func TestReconcile_KeepsExternallyAddedKey(t *testing.T) {
	disk := []byte("A=1\nB=2\n")
	data := []pair{{Key: "A", Value: "9"}}
	touched := map[string]bool{"A": true}

	got := reconcile(disk, data, touched)
	want := "A=9\nB=2\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_KeepsCommentsAndBlankLines(t *testing.T) {
	disk := []byte("# header\n\nA=1\n")
	data := []pair{{Key: "A", Value: "9"}}
	touched := map[string]bool{"A": true}

	got := reconcile(disk, data, touched)
	want := "# header\n\nA=9\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_UntouchedKeyKeepsDiskValue(t *testing.T) {
	disk := []byte("A=disk\n")
	data := []pair{{Key: "A", Value: "stale"}}
	touched := map[string]bool{}

	got := reconcile(disk, data, touched)
	want := "A=disk\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_TouchedKeyWinsOverDisk(t *testing.T) {
	disk := []byte("A=disk\n")
	data := []pair{{Key: "A", Value: "new"}}
	touched := map[string]bool{"A": true}

	got := reconcile(disk, data, touched)
	want := "A=new\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_NewKeysAppendedAtEnd(t *testing.T) {
	disk := []byte("A=1\n")
	data := []pair{{Key: "A", Value: "1"}, {Key: "Z", Value: "26"}}
	touched := map[string]bool{"Z": true}

	got := reconcile(disk, data, touched)
	want := "A=1\nZ=26\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_IsIdempotent(t *testing.T) {
	disk := []byte("# comment\nA=1\n\nB=2\n")
	data := []pair{{Key: "A", Value: "9"}, {Key: "C", Value: "3"}}
	touched := map[string]bool{"A": true, "C": true}

	first := reconcile(disk, data, touched)
	second := reconcile(first, data, touched)

	if !bytes.Equal(first, second) {
		t.Errorf("reconcile is not idempotent.\nFirst:  %q\nSecond: %q", string(first), string(second))
	}
}

func TestReconcile_EmptyDisk(t *testing.T) {
	disk := []byte("")
	data := []pair{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}}
	touched := map[string]bool{"A": true, "B": true}

	got := reconcile(disk, data, touched)
	want := "A=1\nB=2\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

func TestReconcile_ValueContainsEquals(t *testing.T) {
	disk := []byte("DSN=host?a=1&b=2\n")
	data := []pair{{Key: "DSN", Value: "host?a=1&b=2"}}
	touched := map[string]bool{}

	got := reconcile(disk, data, touched)
	want := "DSN=host?a=1&b=2\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}
