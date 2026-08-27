---
PLAN: "feat: enumerate stored keys so a caller can scan for a value pattern"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — `KVStore.Keys()`

## Part of a multi-repo wave

This is one piece of `KEYRING_DOTENV_MASTER_PLAN.md` (orchestrator:
`github.com/tinywasm/app-releases`, `docs/KEYRING_DOTENV_MASTER_PLAN.md`). This
piece has **no dependency on any other piece of that wave** — dispatch it
immediately, in parallel with the `tinywasm/tui` and `tinywasm/keyring` plans.

## Why

`tinywasm/app` needs to scan every key in a project's `.env`-backed store
looking for the ones whose value marks them as living in `tinywasm/keyring`
instead of on disk. Its own `AGENTS.md` forbids reading `.env` directly —
`h.DB` (a `kvdb.KVStore`) is the only sanctioned access path, and today that
interface exposes only `Get`/`Set`. There is no way to enumerate.

## The change

File: **[`interfaces.go`](interfaces.go)**. Add one method to `KVStore`:

```go
// KVStore defines the minimum API
type KVStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	// Keys returns every key currently stored, in insertion order. Empty
	// store returns an empty (non-nil) slice, never nil.
	Keys() []string
}
```

File: **[`methods.go`](methods.go)**. Implement it on `*TinyDB`, next to `Get`/`Set`:

```go
// Keys returns every key currently stored, in insertion order.
func (t *TinyDB) Keys() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	keys := make([]string, 0, len(t.data))
	for _, p := range t.data {
		keys = append(keys, p.Key)
	}
	return keys
}
```

`t.data` is the existing `[]pair` field populated by `New()` (see
[`database.go`](database.go)) and mutated by `Set`/`append` in
[`methods.go`](methods.go) — read it under `t.mu.RLock()` exactly like `Get`
already does, so `Keys()` is safe to call concurrently with `Set`.

## `*TinyDB` is the only implementer — verified

`grep -rn "kvdb\.KVStore" ~/Dev/Project/tinywasm/*/*.go` (run before writing
this plan) shows exactly one embedder outside this repo:
`tinywasm/app/interface.go` (`Handler.DB kvdb.KVStore`), which only ever
**calls** the interface — it does not implement it. No other type in the
ecosystem implements `KVStore`, so this addition cannot break a second
implementer.

## Tests

File: **`keys_test.go`** (new, next to `methods.go`).

| Test | Asserts |
|---|---|
| `TestKeysEmptyStore` | a `TinyDB` with nothing loaded returns `[]string{}`, not `nil` |
| `TestKeysReturnsInsertionOrder` | `Set("A", "1")`, `Set("B", "2")`, `Set("C", "3")` → `Keys()` is `["A","B","C"]` |
| `TestKeysReflectsLoadedFile` | construct `mockStore` (defined in [`database_test.go`](database_test.go), same package — reuse it, do not add a second fake) pre-loaded via its existing mechanism with `"X=1\nY=2\n"`, `New(...)` it, `Keys()` is `["X","Y"]` |
| `TestKeysAfterOverwrite` | `Set("A", "1")` then `Set("A", "2")` → `Keys()` still has exactly one `"A"`, not two |

## Acceptance criteria

- [ ] `go build ./...` and `go vet ./...` clean.
- [ ] `go test ./...` green, including the four new tests.
- [ ] `grep -n "Keys() \[\]string" interfaces.go methods.go` → two matches (interface + implementation).
- [ ] No other file in this repo changed.

## Out of scope

Nothing about `tinywasm/keyring`, `tinywasm/env`, or `tinywasm/app` — those are
separate plans in the same wave. This plan adds one read-only method and
nothing else.
