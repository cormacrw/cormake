# cormake — Vision

## 1. What cormake is

cormake is a terminal "command center" for delegating coding work to Claude. It's a Go/Bubbletea TUI where you create a task, point it at a git repo, and choose one of two modes: **plan** (Claude researches the codebase and proposes an approach without touching any code) or **complete** (Claude autonomously executes the task inside an isolated git worktree). Tasks live under **workspaces**, which group one or more git repos together, so you can keep, say, a personal workspace and a work workspace with entirely different repos and task histories. Everything — workspaces, repos, tasks, session transcripts — is stored as flat JSON on the local filesystem under your home directory; there's no server, no database, no account. Claude is the only agent backend today, but the design keeps that swappable rather than hard-wired. Importing tasks from project-management tools like ClickUp or Jira via MCP is an explicit future direction — the data model leaves room for it, but nothing about it is built yet.

## 2. Core concepts

**Workspaces & repos.** A workspace is just a named grouping of git repos plus the tasks created against them. cormake auto-creates a **default workspace on first run, but it starts empty** — no auto-detection of whatever repo you happened to launch the app from. You explicitly add a repo to a workspace (validated as a real git repo root), and from then on tasks created in that workspace pick one of its repos.

**Tasks.** A task has a title, a description, a workspace, a repo, a mode (`plan` or `complete`), and a status that moves through `pending → running → (awaiting_approval) → completed | failed | cancelled`. Mode is a first-class, permanent property of the task — a plan-mode task never edits code; a complete-mode task always runs inside its own worktree.

**Permission posture.** Complete-mode tasks default to a **low-friction posture**: Claude's built-in `acceptEdits` permission mode auto-approves file edits and common filesystem commands (`mkdir`, `touch`, `mv`, `cp`) inside the worktree with no interruption, while a live approval hook (see §4) catches everything riskier — arbitrary shell commands, network access, git push, and so on — and surfaces it in the TUI for a real decision. This was a deliberate choice over a maximum-lockdown "approve literally everything" posture: the worktree is already the isolation boundary, so the friction budget is spent on things that actually matter.

**Worktrees as the isolation boundary.** Every complete-mode task gets its own git worktree, created once at `<repo>/.claude/worktrees/<task-slug>` and reused across retries of the same task. This is what makes "let Claude work autonomously" safe to say: nothing it does touches your actual working copy or main branch until you decide to merge it.

## 3. How task execution actually works

Under the hood, a task run is a single `claude -p` subprocess invocation, one per task:

```
claude -p \
  --output-format stream-json --include-partial-messages --verbose \
  --session-id <uuid> \
  --settings <generated-settings-path> \
  [--permission-mode plan | -w <worktree-name>] \
  "<prompt built from the task's title/description>"
```

`cwd` is set to the repo path in both modes; for `complete` mode, the `-w` flag is what actually creates and switches into the worktree — cormake doesn't create worktrees itself.

Claude streams its output as newline-delimited JSON on stdout: a `system`/`init` event first (model, tools, MCP servers loaded), `assistant`/`user` messages carrying text and tool-call/tool-result content blocks, occasional `system`/`api_retry` events, and — always as the final line — a `result` message with the final text, cost, and session metadata. This is the live feed the TUI's detail pane renders as the task runs.

Deliberately **not** used: `--input-format stream-json`. That flag exists to stream follow-up turns into a running session over stdin, but its exact wire format isn't documented for the raw CLI (only inferable from an SDK example), and cormake doesn't need it — every kind of input Claude might need mid-run, including clarifying questions, is handled by the mechanism below without ever writing to stdin. If a future "steer a running task" feature wants it, that's a deliberate, separate piece of work, not something to reach for by default.

## 4. The permission/question hook mechanism

This is the central technical insight the whole project rests on, so it's worth stating plainly: **a plain `claude -p` subprocess with no TTY has no way to ask a wrapping program for a real-time decision.** There's no `control_request`/`canUseTool` protocol at the CLI level — that only exists in the Python/TypeScript Agent SDK, which a Go program can't use. Left to its own devices, a headless run either hangs forever waiting on stdin that will never come, or auto-denies/aborts, depending on permission mode.

The way around this is a feature Claude Code does expose at the CLI level: **hooks configured as HTTP callbacks.** A `PreToolUse` hook can be declared as type `http`, pointing at a URL; when Claude wants to use a tool that isn't already covered by the permission mode, it POSTs the pending call to that URL and blocks — for up to a declared timeout — waiting for a JSON decision in the response.

cormake runs **one local HTTP server for the whole app's lifetime**, bound to an ephemeral port on `127.0.0.1`. For every task it starts, it generates a per-task `settings.json` (passed via `--settings`) that points a `PreToolUse` hook at that server, and registers the task's `session_id` against the server so incoming requests can be routed back to the right task. When a hook request comes in, the server enqueues a pending approval, pushes an event into the TUI (which renders it as an overlay — approvals always interrupt whatever else is on screen), and blocks the HTTP response until the user decides or an app-side timeout fires. That app-side timeout is deliberately shorter than the timeout declared in the hook config, so cormake always resolves the request itself rather than racing Claude's own timeout.

The same mechanism handles more than "approve or deny a shell command." Claude's clarifying-question tool, `AskUserQuestion`, is answered through this exact channel too — "approving" that tool call means responding with `updatedInput: {questions, answers}` rather than a bare allow. So cormake's approval overlay special-cases `AskUserQuestion`: instead of a yes/no prompt, it renders the actual multiple-choice questions Claude is asking, collects the user's picks, and returns them in the approval response. One code path, keyed on tool name, is what "handle any kind of input required from Claude" concretely means in this design. It's also worth noting up front that clarifying questions are common in plan mode, not just complete mode — so this hook is wired for both, even though plan mode never needs the edit-approval side of it.

## 5. Data model sketch

- **Workspace** — id, name, the list of repos it contains, created/updated timestamps.
- **Repo** — id, display name, absolute path to the repo root, when it was added.
- **Task** — id, which workspace and repo it belongs to, title, description, mode (`plan`/`complete`), status, the `session_id` used for its claude run, the worktree name it owns (generated once, reused on retry), a result summary, accumulated cost, an error message if it failed, and timestamps for created/started/finished. It also carries a `source` field (defaulting to `"manual"`) and empty `external_id`/`external_url` fields — dormant today, reserved so a future ClickUp/Jira import can populate them without a schema migration.
- **PermissionRequest** — the live, in-flight shape of a pending approval: which task and session it belongs to, the tool name and input Claude wants to run, the cwd and permission mode at the time, when it arrived. This is the thing the approval overlay renders; it doesn't get persisted as task history beyond whatever summary ends up in the transcript.

## 6. Local storage layout

Everything lives under a single directory in the user's home (naming to be finalized alongside the app's own name — currently sketched as `~/.task-manager-cli/`, likely to become `~/.cormake/`):

```
config.json                          global app config
workspaces.json                      all workspaces and their repos
tasks/<task-id>.json                 one file per task
sessions/<task-id>/settings.json     generated per-run claude --settings file (hook config)
sessions/<task-id>/transcript.jsonl  raw stream-json output, appended live as the task runs
```

Flat JSON, atomic writes (write to a temp file, then rename) — no database, easy to inspect or hand-edit, easy to back up.

## 7. High-level architecture shape

Thinking in terms of seams rather than files:

- A **storage layer** that owns reading/writing the JSON on disk and knows nothing about the UI or about Claude.
- An **agent-backend-agnostic runner boundary** — a small interface for "start a run, get back a stream of events, be able to cancel it" — with a Claude-CLI implementation behind it today. UI and orchestration code only ever see translated, backend-neutral events, never raw claude JSON, so a different backend could sit behind the same interface later without a rewrite.
- The **permission/question hook server** described in §4, as its own piece — it doesn't know about the UI directly, it just emits events onto a shared channel.
- An **orchestrator** that's the glue: turns "run this task" into a runner invocation plus hook registration, and funnels everything — task progress, approval requests, completion — into the events the UI consumes.
- The **Bubbletea UI layer** itself, shaped like a lazygit/k9s-style TUI: workspace tabs across the top, a task list on the left, a live streaming detail/log pane on the right, and modal overlays for creating a task, managing workspaces/repos, and — highest priority of all — the approval/question overlay from §4.
- A **worktree layer** that knows how to list, inspect, and clean up the worktrees Claude creates, cross-referenced against task records so a worktree still owned by a running task doesn't get removed out from under it.

## 8. Explicitly out of scope for now

- **ClickUp/Jira import via MCP.** The data model leaves room (`source`, `external_id`, `external_url` on Task) but no importer, no MCP client wiring, and no import UI exist yet.
- **Any agent backend other than Claude.** The runner interface is designed to allow it later; nothing else is implemented against it.
- **Steering a running task mid-flight via stdin.** Not needed today since the hook mechanism covers every form of input Claude currently asks for; would require pinning down the undocumented stdin wire format if ever pursued.

## 9. What's next

This document is the agreed reference point, not a build order. The next planning pass will break the architecture above into concrete, step-by-step implementation milestones — starting with the data model and storage layer, then the TUI shell, then wiring in Claude for plan mode, then the permission/question hook and complete mode, then worktree lifecycle management, then polish.
