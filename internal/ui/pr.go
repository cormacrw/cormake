// Opening/tracking a pull request for a READY_FOR_REVIEW task: kicking off
// a claude run that pushes the branch and creates the PR via gh
// (startOpenPR/buildOpenPRPrompt), then periodically polling gh for its
// description/comments/merge state while the task sits IN_REVIEW
// (queryPR/ensurePRPollTicking), plus opening it in a browser (openURLCmd).
package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cormake/internal/domain"
	"cormake/internal/logformat"
)

// prPollInterval is how often an IN_REVIEW task's PR gets re-fetched from
// GitHub while its task list/detail pane is potentially on screen — cheap
// enough (one `gh pr view` per open PR, there's rarely more than one or two
// at a time) to run in the background regardless of what's currently
// selected, so a merge or a new review comment shows up without the user
// having to reselect the task to trigger a refresh.
const prPollInterval = 30 * time.Second

// startOpenPR kicks off the claude run openPRConfirm staged (by ID, same
// resolve-once pattern as startCompleteTask/startDeleteTask) — a
// Complete-mode resume of the task's existing execute session (so claude
// has full context of what it already built) with a prompt asking it to
// push the branch and open the PR via gh (see buildOpenPRPrompt). The task
// moves to StatusOpeningPR for the run's duration; handleTaskFinished kicks
// off the gh query that confirms it actually landed and moves the task on
// to StatusInReview (see queryPRStatusCmd/handlePRQuery).
func (m *Model) startOpenPR() tea.Cmd {
	var target domain.Task
	found := false
	for _, t := range m.tasks {
		if t.ID == m.confirmTaskID {
			target = t
			found = true
			break
		}
	}
	if !found || target.WorktreePath == "" || target.SessionID == "" {
		return nil
	}

	prSkill := ""
	for _, w := range m.workspaces {
		if w.ID == target.WorkspaceID {
			prSkill = w.PRSkill
			break
		}
	}

	m.appendLogLine(target.ID, logformat.LogCormakeLine("opening PR with "+logformat.AgentBackendLabel(target.AgentBackend)))
	return m.runExecuteAgent(target, buildOpenPRPrompt(target, prSkill), target.SessionID, domain.StatusOpeningPR)
}

// buildOpenPRPrompt asks claude to push the task's branch and open a PR for
// it with the gh CLI. Pushing is spelled out explicitly rather than left to
// `gh pr create`'s own interactive push offer — that offer requires a TTY
// this headless (-p) run doesn't have, so without an explicit instruction
// it would just hang or fail with the branch never having reached origin.
//
// PRSkill, when the workspace has one configured (domain.Workspace.PRSkill),
// takes precedence over the repo's own pull request template — the whole
// point of the setting. Otherwise the prompt points at the template's usual
// locations and asks claude to fill it out, falling back to a plain
// description if the repo has none.
func buildOpenPRPrompt(t domain.Task, prSkill string) string {
	base := t.SourceBranch
	if base == "" {
		base = domain.DefaultSourceBranch
	}
	head := t.TargetBranch
	if head == "" {
		head = t.WorktreeName
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Open a pull request for the work on this task, from branch %q into %q.\n\n", head, base)
	fmt.Fprintf(&b, "First push the current branch to origin (git push -u origin %s), then create the PR with the gh CLI (gh pr create).\n\n", head)

	if prSkill != "" {
		fmt.Fprintf(&b, "Use your %q skill to write the PR description, taking precedence over any pull request template in this repo.\n\n", prSkill)
	} else {
		b.WriteString("Favor this repo's pull request template if it has one (e.g. .github/PULL_REQUEST_TEMPLATE.md, " +
			".github/pull_request_template.md, or docs/PULL_REQUEST_TEMPLATE.md) and fill it out accordingly; " +
			"otherwise write a clear description covering what changed and why.\n\n")
	}

	b.WriteString("For context, here is what this task set out to do and a summary of the work already completed:\n\n")
	b.WriteString(t.Title)
	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("\n\n" + t.Description)
	}
	if strings.TrimSpace(t.ResultSummary) != "" {
		b.WriteString("\n\n" + t.ResultSummary)
	}

	b.WriteString("\n\nWhen you are finished, your final message must state the PR's URL plainly.")
	return b.String()
}

// prQueryMsg reports queryPRStatusCmd's result. forCreation marks the
// one-shot query handleTaskFinished kicks off right after a StatusOpeningPR
// run to confirm the PR actually landed, as opposed to an ordinary
// background poll of an already-open one (see handlePRQuery, which treats
// "not found" very differently depending on which this is).
type prQueryMsg struct {
	taskID      string
	forCreation bool
	snapshot    domain.PRSnapshot
	found       bool
	err         error
}

// queryPRStatusCmd shells out to gh (via queryPR) for taskID's PR on
// branch, run from within worktreePath so gh infers the right repo/remote
// from the worktree's own git config.
func queryPRStatusCmd(taskID, worktreePath, branch string, forCreation bool) tea.Cmd {
	return func() tea.Msg {
		snap, found, err := queryPR(worktreePath, branch)
		return prQueryMsg{taskID: taskID, forCreation: forCreation, snapshot: snap, found: found, err: err}
	}
}

// ghPRView mirrors the subset of `gh pr view --json ...`'s output this
// package actually uses — confirmed directly against real gh output rather
// than assumed from docs alone.
type ghPRView struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`

	Comments []struct {
		Author    struct{ Login string } `json:"author"`
		Body      string                 `json:"body"`
		CreatedAt time.Time              `json:"createdAt"`
	} `json:"comments"`

	// Reviews double as PR comments in this pane: a review with no body
	// (a bare approve/request-changes click) is skipped in queryPR, but one
	// with a body reads exactly like a comment, just tagged with its review
	// state instead of a generic "comment" kind.
	Reviews []struct {
		Author      struct{ Login string } `json:"author"`
		Body        string                 `json:"body"`
		State       string                 `json:"state"`
		SubmittedAt time.Time              `json:"submittedAt"`
	} `json:"reviews"`
}

// queryPR runs `gh pr view <branch> --json ...` from within worktreePath
// and translates the result into a domain.PRSnapshot. found is false when
// gh reports no PR exists for branch — the caller treats that as a
// legitimate "not created" (or "not created yet") rather than folding it
// into err, which is reserved for gh itself failing (not installed, not
// authenticated, network error, etc.).
func queryPR(worktreePath, branch string) (snapshot domain.PRSnapshot, found bool, err error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,url,title,body,state,comments,reviews")
	cmd.Dir = worktreePath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return domain.PRSnapshot{}, false, fmt.Errorf("%s", msg)
	}

	var v ghPRView
	if err := json.Unmarshal(out, &v); err != nil {
		return domain.PRSnapshot{}, false, err
	}
	if v.Number == 0 {
		return domain.PRSnapshot{}, false, nil
	}

	var comments []domain.PRComment
	for _, c := range v.Comments {
		comments = append(comments, domain.PRComment{
			Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt, Kind: "comment",
		})
	}
	for _, r := range v.Reviews {
		if strings.TrimSpace(r.Body) == "" {
			continue
		}
		comments = append(comments, domain.PRComment{
			Author: r.Author.Login, Body: r.Body, CreatedAt: r.SubmittedAt,
			Kind: strings.ToLower(strings.ReplaceAll(r.State, "_", " ")),
		})
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].CreatedAt.Before(comments[j].CreatedAt) })

	return domain.PRSnapshot{
		Number: v.Number, URL: v.URL, Title: v.Title, Body: v.Body, State: v.State,
		Comments: comments, FetchedAt: time.Now(),
	}, true, nil
}

// handlePRQuery reacts to a gh query's result, for either purpose
// prQueryMsg.forCreation distinguishes:
//
//   - forCreation (the one-shot check right after a StatusOpeningPR run):
//     not found/errored means the PR genuinely didn't get created — logged
//     and reverted back to StatusReadyForReview so the user can retry (see
//     openPRConfirm's eligibility check, which allows exactly that). Found
//     means it's confirmed real — move on to StatusInReview and start the
//     background poll (see ensurePRPollTicking).
//   - background poll (forCreation false): best-effort, matching this
//     codebase's other background-refresh patterns (persistTask,
//     appendLogLine) — a transient gh failure is silently ignored rather
//     than logged every 30s, and "not found" is left alone too (a PR isn't
//     expected to un-exist once created; if gh momentarily disagrees, the
//     next poll will self-correct).
func (m *Model) handlePRQuery(msg prQueryMsg) tea.Cmd {
	for i := range m.tasks {
		if m.tasks[i].ID != msg.taskID {
			continue
		}
		if msg.err != nil || !msg.found {
			if msg.forCreation {
				reason := "no pull request found for this branch"
				if msg.err != nil {
					reason = msg.err.Error()
				}
				m.appendLogLine(msg.taskID, logformat.LogCormakeLine("failed to open PR: "+reason))
				m.tasks[i].Status = domain.StatusReadyForReview
				m.persistTask(m.tasks[i])
				m.refreshTaskList()
				m.syncDetail()
			}
			return nil
		}

		wasOpening := m.tasks[i].Status == domain.StatusOpeningPR
		m.tasks[i].PRNumber = msg.snapshot.Number
		m.tasks[i].PRURL = msg.snapshot.URL
		m.detail.SetPRSnapshot(msg.taskID, msg.snapshot)

		if !wasOpening {
			m.persistTask(m.tasks[i])
			m.refreshTaskList()
			m.syncDetail()
			return nil
		}

		m.tasks[i].Status = domain.StatusInReview
		m.persistTask(m.tasks[i])
		m.appendLogLine(msg.taskID, logformat.LogCormakeLine(fmt.Sprintf("opened PR #%d: %s", msg.snapshot.Number, msg.snapshot.URL)))
		m.refreshTaskList()
		m.syncDetail()
		return m.ensurePRPollTicking()
	}
	return nil
}

// prPollTickMsg drives the background PR-polling chain, mirroring
// spinner.TickMsg's self-perpetuating pattern (see ensureSpinnerTicking):
// each tick re-issues itself (via handlePRPollTick) as long as at least one
// task is still IN_REVIEW, and stops cleanly once none are.
type prPollTickMsg struct{}

func prPollTickCmd() tea.Cmd {
	return tea.Tick(prPollInterval, func(time.Time) tea.Msg { return prPollTickMsg{} })
}

// ensurePRPollTicking (re)starts the polling chain if it isn't already
// running and at least one task is IN_REVIEW — called wherever a task can
// newly enter that status (handlePRQuery, once a PR is confirmed open) and
// from Init (see its own doc comment for why Init can't call this directly).
func (m *Model) ensurePRPollTicking() tea.Cmd {
	if m.prPollTicking {
		return nil
	}
	if !m.hasOpenPRTask() {
		return nil
	}
	m.prPollTicking = true
	return prPollTickCmd()
}

func (m Model) hasOpenPRTask() bool {
	for _, t := range m.tasks {
		if t.Status == domain.StatusInReview {
			return true
		}
	}
	return false
}

// handlePRPollTick fires one round of background PR queries (see
// pollOpenPRsCmd) and, if there's still at least one IN_REVIEW task once
// this round is kicked off, re-arms the chain; otherwise lets it stop.
func (m *Model) handlePRPollTick() tea.Cmd {
	if !m.hasOpenPRTask() {
		m.prPollTicking = false
		return nil
	}
	m.prPollTicking = true
	return tea.Batch(m.pollOpenPRsCmd(), prPollTickCmd())
}

// pollOpenPRsCmd fires one background gh query per IN_REVIEW task that
// still has a worktree on record (needed to resolve which repo/remote to
// query — see queryPR). Each fires its own prQueryMsg independently via
// tea.Batch rather than waiting on all of them together, so a slow one
// doesn't hold up the others.
func (m Model) pollOpenPRsCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tasks {
		if t.Status != domain.StatusInReview || t.WorktreePath == "" {
			continue
		}
		cmds = append(cmds, queryPRStatusCmd(t.ID, t.WorktreePath, t.TargetBranch, false))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// openURLMsg reports openURLCmd's result — best-effort, so the only thing
// done with a failure is logging it (see the ui.Model.Update case).
type openURLMsg struct {
	taskID string
	err    error
}

// openURLCmd opens url in the system's default browser — the OS-appropriate
// opener command, since there's no TTY/interactive prompt involved (unlike
// e.g. openInEditorCmd's tea.ExecProcess) and gh itself isn't guaranteed to
// be runnable from wherever the task's worktree used to be (Complete clears
// WorktreePath, but PRURL is kept for exactly this — see domain.Task.PRURL).
func openURLCmd(taskID, url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		return openURLMsg{taskID: taskID, err: cmd.Start()}
	}
}
