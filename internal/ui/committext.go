package ui

import "strings"

const commitDescriptionFence = "```cormake-commit"

// parseAgentResult splits an agent's final message into the user-facing
// summary (shown in the Summary tab and revdiff context) and an optional
// commit description block — short bullet points extracted for git commit
// bodies (see executeSummaryInstruction).
func parseAgentResult(text string) (summary, commitDesc string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	commitDesc, rest, ok := extractCommitDescriptionBlock(text)
	if !ok {
		return strings.TrimSpace(text), ""
	}
	return strings.TrimSpace(rest), commitDesc
}

// extractCommitDescriptionBlock pulls the fenced ```cormake-commit block
// out of text, returning the block body and the remaining prose.
func extractCommitDescriptionBlock(text string) (commitDesc, rest string, ok bool) {
	start := strings.Index(text, commitDescriptionFence)
	if start == -1 {
		return "", text, false
	}
	afterOpen := start + len(commitDescriptionFence)
	if afterOpen < len(text) && text[afterOpen] == '\n' {
		afterOpen++
	}
	endRel := strings.Index(text[afterOpen:], "```")
	if endRel == -1 {
		return "", text, false
	}
	commitDesc = strings.TrimSpace(text[afterOpen : afterOpen+endRel])
	afterClose := afterOpen + endRel + len("```")
	rest = strings.TrimSpace(text[:start] + text[afterClose:])
	if commitDesc == "" {
		return "", text, false
	}
	return commitDesc, rest, true
}

// commitBodyFromTask returns the text to append to a cormake commit message
// body after the subject line — preferring the parsed commit-description
// block, falling back to ResultSummary when the agent omitted the block.
func commitBodyFromTask(summary, commitDesc string) string {
	if body := strings.TrimSpace(commitDesc); body != "" {
		return body
	}
	return strings.TrimSpace(summary)
}
