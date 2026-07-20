package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	exitPreflight = 2
	exitBlocked   = 3
	exitAttempts  = 4
	exitTimeout   = 5
	exitMax       = 6
)

type config struct {
	root             string
	primary          string
	promptPath       string
	planPath         string
	branch           string
	milestone        string
	maxIterations    int
	attemptTimeout   time.Duration
	codexModel       string
	antigravityModel string
	claudeModel      string
	unsafe           bool
	dryRun           bool
}

type task struct {
	ID   string
	Done bool
	Text string
}

type attemptResult struct {
	Provider              string        `json:"provider"`
	TaskID                string        `json:"task_id"`
	Started               time.Time     `json:"started_at"`
	Duration              time.Duration `json:"duration"`
	ExitCode              int           `json:"exit_code"`
	TimedOut              bool          `json:"timed_out"`
	TokenUsageAvailable   bool          `json:"token_usage_available"`
	InputTokens           int64         `json:"input_tokens,omitempty"`
	CachedInputTokens     int64         `json:"cached_input_tokens,omitempty"`
	OutputTokens          int64         `json:"output_tokens,omitempty"`
	ReasoningOutputTokens int64         `json:"reasoning_output_tokens,omitempty"`
	Error                 string        `json:"error,omitempty"`
}

type providerUsage struct {
	Attempts              int           `json:"attempts"`
	Duration              time.Duration `json:"duration"`
	TokenUsageAvailable   bool          `json:"token_usage_available"`
	InputTokens           int64         `json:"input_tokens,omitempty"`
	CachedInputTokens     int64         `json:"cached_input_tokens,omitempty"`
	OutputTokens          int64         `json:"output_tokens,omitempty"`
	ReasoningOutputTokens int64         `json:"reasoning_output_tokens,omitempty"`
}

type usageSummary struct {
	Started        time.Time                `json:"started_at"`
	Updated        time.Time                `json:"updated_at"`
	TotalTasks     int                      `json:"total_tasks"`
	CompletedTasks int                      `json:"completed_tasks"`
	Attempts       int                      `json:"attempts"`
	MaxIterations  int                      `json:"max_iterations"`
	Providers      map[string]providerUsage `json:"providers"`
}

type codexStream struct {
	log     io.Writer
	display io.Writer
	pending []byte
	usage   struct {
		InputTokens           int64 `json:"input_tokens"`
		CachedInputTokens     int64 `json:"cached_input_tokens"`
		OutputTokens          int64 `json:"output_tokens"`
		ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	}
	foundUsage bool
}

type runError struct {
	code int
	err  error
}

func (e *runError) Error() string { return e.err.Error() }

var (
	milestoneRE = regexp.MustCompile(`^### Milestone ([0-9]+\.[0-9]+)\b`)
	itemRE      = regexp.MustCompile(`^([0-9]+)\.\s+(?:\[([ xX])\]\s+)?`)
)

func main() {
	cfg := parseFlags()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ralph:", err)
		var re *runError
		if errors.As(err, &re) {
			os.Exit(re.code)
		}
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.primary, "primary", "", "primary provider: codex, antigravity, or claude (required)")
	flag.StringVar(&cfg.promptPath, "prompt", "prompt.md", "path to the Ralph mission prompt")
	flag.StringVar(&cfg.planPath, "plan", "docs/milestones/v1-milestone-10-plan.md", "path to the milestone plan")
	flag.StringVar(&cfg.branch, "branch", "phase10", "implementation branch")
	flag.StringVar(&cfg.milestone, "milestone", "10", "human milestone label for completion/error messages")
	flag.IntVar(&cfg.maxIterations, "max-iterations", 100, "maximum successful task iterations")
	flag.DurationVar(&cfg.attemptTimeout, "attempt-timeout", 2*time.Hour, "timeout for each provider attempt")
	flag.StringVar(&cfg.codexModel, "codex-model", "gpt-5.6-luna", "Codex model identifier")
	flag.StringVar(&cfg.antigravityModel, "antigravity-model", "Gemini 3.5 Flash (Medium)", "Antigravity model label")
	flag.StringVar(&cfg.claudeModel, "claude-model", "haiku", "Claude Code model alias or id (e.g. haiku, sonnet, opus)")
	flag.BoolVar(&cfg.unsafe, "unsafe-autonomous", false, "acknowledge that agents receive unrestricted autonomous permissions")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "validate configuration without changing Git state or invoking an agent")
	flag.Parse()
	return cfg
}

func run(ctx context.Context, cfg config) error {
	root, err := repositoryRoot()
	if err != nil {
		return preflightError(err)
	}
	cfg.root = root
	mission, tasks, err := preflight(ctx, cfg)
	if err != nil {
		return preflightError(err)
	}
	if cfg.dryRun {
		fmt.Printf("Preflight passed: %d tasks, next %s, primary %s, fallback %s\n", len(tasks), nextTaskID(tasks), cfg.primary, alternate(cfg.primary))
		return nil
	}
	if !cfg.unsafe {
		return preflightError(errors.New("--unsafe-autonomous is required because the agents must edit and commit without approval prompts"))
	}
	if err := requireClean(root); err != nil {
		return preflightError(err)
	}
	if err := ensureBranch(root, cfg.branch); err != nil {
		return preflightError(err)
	}
	usage := usageSummary{Started: time.Now(), TotalTasks: len(tasks), CompletedTasks: completedTaskCount(tasks), MaxIterations: cfg.maxIterations, Providers: make(map[string]providerUsage)}
	printProgress(tasks)
	printRemaining(usage, cfg.primary)

	for iteration := 1; iteration <= cfg.maxIterations; iteration++ {
		current, err := readTasks(filepath.Join(root, cfg.planPath))
		if err != nil {
			return preflightError(err)
		}
		next := firstTODO(current)
		if next == nil {
			fmt.Printf("MILESTONE %s COMPLETE\n", cfg.milestone)
			return nil
		}

		fmt.Printf("\nRalph iteration %d: task %s (primary: %s)\n", iteration, next.ID, cfg.primary)
		beforeHead, err := gitOutput(root, "rev-parse", "HEAD")
		if err != nil {
			return &runError{code: exitAttempts, err: err}
		}
		beforeBlocker, err := fileDigest(filepath.Join(root, "docs/milestones/BLOCKERS.md"))
		if err != nil {
			return &runError{code: exitAttempts, err: err}
		}

		result := invoke(ctx, cfg, cfg.primary, mission, next.ID, iteration)
		usage.add(result)
		usage.save(root)
		printUsage(usage, result)
		printRemaining(usage, cfg.primary)
		outcome, err := validateAttempt(cfg, *next, beforeHead, beforeBlocker)
		if outcome == "success" {
			updated, _ := readTasks(filepath.Join(root, cfg.planPath))
			usage.CompletedTasks = completedTaskCount(updated)
			usage.save(root)
			printProgress(updated)
			continue
		}
		if outcome == "blocked" {
			return &runError{code: exitBlocked, err: fmt.Errorf("task %s recorded a blocker", next.ID)}
		}
		if outcome == "unsafe" {
			return &runError{code: exitAttempts, err: err}
		}
		if parentErr := ctx.Err(); parentErr != nil {
			return &runError{code: 130, err: fmt.Errorf("run interrupted: %w", parentErr)}
		}

		fallback := alternate(cfg.primary)
		fmt.Fprintf(os.Stderr, "Primary %s did not complete task %s (%v); trying %s once.\n", cfg.primary, next.ID, attemptFailure(result), fallback)
		recoveryMission := mission + fmt.Sprintf("\n\nRUNNER RECOVERY NOTE: The previous %s attempt failed before committing. Continue the existing uncommitted work for task %s only. Inspect it carefully, finish and verify that same task, commit it, then stop.\n", cfg.primary, next.ID)
		fallbackResult := invoke(ctx, cfg, fallback, recoveryMission, next.ID, iteration)
		usage.add(fallbackResult)
		usage.save(root)
		printUsage(usage, fallbackResult)
		printRemaining(usage, fallback)
		outcome, err = validateAttempt(cfg, *next, beforeHead, beforeBlocker)
		switch outcome {
		case "success":
			updated, _ := readTasks(filepath.Join(root, cfg.planPath))
			usage.CompletedTasks = completedTaskCount(updated)
			usage.save(root)
			printProgress(updated)
			continue
		case "blocked":
			return &runError{code: exitBlocked, err: fmt.Errorf("task %s recorded a blocker", next.ID)}
		default:
			code := exitAttempts
			if result.TimedOut && fallbackResult.TimedOut {
				code = exitTimeout
			}
			if err == nil {
				err = fmt.Errorf("task %s was not completed by either provider", next.ID)
			}
			return &runError{code: code, err: err}
		}
	}
	remaining, err := readTasks(filepath.Join(root, cfg.planPath))
	if err == nil && firstTODO(remaining) == nil {
		fmt.Printf("MILESTONE %s COMPLETE\n", cfg.milestone)
		return nil
	}
	return &runError{code: exitMax, err: fmt.Errorf("maximum of %d iterations reached", cfg.maxIterations)}
}

func preflight(ctx context.Context, cfg config) (string, []task, error) {
	if cfg.primary != "codex" && cfg.primary != "antigravity" && cfg.primary != "claude" {
		return "", nil, errors.New("--primary must be codex, antigravity, or claude")
	}
	if cfg.maxIterations < 1 || cfg.attemptTimeout <= 0 {
		return "", nil, errors.New("iteration count and attempt timeout must be positive")
	}
	mission, err := extractMission(filepath.Join(cfg.root, cfg.promptPath))
	if err != nil {
		return "", nil, err
	}
	tasks, err := readTasks(filepath.Join(cfg.root, cfg.planPath))
	if err != nil {
		return "", nil, err
	}
	if len(tasks) == 0 {
		return "", nil, fmt.Errorf("no Milestone %s tasks found in the plan", cfg.milestone)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", nil, err
	}
	// Validate only the providers this run can actually use: the primary and its
	// single fallback. This keeps a claude (or codex, or antigravity) run from
	// requiring all three CLIs to be installed.
	if err := checkProviderModel(ctx, cfg.primary, cfg); err != nil {
		return "", nil, err
	}
	if fb := alternate(cfg.primary); fb != cfg.primary {
		if err := checkProviderModel(ctx, fb, cfg); err != nil {
			return "", nil, err
		}
	}
	return mission, tasks, nil
}

func checkProviderModel(ctx context.Context, provider string, cfg config) error {
	switch provider {
	case "codex":
		return checkCodexModel(ctx, cfg.codexModel)
	case "antigravity":
		return checkAntigravityModel(ctx, cfg.antigravityModel)
	case "claude":
		return checkClaudeModel(ctx, cfg.claudeModel)
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
}

// checkClaudeModel is a lighter preflight than Codex/Antigravity: Claude Code has
// no clean model-catalog listing and resolves aliases (haiku/sonnet/opus) at
// runtime, so we only confirm the CLI is present and a model was named. An invalid
// model fails the attempt and the loop falls back like any other provider failure.
func checkClaudeModel(ctx context.Context, model string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return errors.New("claude CLI not found in PATH")
	}
	if strings.TrimSpace(model) == "" {
		return errors.New("claude model must not be empty")
	}
	return nil
}

func checkCodexModel(ctx context.Context, model string) error {
	path, err := exec.LookPath("codex")
	if err != nil {
		return errors.New("codex CLI not found in PATH")
	}
	out, err := exec.CommandContext(ctx, path, "debug", "models").Output()
	if err != nil {
		return fmt.Errorf("read Codex model catalog: %w", err)
	}
	var catalog struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &catalog); err != nil {
		return fmt.Errorf("decode Codex model catalog: %w", err)
	}
	for _, candidate := range catalog.Models {
		if candidate.Slug == model {
			return nil
		}
	}
	return fmt.Errorf("Codex model %q is not available in the current catalog", model)
}

func checkAntigravityModel(ctx context.Context, model string) error {
	path, err := exec.LookPath("agy")
	if err != nil {
		return errors.New("Antigravity CLI (agy) not found in PATH")
	}
	out, err := exec.CommandContext(ctx, path, "models").CombinedOutput()
	if err != nil {
		return fmt.Errorf("read Antigravity model catalog: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == model {
			return nil
		}
	}
	return fmt.Errorf("Antigravity model %q is not available in the current catalog", model)
}

func extractMission(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt: %w", err)
	}
	text := string(b)
	start := strings.Index(text, "```text\n")
	if start < 0 {
		return "", errors.New("prompt must contain one ```text fenced mission")
	}
	start += len("```text\n")
	end := strings.Index(text[start:], "\n```")
	if end < 0 {
		return "", errors.New("prompt mission fence is not closed")
	}
	mission := strings.TrimSpace(text[start : start+end])
	if mission == "" {
		return "", errors.New("prompt mission is empty")
	}
	return mission, nil
}

func readTasks(path string) ([]task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read milestone plan: %w", err)
	}
	defer f.Close()

	var tasks []task
	var currentMilestone string
	var block strings.Builder
	var current *task
	inTaskList := false
	flush := func() {
		if current == nil {
			return
		}
		current.Text = strings.TrimSpace(block.String())
		current.Done = current.Done || strings.Contains(current.Text, "— done:") || strings.Contains(current.Text, "- done:")
		tasks = append(tasks, *current)
		current = nil
		block.Reset()
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "## 6. Task list" {
			inTaskList = true
			continue
		}
		if inTaskList && strings.HasPrefix(line, "## 7.") {
			flush()
			break
		}
		if !inTaskList {
			continue
		}
		if match := milestoneRE.FindStringSubmatch(line); match != nil {
			flush()
			currentMilestone = match[1]
			continue
		}
		if currentMilestone == "" {
			continue
		}
		if match := itemRE.FindStringSubmatch(line); match != nil {
			flush()
			current = &task{ID: currentMilestone + "." + match[1], Done: strings.EqualFold(match[2], "x")}
			block.WriteString(line)
			block.WriteByte('\n')
			continue
		}
		if current != nil {
			block.WriteString(line)
			block.WriteByte('\n')
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func firstTODO(tasks []task) *task {
	for i := range tasks {
		if !tasks[i].Done {
			return &tasks[i]
		}
	}
	return nil
}

func nextTaskID(tasks []task) string {
	if next := firstTODO(tasks); next != nil {
		return next.ID
	}
	return "complete"
}

func completedTaskCount(tasks []task) int {
	done := 0
	for _, candidate := range tasks {
		if candidate.Done {
			done++
		}
	}
	return done
}

func progressLine(tasks []task, width int) string {
	if width < 1 {
		width = 1
	}
	total := len(tasks)
	done := completedTaskCount(tasks)
	filled := 0
	percent := 100.0
	if total > 0 {
		filled = done * width / total
		percent = float64(done) * 100 / float64(total)
	}
	next := nextTaskID(tasks)
	return fmt.Sprintf("Progress [%s%s] %d/%d (%.1f%%) next: %s", strings.Repeat("#", filled), strings.Repeat("-", width-filled), done, total, percent, next)
}

func printProgress(tasks []task) {
	fmt.Println(progressLine(tasks, 30))
}

func (u *usageSummary) add(result attemptResult) {
	u.Attempts++
	u.Updated = time.Now()
	provider := u.Providers[result.Provider]
	provider.Attempts++
	provider.Duration += result.Duration
	provider.TokenUsageAvailable = provider.TokenUsageAvailable || result.TokenUsageAvailable
	provider.InputTokens += result.InputTokens
	provider.CachedInputTokens += result.CachedInputTokens
	provider.OutputTokens += result.OutputTokens
	provider.ReasoningOutputTokens += result.ReasoningOutputTokens
	u.Providers[result.Provider] = provider
}

func (u usageSummary) save(root string) {
	u.Updated = time.Now()
	b, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, ".ralph"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".ralph", "usage.json"), append(b, '\n'), 0o644)
}

func printUsage(summary usageSummary, latest attemptResult) {
	tokens := "tokens unavailable"
	if latest.TokenUsageAvailable {
		tokens = fmt.Sprintf("tokens in=%d cached=%d out=%d reasoning=%d", latest.InputTokens, latest.CachedInputTokens, latest.OutputTokens, latest.ReasoningOutputTokens)
	}
	fmt.Printf("Usage %s: attempt %s, %s; run attempts=%d elapsed=%s\n", latest.Provider, latest.Duration.Round(time.Second), tokens, summary.Attempts, time.Since(summary.Started).Round(time.Second))
}

func printRemaining(summary usageSummary, provider string) {
	remainingTasks := summary.TotalTasks - summary.CompletedTasks
	remainingIterations := summary.MaxIterations - summary.Attempts
	if remainingIterations < 0 {
		remainingIterations = 0
	}
	quota := "provider quota unavailable"
	if providerUsage, ok := summary.Providers[provider]; ok && providerUsage.TokenUsageAvailable {
		quota = fmt.Sprintf("tokens consumed=%d", providerUsage.InputTokens+providerUsage.OutputTokens)
	}
	fmt.Printf("Remaining: tasks=%d, iteration budget=%d, %s\n", remainingTasks, remainingIterations, quota)
}

func invoke(parent context.Context, cfg config, provider, mission, taskID string, iteration int) attemptResult {
	started := time.Now()
	result := attemptResult{Provider: provider, TaskID: taskID, Started: started, ExitCode: -1}
	ctx, cancel := context.WithTimeout(parent, cfg.attemptTimeout)
	defer cancel()

	runDir := filepath.Join(cfg.root, ".ralph", "runs", started.UTC().Format("20060102T150405.000000000Z")+"-"+strconv.Itoa(iteration)+"-"+provider)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		result.Error = err.Error()
		return result
	}
	stdoutFile, err := os.Create(filepath.Join(runDir, "stdout.log"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(filepath.Join(runDir, "stderr.log"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer stderrFile.Close()

	cmd := providerCommand(ctx, cfg, provider, mission)
	cmd.Dir = cfg.root
	var codexOutput *codexStream
	if provider == "codex" {
		codexOutput = &codexStream{log: stdoutFile, display: os.Stdout}
		cmd.Stdout = codexOutput
	} else {
		cmd.Stdout = io.MultiWriter(os.Stdout, stdoutFile)
	}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	err = cmd.Run()
	if codexOutput != nil {
		codexOutput.flush()
		result.TokenUsageAvailable = codexOutput.foundUsage
		result.InputTokens = codexOutput.usage.InputTokens
		result.CachedInputTokens = codexOutput.usage.CachedInputTokens
		result.OutputTokens = codexOutput.usage.OutputTokens
		result.ReasoningOutputTokens = codexOutput.usage.ReasoningOutputTokens
	}
	result.Duration = time.Since(started)
	if err == nil {
		result.ExitCode = 0
	} else {
		result.Error = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
	}
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	metadata, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(filepath.Join(runDir, "metadata.json"), append(metadata, '\n'), 0o644)
	return result
}

func providerCommand(ctx context.Context, cfg config, provider, mission string) *exec.Cmd {
	switch provider {
	case "codex":
		cmd := exec.CommandContext(ctx, "codex", "exec", "--json", "--model", cfg.codexModel, "--dangerously-bypass-approvals-and-sandbox", "--cd", cfg.root, "-")
		cmd.Stdin = strings.NewReader(mission)
		return cmd
	case "claude":
		// Claude Code non-interactive print mode with full autonomy for the loop
		// (trusted checkout). The mission goes on stdin so a long prompt never hits
		// argv limits; cmd.Dir (set by invoke) scopes it to the repo root.
		cmd := exec.CommandContext(ctx, "claude", "--print", "--model", cfg.claudeModel, "--dangerously-skip-permissions")
		cmd.Stdin = strings.NewReader(mission)
		return cmd
	default:
		return exec.CommandContext(ctx, "agy", "--model", cfg.antigravityModel, "--mode", "accept-edits", "--dangerously-skip-permissions", "--print-timeout", cfg.attemptTimeout.String(), "--print", mission)
	}
}

func (s *codexStream) Write(p []byte) (int, error) {
	if _, err := s.log.Write(p); err != nil {
		return 0, err
	}
	s.pending = append(s.pending, p...)
	for {
		newline := bytes.IndexByte(s.pending, '\n')
		if newline < 0 {
			break
		}
		s.consume(s.pending[:newline])
		s.pending = s.pending[newline+1:]
	}
	return len(p), nil
}

func (s *codexStream) flush() {
	if len(s.pending) > 0 {
		s.consume(s.pending)
		s.pending = nil
	}
}

func (s *codexStream) consume(line []byte) {
	var event struct {
		Type  string `json:"type"`
		Usage *struct {
			InputTokens           int64 `json:"input_tokens"`
			CachedInputTokens     int64 `json:"cached_input_tokens"`
			OutputTokens          int64 `json:"output_tokens"`
			ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
		} `json:"usage"`
		Item *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	if event.Type == "item.completed" && event.Item != nil && event.Item.Type == "agent_message" && event.Item.Text != "" {
		fmt.Fprintln(s.display, event.Item.Text)
	}
	if event.Type == "turn.completed" && event.Usage != nil {
		s.foundUsage = true
		s.usage.InputTokens += event.Usage.InputTokens
		s.usage.CachedInputTokens += event.Usage.CachedInputTokens
		s.usage.OutputTokens += event.Usage.OutputTokens
		s.usage.ReasoningOutputTokens += event.Usage.ReasoningOutputTokens
	}
}

func validateAttempt(cfg config, selected task, beforeHead string, beforeBlocker [32]byte) (string, error) {
	afterBlocker, err := fileDigest(filepath.Join(cfg.root, "docs/milestones/BLOCKERS.md"))
	if err != nil {
		return "unsafe", err
	}
	if afterBlocker != beforeBlocker {
		return "blocked", nil
	}
	afterHead, err := gitOutput(cfg.root, "rev-parse", "HEAD")
	if err != nil {
		return "unsafe", err
	}
	tasks, err := readTasks(filepath.Join(cfg.root, cfg.planPath))
	if err != nil {
		return "unsafe", err
	}
	done := false
	for _, candidate := range tasks {
		if candidate.ID == selected.ID {
			done = candidate.Done
			break
		}
	}
	if afterHead != beforeHead {
		countText, err := gitOutput(cfg.root, "rev-list", "--count", beforeHead+".."+afterHead)
		if err != nil {
			return "unsafe", err
		}
		count, err := strconv.Atoi(countText)
		if err != nil || count != 1 || !done {
			return "unsafe", fmt.Errorf("task %s changed Git history ambiguously (commits=%s, done=%t)", selected.ID, countText, done)
		}
		if err := requireClean(cfg.root); err != nil {
			return "unsafe", fmt.Errorf("task %s committed but left a dirty worktree: %w", selected.ID, err)
		}
		return "success", nil
	}
	if done {
		return "unsafe", fmt.Errorf("task %s was marked done without a commit", selected.ID)
	}
	return "retry", nil
}

func repositoryRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("current directory is not inside a Git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

func requireClean(root string) error {
	out, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if out != "" {
		return fmt.Errorf("worktree must be clean before Ralph starts; found:\n%s", out)
	}
	return nil
}

func ensureBranch(root, branch string) error {
	current, err := gitOutput(root, "branch", "--show-current")
	if err != nil {
		return err
	}
	if current == branch {
		return nil
	}
	if err := exec.Command("git", "check-ref-format", "--branch", branch).Run(); err != nil {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	check.Dir = root
	if err := check.Run(); err == nil {
		_, err = gitOutput(root, "switch", branch)
		return err
	}
	_, err = gitOutput(root, "switch", "-c", branch)
	return err
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func fileDigest(path string) ([32]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return sha256.Sum256(nil), nil
	}
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// alternate returns the single fallback provider tried once when the primary
// fails before committing. codex<->antigravity remain paired; claude falls back
// to codex (the most capable generally-available provider).
func alternate(provider string) string {
	switch provider {
	case "codex":
		return "antigravity"
	case "antigravity":
		return "codex"
	case "claude":
		return "codex"
	default:
		return "codex"
	}
}

func attemptFailure(result attemptResult) error {
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return errors.New("provider exited without recording task progress")
}

func preflightError(err error) error { return &runError{code: exitPreflight, err: err} }
