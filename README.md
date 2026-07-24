# cormake

Hello world!

A terminal "command center" for delegating coding work to [Claude](https://claude.com) or [Cursor](https://cursor.com). Create a task, point it at a git repo, and choose **plan** mode (the agent researches the codebase and proposes an approach without touching code) or **execute** mode (the agent works autonomously inside an isolated git worktree).

See [VISION.md](VISION.md) for the full design rationale and architecture.

## Requirements

- Go 1.20+
- At least one supported agent CLI installed and authenticated:
  - [Claude Code](https://claude.com/product/claude-code) (`claude`)
  - [Cursor](https://cursor.com) (`cursor-agent`)

## Build & run

```sh
go build -o cormake ./cmd/cormake
./cormake
```

or run directly without building a binary:

```sh
go run ./cmd/cormake
```

## Storage

All state — workspaces, repos, tasks, session transcripts — is stored as flat JSON under `~/.cormake` (override with the `CORMAKE_HOME` environment variable, useful for tests or trying the app against a scratch directory). There's no server, no database, no account.

## Agent backends

cormake supports two agent CLIs behind the same Plan/Execute workflow:

| Backend | CLI | Config value |
| --- | --- | --- |
| Claude | `claude` | `"claude"` |
| Cursor | `cursor-agent` | `"cursor"` |

Both backends support plan and execute modes, work inside the same git worktrees, and resume their own sessions for review feedback and follow-up messages (`i`).

**Choosing a backend per run.** When you confirm Plan (`p`) or Execute (`e`), the modal shows an agent toggle — press `tab` (or `←`/`→`) to switch between `claude` and `cursor`, then `y` or `enter` to start.

**Workspace default.** Set `DefaultAgentBackend` in [workspace config](#workspace-config) to `"claude"` or `"cursor"` to preselect that backend in the confirmation modal. Omitted defaults to `"claude"`. Each task remembers whichever backend started its session; later resumes always go back to that same agent.

## Concepts

- **Workspace** — a named grouping of git repos plus the tasks created against them. A default workspace is created on first run but starts empty. See [Workspace config](#workspace-config) below for the hand-editable settings each workspace has (readable-id prefix, agent concurrency limit, etc).
- **Repo** — a git repo added to a workspace; tasks created in that workspace pick one of its repos.
- **Task** — has a title, description, workspace, repo, mode (`plan`/`execute`), and moves through a status lifecycle from creation to completion. Each task gets a short readable id from its workspace's prefix (e.g. `ACME-7`). Execute-mode tasks run in their own worktree at `<repo>/.claude/worktrees/<task-id>` (named after that readable id, lowercased, so the worktree/branch is easy to match back to the task), so nothing they do touches your main working copy until you merge it.

## Workspace config

There's no settings UI yet — per-workspace options are hand-edited directly in `~/.cormake/workspaces.json` (or `$CORMAKE_HOME/workspaces.json`) while cormake isn't running. Example entry showing every option:

```json
{
  "ID": "3f1b2c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
  "Name": "acme",
  "Repos": [
    {
      "ID": "9a8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d",
      "Name": "acme-api",
      "Path": "/Users/you/code/acme-api",
      "AddedAt": "2026-01-05T10:00:00Z"
    }
  ],
  "PrimaryColor": "#ff8800",
  "Prefix": "ACME",
  "NextTaskNumber": 8,
  "MaxConcurrentAgents": 3,
  "DefaultTargetBranch": "main",
  "DefaultAgentBackend": "cursor",
  "CreatedAt": "2026-01-05T10:00:00Z",
  "UpdatedAt": "2026-01-05T10:00:00Z"
}
```

- `PrimaryColor` — accent color for this workspace's UI (hex string).
- `Prefix` — readable-id prefix for its tasks, e.g. `ACME` -> `ACME-7`. Auto-derived from `Name` if unset.
- `NextTaskNumber` — next sequence number `Prefix` will hand out; doesn't reset when `Prefix` is edited by hand.
- `MaxConcurrentAgents` — caps how many Plan/Execute agents can run at once across this workspace's tasks. `0` (or omitted) means "unset" and falls back to a default of `1`, not "unlimited" — raise it deliberately if you want more agents running in parallel.
- `DefaultTargetBranch` — the branch a new task's work should merge into by default; preselected (and listed first) in the new-task wizard's source-branch step. Omitted means "unset" and falls back to `"develop"`.
- `DefaultAgentBackend` — which agent CLI Plan/Execute runs in this workspace default to in the confirmation modal: `"claude"` or `"cursor"`. Omitted means "unset" and falls back to `"claude"`.

## Keybindings

| Key | Action |
| --- | --- |
| `n` | New task |
| `enter` | Edit selected task's title/body |
| `p` | Plan (read-only research run) |
| `e` | Execute (autonomous run in a worktree) |
| `tab` | Switch agent backend in the Plan/Execute confirmation modal |
| `r` | Review the task's plan or diff |
| `m` | Mark a ready-for-review task complete |
| `a` | Archive / restore a task |
| `d` | Delete a task |
| `w` | Workspace picker |
| `1`-`4`, `[` `]` | Switch detail tabs (Description/Plan/Summary/Log) |
| `↑`/`↓`/`←`/`→` | Navigate / switch tabs |
| `q` | Quit |

## Project layout

```
cmd/cormake/     app entrypoint
cmd/agentpoc/    throwaway harness for testing the Claude runner in isolation
internal/domain/ task and workspace data model
internal/store/  flat-JSON storage layer (atomic writes)
internal/agent/  agent-backend-agnostic runner boundary, with Claude and Cursor CLI implementations
internal/ui/     Bubbletea TUI (task list, detail pane, editor, review/diff view)
```
