# Runtime regression and Go release baseline

Date: 2026-10-17

## Go toolchain and checks

The installed default Go toolchain was `go1.26.5 windows/amd64`. Do not use that toolchain as the release baseline: `govulncheck@v1.7.0` found reachable Go standard-library vulnerabilities in `shared/update`, `node`, and `client`, including HTTP/2 and TLS issues fixed in Go 1.26.9. Those findings were from the standard library shipped with Go 1.26.5, not from application dependencies.

The checks below used a per-process `GOTOOLCHAIN=go1.26.9`; the global Go installation and `go env -w` were not changed. Go downloaded and selected `go1.26.9 windows/amd64` successfully.

| Check | Result |
| --- | --- |
| `go test ./shared/config/... ./shared/logfiles/... ./shared/update/... ./node/... ./client/...` | Passed after the cancellation-classification fix described below. |
| `go vet ./shared/config/... ./shared/logfiles/... ./shared/update/... ./node/... ./client/...` | Passed. |
| Pinned `staticcheck` 0.6.1 on all packages under `node` | Passed. Earlier in this run, pinned 0.6.1 also passed all five modules. |
| `govulncheck` 1.7.0 on `shared/config`, `shared/logfiles`, `shared/update`, `node`, and `client` | All five scans completed; 0 reachable vulnerabilities in each module. |
| Manage Release Hub API regression (`tests.test_manage_releases`) | Passed, 6 tests. The Windows cross-architecture case verifies x64-only availability does not produce an x86 asset or upgrade, and an existing historical x86 release remains independently discoverable/downloadable. |

The first full test run on Go 1.26.9 exposed a cancellation race in `TestBashRunCancelSyncViaUI`: cancellation while waiting for the workspace lease was returned as `workspace_busy: context canceled` instead of `CANCELLED`. The fix classifies a UI cancellation received during lease wait as `CANCELLED` while preserving genuine lease-timeout/busy behavior. The regression test held the lease deterministically; the targeted test passed 20 repetitions, the `node/internal/tools` package passed, and the full five-module suite then passed.

The earlier Go 1.26.5 scan reported 13 reachable standard-library findings in `shared/update`, 17 in `node`, and 13 in `client`; `shared/config` and `shared/logfiles` had none. Re-running on Go 1.26.9 cleared all reachable findings. Do not treat Go 1.26.5 as an acceptable release build baseline.

## Existing recovery and Auto/Dreaming coverage

Existing tests exercise representative recovery and queue boundaries:

- `node/internal/session/dreaming_queue_restore_test.go` verifies a queued human input survives restart when an orphaned Dreaming attempt is failed during restore.
- `node/internal/session/dreaming_reopen_test.go` verifies a waiting Dreaming approval survives runtime restart and can be resumed, and that an orphaned attempt is failed on restore.
- `node/internal/session/input_box_test.go` verifies idle dequeue prioritizes human input over system Auto input, retains the ordinary-trigger FIFO barrier, and restores system-Auto in-flight ownership.
- `node/internal/session/idle_context_boundary_test.go` verifies idle context boundaries skip Auto agents and active turns.
- `node/internal/session/runtime_continuation_test.go` and `side_effects_cancel_test.go` verify human input waits for explicit cancellation while HITL is pending, and that cancellation/recovery does not duplicate continuation results.
- `node/internal/session/p0_trigger_recovery_test.go` and `trigger_recovery_regression_test.go` verify restart requires recovery for pending trigger delivery, fences old delivery, and discards recovered mailbox items before executing a turn.
- `node/internal/session/runtime_lifecycle_test.go` verifies unknown execution requires reconciliation before continuing.

These are bounded automated regression cases, not a 24-hour endurance or soak test. No 24-hour durability claim is made.

## Local Node terminal protocol smoke

The existing Rust test `live_local_node_terminal_open_input_detach_resume_close_smoke` passed once against the already-running Node at `http://127.0.0.1:18977`. That listener was PID 13952 running the isolated test binary `C:\Users\Administrator\.codex\worktrees\2321\DAgents\.runtime-test-idle-context-20261016\bin\dagents-node-idle.exe`. It is an earlier test binary, not the current release candidate; this smoke validates the Rust client open/input/detach/resume/close protocol against that binary only and does not establish candidate-binary behavior.

The test used a temporary normal Agent `agt-1263487fe7b0628f`, not the pre-existing `agt-a0fb1d62dfabc640`. It ran with `DAGENTS_TERMINAL_SMOKE=1`, `DAGENTS_NODE_ENDPOINT=http://127.0.0.1:18977`, and that temporary Agent ID. The test closed the terminal; a follow-up terminal-list query returned zero sessions, and the temporary Agent was deleted and verified absent. No Node, Desktop, Browser, or installer process was started.
