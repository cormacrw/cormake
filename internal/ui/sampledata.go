package ui

import (
	"time"

	"github.com/google/uuid"

	"cormake/internal/domain"
)

// sampleData bundles the in-memory workspaces, tasks, and canned log lines
// used to drive the TUI shell before a real storage layer and claude
// subprocess integration exist.
type sampleData struct {
	Workspaces []domain.Workspace
	Tasks      []domain.Task
	Logs       map[string][]string
}

func newSampleData() sampleData {
	now := time.Now()

	defaultWS := uuid.NewString()
	workWS := uuid.NewString()
	ossWS := uuid.NewString()

	apiRepo := uuid.NewString()
	billingRepo := uuid.NewString()
	frontendRepo := uuid.NewString()
	cormakeRepo := uuid.NewString()

	workspaces := []domain.Workspace{
		{
			ID:   defaultWS,
			Name: "default",
			Repos: []domain.Repo{
				{ID: apiRepo, Name: "api", Path: "~/code/api", AddedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:   workWS,
			Name: "work",
			Repos: []domain.Repo{
				{ID: billingRepo, Name: "billing-service", Path: "~/code/billing-service", AddedAt: now},
				{ID: frontendRepo, Name: "frontend", Path: "~/code/frontend", AddedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:   ossWS,
			Name: "oss",
			Repos: []domain.Repo{
				{ID: cormakeRepo, Name: "cormake", Path: "~/cormacdev/task-manager-cli", AddedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	fixBugID := uuid.NewString()
	addDocsID := uuid.NewString()
	refactorID := uuid.NewString()
	webhookID := uuid.NewString()
	flakyTestID := uuid.NewString()

	tasks := []domain.Task{
		{
			ID:          fixBugID,
			WorkspaceID: defaultWS,
			RepoID:      apiRepo,
			Title:       "fix bug in payments module",
			Description: "Payments retries are double-charging on timeout.",
			Mode:        domain.ModeComplete,
			Status:      domain.StatusRunning,
			SessionID:   uuid.NewString(),
			Cost:        0.03,
			Source:      "manual",
			CreatedAt:   now,
		},
		{
			ID:            addDocsID,
			WorkspaceID:   defaultWS,
			RepoID:        apiRepo,
			Title:         "add docs for auth flow",
			Description:   "Document the OAuth token refresh flow for new contributors.",
			Mode:          domain.ModePlan,
			Status:        domain.StatusCompleted,
			ResultSummary: "Proposed a docs/auth.md outline covering token issuance and refresh.",
			Cost:          0.01,
			Source:        "manual",
			CreatedAt:     now,
		},
		{
			ID:           refactorID,
			WorkspaceID:  defaultWS,
			RepoID:       apiRepo,
			Title:        "refactor billing client",
			Description:  "Extract the billing HTTP client into its own package.",
			Mode:         domain.ModeComplete,
			Status:       domain.StatusFailed,
			ErrorMessage: "worktree build failed: missing generated protobuf types",
			Cost:         0.05,
			Source:       "manual",
			CreatedAt:    now,
		},
		{
			ID:          webhookID,
			WorkspaceID: workWS,
			RepoID:      billingRepo,
			Title:       "add retry logic to webhook sender",
			Description: "Webhook delivery should retry with backoff on 5xx.",
			Mode:        domain.ModeComplete,
			Status:      domain.StatusPending,
			Source:      "manual",
			CreatedAt:   now,
		},
		{
			ID:          flakyTestID,
			WorkspaceID: workWS,
			RepoID:      frontendRepo,
			Title:       "investigate flaky checkout test",
			Description: "checkout_test.go fails intermittently in CI.",
			Mode:        domain.ModePlan,
			Status:      domain.StatusAwaitingApproval,
			Source:      "manual",
			CreatedAt:   now,
		},
	}

	logs := map[string][]string{
		fixBugID: {
			"assistant: reading payments_test.go",
			"tool_use: Read(payments_test.go)",
			"tool_result: def test_charge_retry(): ...",
			"assistant: found it — the retry wrapper re-sends the charge instead of polling status",
			"tool_use: Read(payments.py)",
			"tool_result: class PaymentClient: ...",
			"assistant: patching the retry handler to poll before re-charging",
			"tool_use: Edit(payments.py)",
			"tool_result: applied",
			"assistant: running the test suite",
			"tool_use: Bash(pytest tests/payments)",
			"tool_result: 12 passed in 1.84s",
			"assistant: all payments tests pass, continuing to check webhook consumers",
			"tool_use: Grep(PaymentClient)",
			"tool_result: 3 matches across 2 files",
			"assistant: no other callers assume the old retry behavior, safe to proceed",
			"tool_use: Bash(git status --porcelain)",
			"tool_result: M payments.py",
			"assistant: staging the fix",
			"tool_use: Bash(git add payments.py)",
			"tool_result: staged",
			"assistant: drafting a commit message",
			"tool_use: Bash(git commit -m \"fix: poll charge status before retrying\")",
			"tool_result: [task-a1b2 3f9c2d1] fix: poll charge status before retrying",
			"assistant: double-checking the webhook consumer path one more time before wrapping up",
			"tool_use: Read(webhooks/consumer.py)",
			"tool_result: def handle_payment_webhook(event): ...",
			"assistant: consumer already treats duplicate charge IDs as idempotent, no further changes needed",
		},
		addDocsID: {
			"assistant: exploring the auth module",
			"tool_use: Glob(**/auth/*.go)",
			"tool_result: auth/token.go, auth/refresh.go, auth/middleware.go",
			"assistant: reading token.go and refresh.go",
			"tool_use: Read(auth/refresh.go)",
			"tool_result: func RefreshToken(...) ...",
			"assistant: drafted docs/auth.md covering issuance, refresh, and expiry handling",
			"result: proposed a docs/auth.md outline covering token issuance and refresh",
		},
		refactorID: {
			"assistant: extracting billing client into its own package",
			"tool_use: Bash(mkdir -p internal/billingclient)",
			"tool_result: created",
			"tool_use: Edit(internal/billing/client.go)",
			"tool_result: applied",
			"assistant: running the build",
			"tool_use: Bash(go build ./...)",
			"tool_result: internal/billingclient/client.go:14: undefined: pb.ChargeRequest",
			"assistant: the billing proto types aren't generated in this worktree",
			"result: worktree build failed: missing generated protobuf types",
		},
		webhookID: {
			"task queued — not started yet",
		},
		flakyTestID: {
			"assistant: looking at checkout_test.go for flakiness",
			"tool_use: Read(checkout_test.go)",
			"tool_result: func TestCheckoutFlow(t *testing.T) { ... }",
			"assistant: this test seems to depend on wall-clock timing, which two approaches could fix",
			"tool_use: AskUserQuestion",
			"awaiting your answer: should the fix use a fake clock, or increase the timeout?",
		},
	}

	return sampleData{Workspaces: workspaces, Tasks: tasks, Logs: logs}
}
