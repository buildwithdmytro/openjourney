package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractMission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	writeTestFile(t, path, "intro\n\n```text\nDO ONE TASK\n```\n\noutro\n")
	got, err := extractMission(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "DO ONE TASK" {
		t.Fatalf("mission = %q", got)
	}
}

func TestReadTasksTracksMultilineDoneNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.md")
	writeTestFile(t, path, `# Plan
## 6. Task list
### Milestone 11.0 — First
1. **One.** Work.
   *Done when:* yes. — done: evidence
2. [x] **Two.** Work.
3. **Three.** Work.
### Milestone 11.1 — Second
1. **Four.** Work.
## 7. Invariants
1. not a task
`)
	tasks, err := readTasks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 {
		t.Fatalf("got %d tasks: %#v", len(tasks), tasks)
	}
	if !tasks[0].Done || !tasks[1].Done || tasks[2].Done || tasks[2].ID != "11.0.3" || tasks[3].ID != "11.1.1" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
	if got := firstTODO(tasks); got == nil || got.ID != "11.0.3" {
		t.Fatalf("first TODO = %#v", got)
	}
}

func TestCurrentMilestonePlanParsesInDocumentOrder(t *testing.T) {
	tasks, err := readTasks(filepath.Join("..", "..", "docs", "milestones", "v1-milestone-18-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 18 {
		t.Fatalf("got %d tasks, want 18", len(tasks))
	}
	if tasks[0].ID != "23.0.1" {
		t.Fatalf("first task = %#v", tasks[0])
	}
	if tasks[len(tasks)-1].ID != "23.7.2" {
		t.Fatalf("last task = %#v", tasks[len(tasks)-1])
	}
}

func TestProgressLine(t *testing.T) {
	tasks := []task{{Done: true}, {Done: true}, {ID: "11.1.1"}, {ID: "11.1.2"}}
	got := progressLine(tasks, 10)
	if got != "Progress [#####-----] 2/4 (50.0%) next: 11.1.1" {
		t.Fatalf("progress = %q", got)
	}
}

func TestCodexStreamCapturesUsageAndDisplaysAgentMessage(t *testing.T) {
	var log bytes.Buffer
	var display bytes.Buffer
	stream := &codexStream{log: &log, display: &display}
	input := "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"task complete\"}}\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":120,\"cached_input_tokens\":80,\"output_tokens\":30,\"reasoning_output_tokens\":10}}\n"
	if _, err := stream.Write([]byte(input[:37])); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write([]byte(input[37:])); err != nil {
		t.Fatal(err)
	}
	stream.flush()
	if log.String() != input {
		t.Fatalf("raw log changed: %q", log.String())
	}
	if display.String() != "task complete\n" {
		t.Fatalf("display = %q", display.String())
	}
	if !stream.foundUsage || stream.usage.InputTokens != 120 || stream.usage.CachedInputTokens != 80 || stream.usage.OutputTokens != 30 || stream.usage.ReasoningOutputTokens != 10 {
		t.Fatalf("usage = %#v, found=%t", stream.usage, stream.foundUsage)
	}
}

func TestClaudeStreamDisplaysLiveOutputAndCapturesUsage(t *testing.T) {
	var log bytes.Buffer
	var display bytes.Buffer
	stream := &claudeStream{log: &log, display: &display}
	input := `{"type":"assistant","message":{"content":[{"type":"text","text":"working on 16.1.1"},{"type":"tool_use","name":"Edit"}]}}` + "\n" +
		`{"type":"result","subtype":"success","usage":{"input_tokens":200,"cache_read_input_tokens":150,"output_tokens":40}}` + "\n"
	// split mid-line to prove the newline framing reassembles across writes
	if _, err := stream.Write([]byte(input[:50])); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write([]byte(input[50:])); err != nil {
		t.Fatal(err)
	}
	stream.flush()
	if log.String() != input {
		t.Fatalf("raw log changed: %q", log.String())
	}
	if display.String() != "working on 16.1.1\n[tool] Edit\n" {
		t.Fatalf("display = %q", display.String())
	}
	if !stream.foundUsage || stream.usage.InputTokens != 200 || stream.usage.CachedInputTokens != 150 || stream.usage.OutputTokens != 40 {
		t.Fatalf("usage = %#v, found=%t", stream.usage, stream.foundUsage)
	}
}

func TestProviderCommandsAreFreshAndUseLockedModels(t *testing.T) {
	cfg := config{root: "/repo", codexModel: "gpt-5.6-luna", antigravityModel: "gemini-3.6-flash-medium", claudeModel: "haiku", attemptTimeout: 2 * time.Hour}
	codex := providerCommand(context.Background(), cfg, "codex", "mission")
	joined := strings.Join(codex.Args, " ")
	if !strings.Contains(joined, "exec --json --model gpt-5.6-luna") || !strings.Contains(joined, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("unexpected Codex command: %s", joined)
	}
	agy := providerCommand(context.Background(), cfg, "antigravity", "mission")
	joined = strings.Join(agy.Args, " ")
	if !strings.Contains(joined, "gemini-3.6-flash-medium") || !strings.Contains(joined, "--dangerously-skip-permissions") || !strings.Contains(joined, "--print mission") {
		t.Fatalf("unexpected Antigravity command: %s", joined)
	}
	claude := providerCommand(context.Background(), cfg, "claude", "mission")
	joined = strings.Join(claude.Args, " ")
	if !strings.Contains(joined, "claude --print --model haiku") || !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("unexpected Claude command: %s", joined)
	}
	// stream-json (+ required --verbose) is what makes Claude log live instead of
	// emitting one silent blob at the end; keep them wired.
	if !strings.Contains(joined, "--output-format stream-json") || !strings.Contains(joined, "--verbose") {
		t.Fatalf("Claude must stream live output: %s", joined)
	}
	if claude.Stdin == nil {
		t.Fatal("Claude mission must be passed on stdin")
	}
	if alternate("claude") != "codex" {
		t.Fatalf("claude fallback = %q, want codex", alternate("claude"))
	}
}

func TestRunFallsBackAndCompletesSameTask(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "ralph@example.test")
	git(t, repo, "config", "user.name", "Ralph Test")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), ".ralph/\n")
	writeTestFile(t, filepath.Join(repo, "prompt.md"), "```text\nDO ONE TASK\n```\n")
	writeTestFile(t, filepath.Join(repo, "plan.md"), "## 6. Task list\n### Milestone 11.0 — First\n1. **One.** Work.\n## 7. Invariants\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "test fixture")

	binDir := t.TempDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/bin/sh
if [ "$1" = "debug" ]; then
  printf '%s\n' '{"models":[{"slug":"gpt-5.6-luna"}]}'
  exit 0
fi
printf 'partial\n' > partial.txt
exit 17
`)
	writeExecutable(t, filepath.Join(binDir, "agy"), `#!/bin/sh
if [ "$1" = "models" ]; then
  printf '%s\n' 'gemini-3.6-flash-medium'
  exit 0
fi
sed -i 's/^1\. \*\*One/1. [x] **One/' plan.md
printf '%s\n' ' — done: fallback completed the fixture.' >> plan.md
git add plan.md partial.txt
git commit -m 'feat: complete 11.0.1'
printf '%s\n' '11.0.1 complete'
`)

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	cfg := config{
		primary:          "codex",
		promptPath:       "prompt.md",
		planPath:         "plan.md",
		branch:           "phase6",
		milestone:        "10",
		maxIterations:    1,
		attemptTimeout:   time.Minute,
		codexModel:       "gpt-5.6-luna",
		antigravityModel: "gemini-3.6-flash-medium",
		unsafe:           true,
	}
	if err := run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if branch := strings.TrimSpace(git(t, repo, "branch", "--show-current")); branch != "phase6" {
		t.Fatalf("branch = %q", branch)
	}
	if count := strings.TrimSpace(git(t, repo, "rev-list", "--count", "main..phase6")); count != "1" {
		t.Fatalf("agent commit count = %q", count)
	}
	tasks, err := readTasks(filepath.Join(repo, "plan.md"))
	if err != nil || len(tasks) != 1 || !tasks[0].Done {
		t.Fatalf("tasks = %#v, err = %v", tasks, err)
	}
	usageBytes, err := os.ReadFile(filepath.Join(repo, ".ralph", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(usageBytes), `"attempts": 2`) || !strings.Contains(string(usageBytes), `"completed_tasks": 1`) {
		t.Fatalf("usage summary = %s", usageBytes)
	}
}

func TestValidateAttemptStopsOnBlocker(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "plan.md"), "## 6. Task list\n### Milestone 11.0 — First\n1. **One.** Work.\n## 7. Invariants\n")
	blocker := filepath.Join(repo, "docs/milestones/BLOCKERS.md")
	before, err := fileDigest(blocker)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, blocker, "11.0.1 blocked\n")
	outcome, err := validateAttempt(config{root: repo, planPath: "plan.md"}, task{ID: "11.0.1"}, "unused", before)
	if err != nil || outcome != "blocked" {
		t.Fatalf("outcome=%q err=%v", outcome, err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeTestFile(t, path, content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
