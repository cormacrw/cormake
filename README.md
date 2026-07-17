# cormake

A terminal "command center" for delegating coding work to [Claude](https://claude.com). Create a task, point it at a git repo, and choose **plan** mode (Claude researches the codebase and proposes an approach without touching code) or **execute** mode (Claude works autonomously inside an isolated git worktree).

See [VISION.md](VISION.md) for the full design rationale and architecture.

## Requirements

- Go 1.20+
- The `claude` CLI installed and authenticated

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

## Concepts

- **Workspace** — a named grouping of git repos plus the tasks created against them. A default workspace is created on first run but starts empty. Each workspace has its own readable-id `Prefix` (e.g. `ACME`, hand-set by editing `workspaces.json`; auto-derived from the workspace name if unset) used to number its tasks.
- **Repo** — a git repo added to a workspace; tasks created in that workspace pick one of its repos.
- **Task** — has a title, description, workspace, repo, mode (`plan`/`execute`), and moves through a status lifecycle from creation to completion. Each task gets a short readable id from its workspace's prefix (e.g. `ACME-7`). Execute-mode tasks run in their own worktree at `<repo>/.claude/worktrees/<task-id>` (named after that readable id, lowercased, so the worktree/branch is easy to match back to the task), so nothing they do touches your main working copy until you merge it.

## Keybindings

| Key | Action |
| --- | --- |
| `n` | New task |
| `enter` | Edit selected task's title/body |
| `p` | Plan (read-only research run) |
| `e` | Execute (autonomous run in a worktree) |
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
internal/agent/  agent-backend-agnostic runner boundary, with a Claude CLI implementation
internal/ui/     Bubbletea TUI (task list, detail pane, editor, review/diff view)
```
