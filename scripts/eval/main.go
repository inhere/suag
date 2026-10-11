// Command eval runs the suag evaluation tasks in scripts/eval/testdata/tasks
// in isolated temporary workspaces and SUAG_HOMEs. See README.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/goccy/go-yaml"
)

type options struct {
	bin, task, model, configFrom, out string
	agent, ompBin                     string
	fake                              bool
	timeout                           time.Duration
	tasksDir                          string
	probe                             bool
	probeRepeat                       int
	probeGap                          time.Duration
}

// Result is one results.jsonl line.
type Result struct {
	Task        string  `json:"task"`
	Agent       string  `json:"agent"`
	Passed      bool    `json:"passed"`
	Error       string  `json:"error,omitempty"`
	SessionID   string  `json:"session_id"`
	Mode        string  `json:"mode"`
	Model       string  `json:"model"`
	Fake        bool    `json:"fake"`
	DurationMs  int64   `json:"duration_ms"`
	Calls       int     `json:"calls"`
	InputTokens int64   `json:"input_tokens"`
	OutputToks  int64   `json:"output_tokens"`
	CacheRead   int64   `json:"cache_read_tokens"`
	CacheWrite  int64   `json:"cache_write_tokens"`
	CacheHit    float64 `json:"cache_hit_rate"`
	// FirstUncached is the uncached input of the first main-loop model call
	// (suag: input - cache_read - cache_write from the trace; omp: usage.input).
	// nil when unavailable (no usable model event).
	FirstUncached *int64        `json:"first_call_uncached"`
	CostUSD       float64       `json:"cost_usd"`
	ToolCalls     int           `json:"tool_calls"`
	RawBytes      *int64        `json:"tool_raw_bytes"`
	InlineBytes   *int64        `json:"tool_inline_bytes"`
	Spilled       int           `json:"tool_spilled"`
	Answer        string        `json:"answer"`
	Checks        []CheckResult `json:"checks"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run() error {
	var o options
	flag.StringVar(&o.bin, "bin", filepath.Join("tmp", "smoke", "suag"), "suag executable (.exe is added on Windows)")
	flag.StringVar(&o.task, "task", "*", "glob of task names")
	flag.StringVar(&o.model, "model", "", "model passed to `suag run --model`")
	flag.BoolVar(&o.fake, "fake", false, "use scripts/ptysmoke/fakellm.py (pipeline check only)")
	flag.StringVar(&o.configFrom, "config-from", "", "directory whose config.yaml and .env are copied (read-only) into the temp SUAG_HOME")
	flag.StringVar(&o.out, "out", "", "output directory (default tmp/eval/<run-id>)")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Minute, "per-task timeout (a task's own timeout field overrides it)")
	flag.StringVar(&o.agent, "agent", "suag", "agent under test: suag or omp (oh-my-pi, same model via --model)")
	flag.StringVar(&o.ompBin, "omp-bin", "omp", "omp executable for --agent omp")
	flag.StringVar(&o.tasksDir, "tasks-dir", filepath.Join("scripts", "eval", "testdata", "tasks"), "tasks directory")
	flag.BoolVar(&o.probe, "probe-prefix", false, "run the prefix-cache probe (exclusive with tasks; suag only)")
	flag.IntVar(&o.probeRepeat, "probe-repeat", 3, "probe: repetitions per group")
	flag.DurationVar(&o.probeGap, "probe-gap", 5*time.Second, "probe: pause between two requests")
	flag.Parse()
	taskSet := false
	flag.Visit(func(f *flag.Flag) { taskSet = taskSet || f.Name == "task" || f.Name == "tasks-dir" })
	if o.probe && (taskSet || o.agent != "suag") {
		return errors.New("--probe-prefix is exclusive with --task/--tasks-dir and needs --agent suag")
	}
	if o.probe && o.probeRepeat < 1 {
		return errors.New("--probe-repeat must be >= 1")
	}

	if runtime.GOOS == "windows" && !strings.HasSuffix(o.bin, ".exe") {
		o.bin += ".exe"
	}
	bin, err := filepath.Abs(o.bin)
	if err != nil {
		return err
	}
	switch o.agent {
	case "suag":
		if _, err := os.Stat(bin); err != nil {
			return fmt.Errorf("suag binary: %w", err)
		}
		if !o.fake && o.configFrom == "" {
			return errors.New("need --fake or --config-from <dir>")
		}
	case "omp":
		if o.fake {
			return errors.New("--fake works only with --agent suag")
		}
	default:
		return fmt.Errorf("unknown --agent %q (suag|omp)", o.agent)
	}
	runID := time.Now().Format("20060102-150405")
	if o.out == "" {
		o.out = filepath.Join("tmp", "eval", runID)
	}
	if o.out, err = filepath.Abs(o.out); err != nil {
		return err
	}
	if o.probe {
		return runProbe(&o, bin, runID)
	}
	tasks, err := loadTasks(o.tasksDir, o.task)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no task matches %q", o.task)
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	jf, err := os.Create(filepath.Join(o.out, "results.jsonl"))
	if err != nil {
		return err
	}
	defer jf.Close()
	enc := json.NewEncoder(jf)

	var results []Result
	for _, t := range tasks {
		r, err := runTask(&o, bin, runID, t)
		if err != nil { // runner-side failure of this task: record and go on
			r.Error = err.Error()
		}
		results = append(results, r)
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	printTable(os.Stdout, results)
	fmt.Printf("\nresults: %s\n", filepath.Join(o.out, "results.jsonl"))
	return nil
}

func runTask(o *options, bin, runID string, t *Task) (Result, error) {
	sid := "eval-" + runID + "-" + t.Name
	r := Result{Task: t.Name, Agent: o.agent, SessionID: sid, Mode: t.Mode, Model: o.model, Fake: o.fake}
	root := filepath.Join(o.out, t.Name)
	ws, home := filepath.Join(root, "ws"), filepath.Join(root, "home")
	if err := copyDir(filepath.Join(t.dir, "fixture"), ws); err != nil {
		return r, fmt.Errorf("copy fixture: %w", err)
	}
	for _, g := range t.Generate {
		if err := g.write(ws); err != nil {
			return r, fmt.Errorf("generate: %w", err)
		}
	}
	if out, err := exec.Command("git", "-C", ws, "init", "-q").CombinedOutput(); err != nil {
		return r, fmt.Errorf("git init: %v: %s", err, out)
	}
	timeout := t.effectiveTimeout(o.timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if o.agent == "omp" {
		r.SessionID = ""
		runOmp(ctx, o, t, timeout, ws, filepath.Join(root, "omp-sessions"), &r)
		return r, nil
	}
	model, stop, err := setupSuagHome(ctx, o, home, t.FakeMode)
	if err != nil {
		return r, err
	}
	defer stop()

	env := isolatedEnv(home)
	// A multi-turn task sends its prompts in order to the same session; the
	// run stops at the first failing turn and checks judge the last answer.
	start := time.Now()
	var stdout []byte
	var runErr error
	for _, prompt := range t.Turns() {
		stdout, runErr = runCmd(ctx, bin, suagRunArgs(sid, prompt, model, t.Mode == "allow-write"), ws, env)
		if runErr != nil {
			break
		}
	}
	r.DurationMs = time.Since(start).Milliseconds()
	if runErr != nil {
		r.Error = runErr.Error()
	}
	r.Answer = finalAnswer(stdout)
	t.finish(ws, r.Answer, &r)
	r.Passed = runErr == nil && allPassed(r.Checks)

	sout, err := runCmd(context.Background(), bin, []string{"stats", "--session", sid, "--json"}, ws, env)
	if err != nil {
		if r.Error == "" {
			r.Error = "stats: " + err.Error()
		}
	} else {
		parseStats(sout, &r)
	}
	if tout, err := runCmd(context.Background(), bin, []string{"trace", sid, "--json"}, ws, env); err == nil {
		if c, ok := firstMainCall(tout); ok {
			v := c.uncached()
			r.FirstUncached = &v
		}
	}
	return r, nil
}

// suagRunArgs builds the `suag run` arguments of one turn.
func suagRunArgs(sid, prompt, model string, allowWrite bool) []string {
	args := []string{"run", "--session-id", sid, "--output-format", "stream-json", "--prompt", prompt}
	if model != "" {
		args = append(args, "--model", model)
	}
	if allowWrite {
		args = append(args, "--allow-write")
	}
	return args
}

// setupSuagHome creates home with an isolated config (fake provider with a
// fresh fakellm, or a copy of --config-from) and returns the model to pass
// (empty = config default) and a stop func for the fake server.
func setupSuagHome(ctx context.Context, o *options, home, fakeMode string) (string, func(), error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", nil, err
	}
	if !o.fake {
		return o.model, func() {}, writeConfig(home, o.configFrom, 0)
	}
	stop, port, err := startFake(ctx, fakeMode)
	if err != nil {
		return "", nil, err
	}
	if err := writeConfig(home, "", port); err != nil {
		stop()
		return "", nil, err
	}
	model := o.model
	if model == "" {
		model = "fake/m1"
	}
	return model, stop, nil
}

// runCmd runs bin in dir with env and returns its stdout. A non-zero exit
// includes the tail of stderr in the error.
func runCmd(ctx context.Context, bin string, args []string, dir string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.WaitDelay = 5 * time.Second
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if err != nil {
		tail := strings.TrimSpace(se.String())
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return so.Bytes(), fmt.Errorf("%s %s: %v: %s", filepath.Base(bin), args[0], err, tail)
	}
	return so.Bytes(), nil
}

func isolatedEnv(home string) []string {
	drop := map[string]bool{"SUAG_HOME": true, "SUAG_MODEL": true, "HOME": true, "USERPROFILE": true}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[strings.ToUpper(k)] {
			env = append(env, kv)
		}
	}
	env = append(env, "SUAG_HOME="+home, "HOME="+home)
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+home)
	}
	return env
}

// finalAnswer returns the "result" field of the last stream-json result line.
func finalAnswer(stdout []byte) string {
	var answer string
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var line struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Type == "result" {
			answer = line.Result
		}
	}
	return answer
}

func parseStats(raw []byte, r *Result) {
	var rep struct {
		Models []struct {
			Calls      int     `json:"calls"`
			In         int64   `json:"input_tokens"`
			Out        int64   `json:"output_tokens"`
			CacheRead  int64   `json:"cache_read_tokens"`
			CacheWrite int64   `json:"cache_write_tokens"`
			Cost       float64 `json:"cost_usd"`
		} `json:"models"`
		Tools []struct {
			Calls       int    `json:"calls"`
			RawBytes    *int64 `json:"raw_bytes"`
			InlineBytes *int64 `json:"inline_bytes"`
			Spilled     int    `json:"spilled"`
		} `json:"tools"`
	}
	if json.Unmarshal(bytes.TrimSpace(raw), &rep) != nil {
		return
	}
	for _, m := range rep.Models {
		r.Calls += m.Calls
		r.InputTokens += m.In
		r.OutputToks += m.Out
		r.CacheRead += m.CacheRead
		r.CacheWrite += m.CacheWrite
		r.CostUSD += m.Cost
	}
	if total := r.InputTokens + r.CacheRead + r.CacheWrite; total > 0 {
		r.CacheHit = float64(r.CacheRead) / float64(total)
	}
	for _, t := range rep.Tools {
		r.ToolCalls += t.Calls
		r.RawBytes = addPtr(r.RawBytes, t.RawBytes)
		r.InlineBytes = addPtr(r.InlineBytes, t.InlineBytes)
		r.Spilled += t.Spilled
	}
}

func addPtr(a, b *int64) *int64 {
	if b == nil {
		return a
	}
	v := *b
	if a != nil {
		v += *a
	}
	return &v
}

// writeConfig writes <home>/config.yaml (copying fromDir's config.yaml and
// .env when fromDir is set, else a fake-provider config) and forces the files
// memory backends.
func writeConfig(home, fromDir string, fakePort int) error {
	cfg := map[string]any{}
	if fromDir != "" {
		raw, err := os.ReadFile(filepath.Join(fromDir, "config.yaml"))
		if err != nil {
			return fmt.Errorf("--config-from: %w", err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("--config-from config.yaml: %w", err)
		}
		if env, err := os.ReadFile(filepath.Join(fromDir, ".env")); err == nil {
			if err := os.WriteFile(filepath.Join(home, ".env"), env, 0o600); err != nil {
				return err
			}
		}
	} else {
		cfg["model"] = "fake/m1"
		cfg["providers"] = map[string]any{"fake": map[string]any{
			"type": "openai", "base_url": fmt.Sprintf("http://127.0.0.1:%d/v1", fakePort), "api_key": "k"}}
	}
	mem, _ := cfg["memory"].(map[string]any)
	if mem == nil {
		mem = map[string]any{}
	}
	for _, scope := range []string{"user", "project"} {
		sm, _ := mem[scope].(map[string]any)
		if sm == nil {
			sm = map[string]any{}
		}
		sm["backend"] = "files"
		mem[scope] = sm
	}
	cfg["memory"] = mem
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "config.yaml"), out, 0o600)
}

// startFake launches fakellm.py on a free port and returns a stop func.
func startFake(ctx context.Context, mode string) (func(), int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	script := filepath.Join("scripts", "ptysmoke", "fakellm.py")
	var lastErr error
	for _, py := range []string{"python3", "python"} {
		cmd := exec.CommandContext(ctx, py, "-I", script, "--port", fmt.Sprint(port), "--mode", mode, "--delay", "0.05")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, 0, err
		}
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		if !strings.Contains(line, "listening") {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			lastErr = fmt.Errorf("%s: fake model did not start: %q", py, line)
			continue
		}
		go io.Copy(io.Discard, stdout) //nolint:errcheck // keep the pipe drained
		return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }, port, nil
	}
	return nil, 0, lastErr
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func printTable(w io.Writer, rs []Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// PROMPT is every prompt token sent (uncached + cache read/write), the
	// fair basis across agents; TOKENS(in/out) keeps uncached input / output.
	// FIRST is the uncached input of the first main-loop call ("-" if unknown).
	fmt.Fprintln(tw, "TASK\tAGENT\tRESULT\tCALLS\tTOOLS\tSPILL\tFIRST\tPROMPT\tTOKENS(in/out)\tHIT%\tCOST\tTIME")
	for _, r := range rs {
		status := "FAIL"
		if r.Passed {
			status = "PASS"
		}
		prompt := r.InputTokens + r.CacheRead + r.CacheWrite
		first := "-"
		if r.FirstUncached != nil {
			first = fmt.Sprint(*r.FirstUncached)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%d\t%d/%d\t%.1f\t$%.4f\t%.1fs\n", r.Task, r.Agent, status, r.Calls, r.ToolCalls, r.Spilled,
			first, prompt, r.InputTokens, r.OutputToks, r.CacheHit*100, r.CostUSD, float64(r.DurationMs)/1000)
	}
	_ = tw.Flush()
}
