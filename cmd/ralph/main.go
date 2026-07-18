package main

import (
	"bufio"
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
	maxIterations    int
	attemptTimeout   time.Duration
	codexModel       string
	antigravityModel string
	unsafe           bool
	dryRun           bool
}

type task struct {
	ID   string
	Done bool
	Text string
}

type attemptResult struct {
	Provider string        `json:"provider"`
	TaskID   string        `json:"task_id"`
	Started  time.Time     `json:"started_at"`
	Duration time.Duration `json:"duration"`
	ExitCode int           `json:"exit_code"`
	TimedOut bool          `json:"timed_out"`
	Error    string        `json:"error,omitempty"`
}

type runError struct {
	code int
	err  error
}

func (e *runError) Error() string { return e.err.Error() }

var (
	milestoneRE = regexp.MustCompile(`^### Milestone (11\.[0-9]+)\b`)
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
	flag.StringVar(&cfg.primary, "primary", "", "primary provider: codex or antigravity (required)")
	flag.StringVar(&cfg.promptPath, "prompt", "prompt.md", "path to the Ralph mission prompt")
	flag.StringVar(&cfg.planPath, "plan", "docs/milestones/v1-milestone-6-plan.md", "path to the milestone plan")
	flag.StringVar(&cfg.branch, "branch", "phase6", "implementation branch")
	flag.IntVar(&cfg.maxIterations, "max-iterations", 100, "maximum successful task iterations")
	flag.DurationVar(&cfg.attemptTimeout, "attempt-timeout", 2*time.Hour, "timeout for each provider attempt")
	flag.StringVar(&cfg.codexModel, "codex-model", "gpt-5.6-luna", "Codex model identifier")
	flag.StringVar(&cfg.antigravityModel, "antigravity-model", "Gemini 3.5 Flash (Medium)", "Antigravity model label")
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

	for iteration := 1; iteration <= cfg.maxIterations; iteration++ {
		current, err := readTasks(filepath.Join(root, cfg.planPath))
		if err != nil {
			return preflightError(err)
		}
		next := firstTODO(current)
		if next == nil {
			fmt.Println("MILESTONE 6 COMPLETE")
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
		outcome, err := validateAttempt(cfg, *next, beforeHead, beforeBlocker)
		if outcome == "success" {
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
		outcome, err = validateAttempt(cfg, *next, beforeHead, beforeBlocker)
		switch outcome {
		case "success":
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
		fmt.Println("MILESTONE 6 COMPLETE")
		return nil
	}
	return &runError{code: exitMax, err: fmt.Errorf("maximum of %d iterations reached", cfg.maxIterations)}
}

func preflight(ctx context.Context, cfg config) (string, []task, error) {
	if cfg.primary != "codex" && cfg.primary != "antigravity" {
		return "", nil, errors.New("--primary must be codex or antigravity")
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
		return "", nil, errors.New("no Milestone 6 tasks found in the plan")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", nil, err
	}
	if err := checkCodexModel(ctx, cfg.codexModel); err != nil {
		return "", nil, err
	}
	if err := checkAntigravityModel(ctx, cfg.antigravityModel); err != nil {
		return "", nil, err
	}
	return mission, tasks, nil
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
	cmd.Stdout = io.MultiWriter(os.Stdout, stdoutFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	err = cmd.Run()
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
	if provider == "codex" {
		cmd := exec.CommandContext(ctx, "codex", "exec", "--model", cfg.codexModel, "--dangerously-bypass-approvals-and-sandbox", "--cd", cfg.root, "-")
		cmd.Stdin = strings.NewReader(mission)
		return cmd
	}
	return exec.CommandContext(ctx, "agy", "--model", cfg.antigravityModel, "--mode", "accept-edits", "--dangerously-skip-permissions", "--print-timeout", cfg.attemptTimeout.String(), "--print", mission)
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

func alternate(provider string) string {
	if provider == "codex" {
		return "antigravity"
	}
	return "codex"
}

func attemptFailure(result attemptResult) error {
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return errors.New("provider exited without recording task progress")
}

func preflightError(err error) error { return &runError{code: exitPreflight, err: err} }
