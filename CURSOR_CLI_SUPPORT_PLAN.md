# Plan: Support `cursor-agent` as a second agent backend

## Context

cormake currently hardwires every Plan/Execute run to the `claude` CLI
(`internal/agent/claude`). The `agent.Runner` interface
(`internal/agent/agent.go`) and `agent.Event` were already written
backend-agnostically ("claude is currently the only implementation"), and
`cursor-agent` (Cursor's headless CLI, confirmed installed and working
locally) exposes an equivalent surface: `-p --output-format stream-json`,
a read-only `--mode plan`, a `--force`/`--yolo` bypass-permissions
equivalent, and `--resume [chatId]`. The goal here is to let a user pick
`cursor-agent` as the agent that actually runs a task's Plan/Execute
work, per-workspace by default, overridable per-run.

Cost tracking is explicitly out of scope for this pass: `cursor-agent`'s
`result` event has no `total_cost_usd` field at all (confirmed by running
it directly), so cursor-backed runs will simply show `$0`/no cost
contribution. Fixing that (e.g. a model-price table to estimate cost from
tokens) is a separate follow-up.

## Verified facts about `cursor-agent`'s stream-json dialect

Confirmed by running `cursor-agent -p --output-format stream-json
--stream-partial-output --trust "<prompt>"` directly and inspecting
output (not assumed from docs):

- `{"type":"system","subtype":"init","session_id":...,"model":...,"cwd":...}`
  — field names match claude's `rawSystemInit` exactly, trivial to reuse.
- `{"type":"assistant","message":{"content":[{"type":"text","text":...}]}}`
  — same shape as claude's text blocks. No `parent_tool_use_id` field, so
  subagent detection doesn't apply (`IsSubagent` is always `false` for
  cursor).
- Tool calls are a **different event type entirely**, not content blocks:
  `{"type":"tool_call","subtype":"started","call_id":"...","tool_call":{"shellToolCall":{"args":{...}}}}`
  followed later by `{"type":"tool_call","subtype":"completed","call_id":"...","tool_call":{"shellToolCall":{"args":{...},"result":{"success":{"stdout":...,"exitCode":...}}}}}`.
  The tool kind is the single dynamic key inside `tool_call` (e.g.
  `shellToolCall`) rather than a flat `name` field — matched across
  started/completed via `call_id`.
- `{"type":"thinking","subtype":"delta"|"completed","text":...}` is a
  separate top-level stream, not a content block. Not translated in v1,
  same as claude's own (already-untranslated) thinking blocks.
- `{"type":"result","subtype":"success","result":...,"session_id":...,"is_error":...,"usage":{"inputTokens":...,"outputTokens":...,"cacheReadTokens":...,"cacheWriteTokens":...}}`
  — camelCase, no `total_cost_usd`.
- A directory must be explicitly trusted for headless use: `--trust` (or
  `-f`/`--yolo`) is required or the process just prints a trust prompt
  and produces no stream-json at all. Since `RepoPath` always points at a
  disposable cormake-created worktree (never the user's real checkout),
  passing `--trust` unconditionally is safe.

## Design

### 1. Backend type + workspace default

Add to `internal/domain` (alongside `Status`):

```go
type AgentBackend string

const (
	AgentBackendClaude AgentBackend = "claude"
	AgentBackendCursor AgentBackend = "cursor"
)
```

- `Workspace.DefaultAgentBackend AgentBackend` — hand-edited via
  `workspaces.json` for now, same tier as `PrimaryColor`/`Prefix`/
  `MaxConcurrentAgents` (no dedicated settings UI exists for any of
  those yet). Add `Workspace.EffectiveDefaultAgentBackend()` following
  the exact pattern of `EffectiveMaxConcurrentAgents`/
  `EffectiveDefaultTargetBranch`: empty/unset → `AgentBackendClaude`.
- `Task.AgentBackend domain.AgentBackend` — the backend a task's *active
  session* actually belongs to. Set once, when a fresh Plan or Execute
  run is kicked off from the confirmation modal; zero-value (empty) on
  already-persisted tasks is treated as claude, matching the codebase's
  existing zero-value-means-legacy-default convention. Every later
  resume of that same task (revise-after-review, free-form message via
  `sendInputPrompt`) reuses `t.AgentBackend` — those flows don't go
  through the confirmation modal at all today, so they need no new UI,
  just to route through the right runner.

Note Execute already always starts a **fresh** session regardless of a
prior Plan (`startExecuteRun` passes `resumeSessionID=""`), so there is
no cross-backend resume hazard between Plan and Execute — Execute is
always free to pick its own backend independent of what Plan used.

### 2. New `internal/agent/cursor` package implementing `agent.Runner`

Mirrors `internal/agent/claude` one-for-one:

- `client.go` — `Start`/`Attach`, identical process/tail/signal handling
  to `claude/client.go` (spawn detached via `Setsid`, redirect to real
  files, tail via the existing `tailFile`/`ProcessAlive` helpers), just
  invoking `cursor-agent` instead of `claude`. **Worth factoring out**
  the ~150 lines of exec/tail/signal plumbing in `claude/client.go` that
  have nothing claude-specific about them (already true today — the
  comment on `agent.go` even says so) into a small shared helper (e.g.
  `internal/agent/procrunner`) both packages call, rather than
  copy-pasting the whole file. Keeps the two backends from silently
  drifting on process-lifecycle bugs.
- `args.go` — `BuildArgs`:
  - always: `-p --output-format stream-json --stream-partial-output --trust`
  - `RunModePlan` → `--mode plan`
  - `RunModeComplete` → `--force`
  - `ResumeSessionID` set → `--resume <id>`; otherwise no session-id flag
    is passed at all (unlike claude, `cursor-agent` has no
    "start-new-session-with-this-id" flag — a fresh run just gets
    whatever session id it generates, read back off the `init` event,
    which cormake already captures generically via `EventInit.SessionID`)
  - `SettingsPath` has no cursor equivalent (no hook-server concept) —
    ignored for this backend
  - prompt appended last, same as claude
- `translate.go` — new dialect parser:
  - `system`/`init` → `EventInit` (direct field reuse)
  - `assistant` text blocks → `EventText`
  - `tool_call`/`started` → `EventToolUse`: `ToolName` = the single
    dynamic key under `tool_call` (e.g. `"shellToolCall"`), `ToolInput`
    = JSON of that key's `args`. Good-enough-for-display fidelity,
    matching the existing claude translator's own stated bar ("not
    trying to render structured content specially yet").
  - `tool_call`/`completed` → `EventToolResult`: `Text` = JSON of that
    key's `result` (shape varies per tool kind — `shellToolCall` has
    `result.success.{stdout,exitCode}` or presumably a `result.error`
    variant; treat generically rather than special-casing every tool
    kind in v1)
  - `thinking` → dropped (unhandled, same as claude's thinking blocks)
  - `result` → `EventResult`: map `usage.inputTokens` →
    `InputTokens`, `outputTokens` → `OutputTokens`, `cacheReadTokens` →
    `CacheReadInputTokens`, `cacheWriteTokens` → `CacheCreationInputTokens`.
    `CostUSD` left at `0` (out of scope, see Context).
  - everything else silently dropped, same policy as claude's translator

`tail.go`'s `tailFile`/`ReplayFile`/`ProcessAlive` are backend-neutral
already and can be reused directly (or moved into the shared
`procrunner` package alongside the client-lifecycle code above) rather
than duplicated.

### 3. Wire the runner registry into `internal/ui`

- Replace `Model.runner agent.Runner` (single hardcoded
  `claude.Client{}`, set in `app.go` around line 225) with a small
  lookup, e.g. `runners map[domain.AgentBackend]agent.Runner`, populated
  once at startup with `{AgentBackendClaude: claude.Client{}, AgentBackendCursor: cursor.Client{}}`.
- Add a helper `func (m Model) runnerFor(backend domain.AgentBackend) agent.Runner`
  that falls back to `claude.Client{}` for empty/unrecognized values.
- Every call site that currently does `m.runner.Start(...)` /
  `.Attach(...)` (`runPlanAgent`, `runExecuteAgent`, task-reconnect on
  restart) switches to `m.runnerFor(t.AgentBackend).Start(...)` /
  `.Attach(...)`.
- `claude.ProcessAlive`/`claude.ReplayFile` calls in `app.go` (lines
  ~1657, ~1682) are backend-neutral file/PID operations — fine to keep
  calling the `claude` package's copies as-is, or switch to whichever
  package ends up owning the shared `procrunner` helper from step 2.

### 4. Radio selector on the Plan/Execute confirmation modal

Today `openConfirm`/`renderConfirmModal`/`updateConfirmModal` (`app.go`)
render a plain "`<message>` [y]es [n]o" box — no `huh` form involved.
Add a lightweight backend choice to it:

- Add `confirmBackend domain.AgentBackend` to `Model`, initialized in
  `openConfirm` to `ws.EffectiveDefaultAgentBackend()` for the task's
  workspace.
- `renderConfirmModal` gains a second line under the message: something
  like `Agent:  ( ) claude   (•) cursor` with the selected one
  highlighted — a manual two-option toggle is enough here (no need to
  pull in a full `huh.Select`, since this modal isn't a `huh.Form` today
  and this is a single binary choice); a left/right or tab keypress
  flips `confirmBackend`, `[y]es [n]o` stays the confirm/cancel keys.
  (If preferred, this can instead be a real `huh.NewSelect` embedded the
  way `branchPicker` embeds `huh.Form` — flag this as an open call for
  Phase 3 review: manual toggle is less code and matches this modal's
  current plain-text style; a `huh.Select` is more consistent with
  every other picker in the app like `branchPicker`/the new-task
  wizard's repo/branch steps.)
- `updateConfirmModal`'s `y`/`enter` branch passes `m.confirmBackend`
  through to `startPlanRun`/`startExecuteRun`, which stamp it onto
  `Task.AgentBackend` before calling `runPlanAgent`/`runExecuteAgent`.
- The delete-confirmation path (`confirmKindDelete`) is unaffected —
  only the two status-transition confirms (`StatusPlanning`,
  `StatusInProgress`) show the selector.

## Files touched (representative, not exhaustive)

- `internal/domain/workspace.go` — `DefaultAgentBackend` field +
  `EffectiveDefaultAgentBackend()`
- `internal/domain/task.go` — `AgentBackend` field
- new: `internal/agent/cursor/{client.go,args.go,translate.go}` (+
  optional shared `internal/agent/procrunner` if extracted)
- `internal/ui/app.go` — runner registry/`runnerFor`, `confirmBackend`
  state, `openConfirm`/`renderConfirmModal`/`updateConfirmModal`,
  `startPlanRun`/`startExecuteRun` signatures, every `m.runner.*` call
  site

## Verification

- Unit-test-level: a `translate_test.go` in `internal/agent/cursor`
  feeding the exact captured JSON lines above through `translateLine`
  and asserting the resulting `agent.Event`s — same style as any
  existing tests under `internal/agent` (check for a `claude` translate
  test to match its structure; if none exists, model it on
  `internal/store/store_test.go`'s style).
- End-to-end: run `cormake`, create a task in a workspace with
  `DefaultAgentBackend` unset (should default-select claude in the
  modal), Plan it, confirm the selector shows and flipping it to cursor
  actually spawns `cursor-agent` (visible via `ps` or the raw stdout log
  file under the task's log dir) and streams recognizable text/tool
  events into the task's log pane exactly like a claude run does.
  Repeat for Execute. Confirm a task's `Cost` stays `$0` for
  cursor-backed runs rather than erroring or showing garbage.
- Confirm an existing `workspaces.json`/task file from before this
  change still loads fine (zero-value `DefaultAgentBackend`/
  `AgentBackend` decode to empty string, which `EffectiveDefaultAgentBackend`/
  `runnerFor` treat as claude) — no migration needed given plain
  JSON marshal/unmarshal.
