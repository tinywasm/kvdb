---
PLAN: "fix: never clobber external edits to the backing file"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 2200519370399796353
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — persist must reconcile with the file on disk, not overwrite it

## 1. The defect

`TinyDB` loads the backing file **once**, in `New` ([database.go:79-98](../database.go)),
and every later write rewrites the **whole file** from the in-memory snapshot:

```go
func (t *TinyDB) persist() error {
	data := t.snapshot()                 // serialized from t.data only
	if err := t.store.SetFile(t.name, data); err != nil {
```

The debounced path in `schedulePersist` ([methods.go](../methods.go)) does the same with
`t.store.SetFile(t.name, data)`.

Three things are destroyed by that write, silently:

1. **Keys added to the file externally after load.** They are not in `t.data`, so they
   vanish on the next `Set` of any other key.
2. **Comment lines and blank lines.** `New` skips blank lines, and `splitOnFirstEquals`
   returns `("", "")` for a line with no `=` (a `# comment`), which the loader drops
   because `key == ""`. `snapshot()` then re-emits only `key=value` pairs, so every
   comment in the file is gone after the first write.
3. **Values edited externally.** A hand edit is reverted to the value loaded at startup.

This is not theoretical. The main consumer (`tinywasm/app`) backs each project's `.env`
with this store and runs as a long-lived daemon: the daemon writes UI state keys
(`browser_position`, `browser_size`) whenever the developer moves the dev browser window,
so every hand edit to that `.env` made while the daemon runs is discarded on the next
window move.

## 2. The rule this plan installs

**A write must only change the keys this process actually wrote.** Everything else in the
file — unknown keys, comments, blank lines, key order — survives byte-for-byte.

## 3. Repo constraints (read before writing code)

- **No standard library.** This package compiles for WASM. `database.go` and `methods.go`
  use `github.com/tinywasm/fmt` (dot-imported: `Convert`, `Err`) and
  `github.com/tinywasm/time` (`Timer`, `AfterFunc`). `sync` is the only stdlib package in
  use and is allowed. Do **NOT** import `strings`, `strconv`, `errors`, `fmt`, `os`, `bytes`
  or `time`. Split a string with `Convert(s).Split("\n")`, trim with
  `Convert(s).TrimSpace().String()`, build errors with `Err(...)` — exactly as the existing
  code does.
- **Every repeated string is a named constant.** No string literal in logic.
- Public API additions must keep `KVStore` (in [interfaces.go](../interfaces.go)) unchanged
  — see §6.

## 4. Stage 1 — failing tests first (TDD)

Write these before any production change. Each must fail against `main`.

New file `reconcile_test.go` (package `kvdb`, unit tests on the pure function):

| Test | Asserts |
|---|---|
| `TestReconcile_KeepsExternallyAddedKey` | disk has `A=1\nB=2\n`, memory knows only `A` (touched, value `9`) → output contains `A=9` **and** `B=2` |
| `TestReconcile_KeepsCommentsAndBlankLines` | disk `# header\n\nA=1\n` with `A` touched → output is `# header\n\nA=9\n`, the comment and the blank line in their original positions |
| `TestReconcile_UntouchedKeyKeepsDiskValue` | disk `A=disk\n`, memory holds `A=stale` untouched → output is `A=disk\n` |
| `TestReconcile_TouchedKeyWinsOverDisk` | disk `A=disk\n`, memory holds `A=new` touched → output is `A=new\n` |
| `TestReconcile_NewKeysAppendedAtEnd` | disk `A=1\n`, memory adds touched `Z=26` → output is `A=1\nZ=26\n` |
| `TestReconcile_IsIdempotent` | `reconcile(reconcile(d, p, t), p, t)` equals `reconcile(d, p, t)` — no growing blank lines, no duplicated keys |
| `TestReconcile_EmptyDisk` | disk empty, two touched keys → both emitted in insertion order, each ending in `\n` |
| `TestReconcile_ValueContainsEquals` | disk `DSN=host?a=1&b=2\n` untouched → byte-identical output |

Additions to `methods_test.go` (integration through a `Store` double):

| Test | Asserts |
|---|---|
| `TestPersist_DoesNotDropExternalKey` | after `New`, write `EXTERNAL=1` into the store's file behind the DB's back, then `Set("A","9")` + `Flush()` → the file still contains `EXTERNAL=1` |
| `TestPersist_DoesNotDropComments` | same shape, with a `# comment` line |
| `TestReload_AdoptsExternalChanges` | external edit changes an untouched key's value → after `Reload()`, `Get` returns the new value |
| `TestReload_KeepsUnflushedLocalWrites` | `Set("A","local")` (not yet flushed), external file sets `A=remote`, `Reload()` → `Get("A")` is still `"local"` |

## 5. Stage 2 — `reconcile.go` (new file)

Pure, `Store`-free, so the tests above need no I/O:

```go
package kvdb

// lineKind classifies a line of the backing file.
const (
	kindOther = iota // comment, blank, or anything without '='
	kindPair
)

type fileLine struct {
	raw  string // the line exactly as read, without its trailing newline
	key  string // "" unless kind == kindPair
	kind int
}

// parseLines splits the backing file into classified lines, preserving order.
func parseLines(data []byte) []fileLine

// reconcile returns the bytes to write: every line of disk is preserved in
// place, pair lines whose key is in touched are re-emitted with the in-memory
// value, and touched keys absent from disk are appended in insertion order.
func reconcile(disk []byte, data []pair, touched map[string]bool) []byte
```

Rules `reconcile` must implement, in this order:

1. Walk `parseLines(disk)`. For `kindOther`, emit `raw` unchanged. For `kindPair`:
   - key in `touched` → emit `key + sepEquals + <value from data>`;
   - otherwise → emit `raw` unchanged.
   - A key seen on disk is marked as emitted.
2. After the disk lines, for every pair in `data` (insertion order) whose key is in
   `touched` and was not emitted in step 1 → emit `key + sepEquals + value`.
3. Every emitted line is followed by `sepNewline`. Never emit a trailing blank line.
4. A key that appears twice on disk keeps both lines; only the first is updated, the
   second is emitted unchanged (the loader already resolves duplicates by first match).

Constants for this file:

```go
const (
	sepEquals  = "="
	sepNewline = "\n"
)
```

Reuse `splitOnFirstEquals` from `database.go` for classification — do not write a second
splitter.

## 6. Stage 3 — wire it into `TinyDB`

In [database.go](../database.go):

- Add the field `touched map[string]bool` to `TinyDB` and initialize it in `New`
  (`touched: make(map[string]bool)`).
- Add:

```go
// Reload re-reads the backing file and merges it into memory. Keys written by
// this process since the last flush keep their in-memory value; every other key
// takes the value on disk, and keys only present on disk are added.
func (t *TinyDB) Reload() error
```

  `Reload` is **not** added to the `KVStore` interface in
  [interfaces.go](../interfaces.go) — that interface is implemented by test doubles in
  consuming repos, and widening it would break them. Consumers that need it type-assert
  for `interface{ Reload() error }`.

In [methods.go](../methods.go):

- `Set` marks `t.touched[key] = true` on both branches (existing key and new key). Keep the
  existing `append` fast-path for a brand-new key: an append never destroys anything.
- `persist()` becomes:

```go
func (t *TinyDB) persist() error {
	disk, _ := t.store.GetFile(t.name) // absent file -> empty disk, not an error
	data := reconcile(disk, t.data, t.touched)
	if err := t.store.SetFile(t.name, data); err != nil {
		t.log(msgErrPersisting, err.Error())
		return err
	}
	return nil
}
```

- The debounced closure in `schedulePersist` must go through the same path. Today it
  snapshots under the lock and calls `t.store.SetFile` directly; change it to build the
  bytes with `reconcile` using the same read-then-merge sequence, keeping the existing
  "snapshot under lock, write outside the lock" shape: read the disk bytes **before**
  taking the lock is wrong (the value could change); read them inside the lock, build the
  bytes inside the lock, and only `SetFile` outside it.
- `snapshot()` is now only used by nothing. **Delete `snapshot()`** and verify with
  `grep -rn "snapshot(" .` → only matches in comments, if any.

Replace the two log literals with constants (they are already repeated across the file):

```go
const (
	msgErrPersisting = "error persisting:"
	msgErrAppending  = "error appending:"
)
```

`grep -rn '"error persisting:"\|"error appending:"' .` → empty after the change.

## 7. Acceptance criteria

- `gotest ./...` green.
- `grep -rn "snapshot(" .` → no function definition or call remains.
- `grep -rn '"error persisting:"' .` → empty (constant used instead).
- No new stdlib import: `grep -rn '"strings"\|"strconv"\|"errors"\|"fmt"\|"os"\|"bytes"\|"time"' *.go` → empty.
- `KVStore` in `interfaces.go` is unchanged (`git diff interfaces.go` → only the doc comment, if anything).
- `README.md` documents `Reload()` and states the guarantee: a write only changes the keys
  this process wrote; comments, blank lines, key order and unknown keys survive.

## 8. Stages

| # | Stage | Files |
|---|-------|-------|
| 1 | Failing tests | `reconcile_test.go` (new), `methods_test.go` |
| 2 | Pure reconcile | `reconcile.go` (new) |
| 3 | Wire into TinyDB + `Reload` | `database.go`, `methods.go` |
| 4 | Constants + delete `snapshot()` | `methods.go` |
| 5 | Docs | `README.md` |
