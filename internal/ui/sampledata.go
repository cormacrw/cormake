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
	rateLimitID := uuid.NewString()
	landingPageID := uuid.NewString()
	webhookID := uuid.NewString()
	flakyTestID := uuid.NewString()
	cssCleanupID := uuid.NewString()
	onboardingEmailID := uuid.NewString()

	tasks := []domain.Task{
		{
			ID:          fixBugID,
			WorkspaceID: defaultWS,
			RepoID:      apiRepo,
			Title:       "fix bug in payments module",
			Description: "Payments retries are double-charging on timeout.",
			Status:      domain.StatusInProgress,
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
			Status:        domain.StatusReadyForReview,
			ResultSummary: "Opened PR #217 with a new docs/auth.md page.",
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
			Status:       domain.StatusFailed,
			ErrorMessage: "worktree build failed: missing generated protobuf types",
			Cost:         0.05,
			Source:       "manual",
			CreatedAt:    now,
		},
		{
			ID:            rateLimitID,
			WorkspaceID:   defaultWS,
			RepoID:        apiRepo,
			Title:         "add rate limiting to public API",
			Description:   "Protect the public API gateway with a per-IP token bucket limiter.",
			Status:        domain.StatusComplete,
			ResultSummary: "Shipped in PR #482.",
			Cost:          0.08,
			Source:        "manual",
			CreatedAt:     now,
		},
		{
			ID:             landingPageID,
			WorkspaceID:    defaultWS,
			RepoID:         apiRepo,
			Title:          "old marketing landing page tweak",
			Description:    "Swap the hero image on the pricing page — deprioritized for now.",
			Status:         domain.StatusArchived,
			PreviousStatus: domain.StatusTodo,
			Source:         "manual",
			CreatedAt:      now,
		},
		{
			ID:          webhookID,
			WorkspaceID: workWS,
			RepoID:      billingRepo,
			Title:       "add retry logic to webhook sender",
			Description: "Webhook delivery should retry with backoff on 5xx.",
			Status:      domain.StatusTodo,
			Source:      "manual",
			CreatedAt:   now,
		},
		{
			ID:          flakyTestID,
			WorkspaceID: workWS,
			RepoID:      frontendRepo,
			Title:       "investigate flaky checkout test",
			Description: "checkout_test.go fails intermittently in CI.",
			Status:      domain.StatusPlanning,
			Source:      "manual",
			CreatedAt:   now,
		},
		{
			ID:          cssCleanupID,
			WorkspaceID: workWS,
			RepoID:      frontendRepo,
			Title:       "clean up unused CSS classes",
			Description: "Remove dead CSS left over from the last redesign.",
			Status:      domain.StatusAwaitingApproval,
			Source:      "manual",
			CreatedAt:   now,
		},
		{
			ID:            onboardingEmailID,
			WorkspaceID:   workWS,
			RepoID:        frontendRepo,
			Title:         "simplify onboarding email flow",
			Description:   "Three onboarding emails largely repeat themselves.",
			Status:        domain.StatusPlanned,
			ResultSummary: "Merge day2 into welcome, simplify day7, drop 2 redundant CTAs.",
			Cost:          0.02,
			Source:        "manual",
			CreatedAt:     now,
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
			"assistant: opened PR #217 with the new docs page — ready for your review",
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
		rateLimitID: {
			"assistant: adding a token bucket rate limiter to the public API gateway",
			"tool_use: Read(gateway/middleware.go)",
			"tool_result: func Chain(mw ...Middleware) Middleware { ... }",
			"assistant: implementing a per-IP token bucket RateLimit middleware",
			"tool_use: Edit(gateway/ratelimit.go)",
			"tool_result: applied",
			"assistant: running the test suite",
			"tool_use: Bash(go test ./gateway/...)",
			"tool_result: ok  \tcormake/gateway\t0.412s",
			"assistant: opened PR #482",
			"result: shipped rate limiting to the public API gateway",
		},
		landingPageID: {
			"task queued — archived before starting",
		},
		webhookID: {
			"task queued — not started yet",
		},
		flakyTestID: {
			"assistant: looking at checkout_test.go for flakiness",
			"tool_use: Read(checkout_test.go)",
			"tool_result: func TestCheckoutFlow(t *testing.T) { ... }",
			"assistant: this test seems to depend on wall-clock timing",
			"tool_use: Grep(time.Now)",
			"tool_result: 4 matches in checkout_test.go",
			"assistant: drafting a plan: inject a fake clock instead of increasing timeouts",
		},
		cssCleanupID: {
			"assistant: scanning for unused CSS classes",
			"tool_use: Bash(npx purgecss --css styles/*.css --content src/**/*.tsx)",
			"tool_result: 23 unused classes found across 4 files",
			"assistant: two of these are referenced dynamically via template strings, which static analysis can't see",
			"tool_use: AskUserQuestion",
			"awaiting your answer: remove the 2 dynamically-referenced classes anyway, or keep them to be safe?",
		},
		onboardingEmailID: {
			"assistant: reviewing the onboarding email templates",
			"tool_use: Glob(emails/onboarding/*.mjml)",
			"tool_result: welcome.mjml, day2.mjml, day7.mjml",
			"assistant: day2 and day7 largely duplicate welcome's content",
			"assistant: proposed plan: merge day2 into welcome, keep day7 as a distinct check-in, drop 2 redundant CTAs",
			"result: plan ready — awaiting your go-ahead to execute",
		},
	}

	return sampleData{Workspaces: workspaces, Tasks: tasks, Logs: logs}
}
