# PLAN: migrate wasmbrowsertest's chromedp usage to devbrowser's vendored copy.

## Background (one paragraph — the bug itself is already fixed elsewhere)

wasmbrowsertest used to fail with an empty `"chrome failed to start:"` error
because `chromedp.findExecPath()` (its direct dependency,
`github.com/chromedp/chromedp`) checks `chromium` before `google-chrome`,
and some Chromium packages crash instantly in headless mode. That root
cause, its investigation, and the fix are fully documented in
`github.com/tinywasm/devbrowser`'s own history (tagged **`v0.4.1`**,
commit `5052476`) — not repeated here. Treat devbrowser's resolution logic
as a given, already-correct dependency; do not re-derive or re-investigate
the Chrome-crash bug as part of this work.

## What this migration actually is

**This is an import-path swap, not a rewrite.** `devbrowser` vendors its own
full copy of chromedp under `github.com/tinywasm/devbrowser/chromedp` and
`github.com/tinywasm/devbrowser/cdproto/...`, with identical package names
and APIs to the upstream `github.com/chromedp/chromedp` /
`github.com/chromedp/cdproto/...` this repo currently imports (confirmed:
same `package chromedp`, `package runtime`, etc., same exported surface).
wasmbrowsertest's existing chromedp-based code — the task list, the
`chromedp.ListenTarget`/`handleEvent` console relay, the `-cpuprofile`
profiler wrapping — **stays exactly as it is**. The only two changes:

1. Swap every `github.com/chromedp/...` import in this repo to the
   equivalent `github.com/tinywasm/devbrowser/...` import.
2. Use `devbrowser.ResolveChromeExecPath()` to pick the Chrome binary
   (via `chromedp.ExecPath(...)`), instead of relying on chromedp's
   built-in `findExecPath()` default resolution — that's the one substantive
   behavior change, and it's the actual bug fix.

Do not build any new devbrowser API, hooks, or callbacks for this — this
repo already has its own, working task-list/event-handling code; the goal
is only to source it from one copy of chromedp instead of two. (An earlier
draft of this plan proposed a higher-level `devbrowser.RunWasmTest`
function for this repo to call instead of the import swap; that function
was speculative, had no caller, and has since been removed from
devbrowser — don't resurrect that approach.)

## Hard constraints (non-negotiable, apply to every task below)

1. **The public API/CLI contract does not change, for any reason.** Every
   existing command-line flag (`-quiet`, `-coverprofile`, `-cpuprofile`, and
   any other flag parsed in `main.go`), every existing env var
   (`WASM_HEADLESS`), the `go test -exec wasmbrowsertest` invocation contract
   itself (args in: `[exec-flags] <test-binary> [test-flags]`),
   stdout/stderr formatting, and the process exit code semantics must behave
   exactly as before the migration.
2. **All tests must pass before this is considered done.** That includes
   this repo's own test suite (`gotest` / `go test ./...`) AND the
   self-contained fixture checks in Tasks 6-8. A migration that breaks any
   currently-passing test, or that "fixes" the fixture checks only by
   disabling/skipping a test, does not count as complete.

## Tasks

1. **Fix the module path and documentation to match the actual owner
   (`tinywasm`), not the historical upstream (`agnivade`).** This repo's
   `go.mod` still declares `module github.com/agnivade/wasmbrowsertest`,
   and `handler.go`/`main_test.go`/`README.md` still import/link
   `github.com/agnivade/wasmbrowsertest/...`, even though `git remote -v`
   shows this repo actually lives at
   `git@github.com:tinywasm/wasmbrowsertest.git`. This is not a stable
   contract being preserved — it's an existing inconsistency, already
   proven by the fact that `devflow/gotest.go` (`installWasmBrowserTest`)
   already runs `go install github.com/tinywasm/wasmbrowsertest@latest`.
   Concretely:
   - Change `go.mod`'s `module` line to `github.com/tinywasm/wasmbrowsertest`.
   - Update every internal self-import (e.g. `handler.go`:
     `"github.com/agnivade/wasmbrowsertest/filesys"` → `"github.com/tinywasm/wasmbrowsertest/filesys"`).
   - Update `README.md`: the CI badge URL, all `go install
     github.com/agnivade/wasmbrowsertest@latest` instructions (there are
     several — main binary and the `cmd/cleanenv` one), and the CI workflow
     snippet, all to the `tinywasm` path. Leave links to historical upstream
     GitHub issues (e.g. in `main_test.go`) as-is if they refer to an issue
     actually filed upstream.
   - Run `go build ./...` and `go vet ./...` after the rename to catch any
     remaining stale import.

2. **Add `github.com/tinywasm/devbrowser` as a dependency**, pinned to
   `v0.4.0`, in `wasmbrowsertest/go.mod`:
   ```bash
   go get github.com/tinywasm/devbrowser@v0.4.0
   ```

3. **Swap the two files that import chromedp directly to devbrowser's
   vendored copy.** Only `main.go` and `profiler.go` import
   `github.com/chromedp/...` in this repo (verified: `grep -rln
   "github.com/chromedp" --include=*.go .` excluding `_test.go` files
   returns just these two). Change:
   - `main.go`:
     - `"github.com/chromedp/cdproto/inspector"` → `"github.com/tinywasm/devbrowser/cdproto/inspector"`
     - `"github.com/chromedp/cdproto/profiler"` → `"github.com/tinywasm/devbrowser/cdproto/profiler"`
     - `cdpruntime "github.com/chromedp/cdproto/runtime"` → `cdpruntime "github.com/tinywasm/devbrowser/cdproto/runtime"`
     - `"github.com/chromedp/cdproto/target"` → `"github.com/tinywasm/devbrowser/cdproto/target"`
     - `"github.com/chromedp/chromedp"` → `"github.com/tinywasm/devbrowser/chromedp"`
   - `profiler.go`:
     - `"github.com/chromedp/cdproto/profiler"` → `"github.com/tinywasm/devbrowser/cdproto/profiler"`
   - No other code in either file changes — package names match
     (`chromedp`, `profiler`, `runtime`, `target`, `inspector`), so every
     existing call site (`chromedp.NewExecAllocator`, `chromedp.Flag`,
     `chromedp.ListenTarget`, `chromedp.Navigate`, `profiler.Enable()`,
     `cdpruntime.EventConsoleAPICalled`, etc.) keeps compiling unmodified.
   - Also check `main_test.go` and any other `_test.go` file for
     `github.com/chromedp` imports and swap those too, for consistency
     (even though the earlier grep excluding tests found none — re-check
     without the exclusion in case that's changed).

4. **Wire in `devbrowser.ResolveChromeExecPath()`** — this is the actual
   bug fix, and the only behavior-affecting change in this plan. In
   `main.go`, in the `opts := chromedp.DefaultExecAllocatorOptions[:]`
   block (around line 190), add:
   ```go
   opts = append(opts, chromedp.ExecPath(devbrowser.ResolveChromeExecPath()))
   ```
   (with `"github.com/tinywasm/devbrowser"` imported for the unqualified
   `devbrowser.ResolveChromeExecPath` call — note this is a different
   import than `github.com/tinywasm/devbrowser/chromedp` from Task 3; both
   are needed). This makes wasmbrowsertest resolve the same
   known-good Chrome binary devbrowser resolves elsewhere, instead of
   falling back to chromedp's own `findExecPath()` (which is what picks a
   broken `chromium` over a working `google-chrome` on affected hosts).
   Everything else in the `opts` construction (`WASM_HEADLESS=off`
   handling, the WSL `DisableGPU` special-case) stays untouched.

5. **Remove the now-unused direct `github.com/chromedp/chromedp` and
   `github.com/chromedp/cdproto/...` dependencies** from `go.mod`/`go.sum`.
   After Task 3, no `.go` file in this repo should import
   `"github.com/chromedp/..."` anymore — confirm with
   `grep -rl '"github.com/chromedp' --include=*.go .` returning nothing,
   then run `go mod tidy`.

6. **Build a self-contained fixture package that exercises everything
   `gotest` parses, and use it for every check below.** Do not depend on any
   other local repo — this plan must be executable by an agent with no
   access to this machine's other projects. Create a throwaway directory
   (e.g. `/tmp/wasmtest-fixture`) with:

   `go.mod`:
   ```
   module wasmtestfixture

   go 1.25
   ```

   `fixture_test.go`:
   ```go
   package wasmtestfixture

   import (
       "testing"
       "time"
   )

   // TestFast proves ordinary "--- PASS" lines still reach stdout.
   func TestFast(t *testing.T) {}

   // TestSlow proves per-test timing (used by gotest's FindSlowestTest,
   // which triggers above a 2.0s threshold) still reaches stdout.
   func TestSlow(t *testing.T) {
       time.Sleep(2200 * time.Millisecond)
   }

   // TestFailing proves "--- FAIL" lines still reach stdout (used by
   // gotest's EvaluateTestResults). Controlled by an env var so it can be
   // toggled on/off across the two runs required below, instead of needing
   // to hand-edit the file mid-check.
   func TestFailing(t *testing.T) {
       if failEnabled() {
           t.Fatal("deliberate failure to verify --- FAIL propagation")
       }
   }
   ```

   `fixture_helpers.go`:
   ```go
   package wasmtestfixture

   import "os"

   func failEnabled() bool {
       return os.Getenv("FIXTURE_FAIL") == "1"
   }
   ```

7. **Run the fixture directly with `go test -exec wasmbrowsertest`** (not
   `gotest` yet — isolates wasmbrowsertest itself from devflow):
   ```bash
   cd /tmp/wasmtest-fixture
   GOOS=js GOARCH=wasm go test -exec wasmbrowsertest -v .
   ```
   Confirm:
   - `=== RUN   TestFast` / `--- PASS: TestFast` are present (proves the
     console relay still works after the import swap — unchanged code, so
     this should be a formality, but confirm it anyway).
   - `--- PASS: TestSlow (2.2Xs)` shows a real elapsed time (exact format
     `gotest`'s `FindSlowestTest` regex depends on).
   - Overall result is `ok`, exit code `0` (with `FIXTURE_FAIL` unset).
   - Re-run with `FIXTURE_FAIL=1`: `--- FAIL: TestFailing` appears with the
     `t.Fatal` message visible, overall result `FAIL`, exit code non-zero.
   - The run uses `google-chrome` (or whatever `ResolveChromeExecPath`
     resolves to on the machine running this) and does not hit the empty
     `"chrome failed to start:"` error from the Background section.

8. **Run the same fixture through `gotest` itself**, to prove the full
   parsing chain (`EvaluateTestResults`, `FindSlowestTest`,
   `FindTimedOutTests`, `calculateAverageCoverage`) still works end-to-end.
   If `gotest` is available (`go install
   github.com/tinywasm/devflow/cmd/gotest@latest`), run it from
   `/tmp/wasmtest-fixture` with `FIXTURE_FAIL` unset and confirm: summary
   line reports `wasm ✅`; a `⚠️ slow: TestSlow (2.2s)` line prints;
   `coverage: X%` is non-zero. Re-run with `FIXTURE_FAIL=1` and confirm
   `gotest` reports overall failure (`wasm ❌`, non-zero exit code) with
   `TestFailing`'s assertion output visible. If `gotest` isn't installable
   in the execution environment, stop and report that explicitly rather
   than skipping this task silently (Hard constraint 2).

9. **Run this repo's own full test suite and confirm every test passes** —
   `gotest` (or `go test ./...` if `gotest` itself isn't usable standalone
   here) — before considering the migration complete. A partially-green run
   is not acceptable; if something fails, fix it or explicitly stop and
   report it, don't mark the plan done anyway.

## Out of scope

- The Chrome-crash bug itself and its exec-path resolution algorithm —
  already implemented and fixed in `devbrowser@v0.4.0`; this repo only
  calls `ResolveChromeExecPath()`, it doesn't reimplement or extend it.
- Filing/fixing the underlying Debian `chromium` packaging issue.
- Any new devbrowser API, hooks, or callbacks — not needed for this
  migration; devbrowser's public surface for this repo's purposes is just
  `ResolveChromeExecPath()` plus the vendored `chromedp`/`cdproto`
  subpackages.
