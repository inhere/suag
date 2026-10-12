package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// repeatLines renders format (with the index as its only verb) n times.
func repeatLines(n int, format string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, format+"\n", i)
	}
	return b.String()
}

func TestOutputRulesBuiltin(t *testing.T) {
	cases := []struct {
		name, command, output string
		want, notWant         []string
	}{
		{
			name:    "go test without -v: packages only",
			command: "go test ./...",
			output:  repeatLines(60, "?   \tgithub.com/x/cmd%d\t[no test files]") + repeatLines(10, "ok  \tgithub.com/x/pkg%d\t0.1s") + "[exit code: 0]",
			want:    []string{"ok  \tgithub.com/x/pkg9\t0.1s\n[exit code: 0]\ngo test: 10 package(s) ok"},
			notWant: []string{"no test files", "test(s) passed"},
		},
		{
			name:    "cargo test passed",
			command: "cargo test",
			output: repeatLines(5, "   Compiling dep%d v0.1.0") + "    Finished test target(s) in 1.2s\n\nrunning 30 tests\n" +
				repeatLines(30, "test tests::case_%d ... ok") + "\ntest result: ok. 30 passed; 0 failed; 0 ignored\n[exit code: 0]",
			want:    []string{"test result: ok. 30 passed", "cargo test: 30 test(s) passed"},
			notWant: []string{"case_0 ... ok", "Compiling"},
		},
		{
			name:    "cargo test failed",
			command: "cd crate && cargo test --lib",
			output: "running 30 tests\n" + repeatLines(29, "test tests::case_%d ... ok") + "test tests::bad ... FAILED\n\nfailures:\n\n" +
				"---- tests::bad stdout ----\nthread 'tests::bad' panicked at src/lib.rs:10:5:\nassertion `left == right` failed\n  left: 1\n right: 2\n\n" +
				"test result: FAILED. 29 passed; 1 failed\n[exit code: 101]",
			want:    []string{"test tests::bad ... FAILED", "panicked at src/lib.rs:10:5", "  left: 1", "test result: FAILED"},
			notWant: []string{"case_0 ... ok", "test(s) passed"},
		},
		{
			name:    "jest passed",
			command: "npx jest",
			output:  " PASS  src/a.test.js\n  sum\n" + repeatLines(30, "    ✓ adds case %d (3 ms)") + "\nTest Suites: 1 passed, 1 total\nTests:       30 passed, 30 total\n[exit code: 0]",
			want:    []string{"Tests:       30 passed, 30 total", "tests: 30 passed"},
			notWant: []string{"✓", "PASS  src/a.test.js"},
		},
		{
			name:    "npm test with a failure",
			command: "npm test",
			output: " FAIL  src/b.test.js\n" + repeatLines(29, "    ✓ case %d (1 ms)") + "    ✕ adds wrong (2 ms)\n\n  ● sum › adds wrong\n\n" +
				"    expect(received).toBe(expected)\n    Expected: 3\n    Received: 4\n\nTests:       1 failed, 29 passed, 30 total\n[exit code: 1]",
			want:    []string{"FAIL  src/b.test.js", "✕ adds wrong", "● sum › adds wrong", "Expected: 3", "Received: 4"},
			notWant: []string{"✓ case 0", "tests: "},
		},
		{
			name:    "vitest passed",
			command: "pnpm vitest run",
			output:  repeatLines(30, " ✓ src/m%d.test.ts (5 tests) 3ms") + "\n Test Files  30 passed (30)\n      Tests  150 passed (150)\n[exit code: 0]",
			want:    []string{"Tests  150 passed (150)", "tests: 30 passed"},
			notWant: []string{"src/m0.test.ts"},
		},
		{
			name:    "pytest -v passed",
			command: "python -m pytest -v tests",
			output: "============================= test session starts ==============================\ncollected 30 items\n\n" +
				repeatLines(30, "tests/test_a.py::test_%d PASSED                                          [ 50%%]") +
				"\n============================== 30 passed in 0.12s ==============================\n[exit code: 0]",
			want:    []string{"30 passed in 0.12s", "pytest: 30 test(s) passed"},
			notWant: []string{"::test_0 PASSED"},
		},
		{
			name:    "pytest failed",
			command: "pytest tests",
			output: repeatLines(29, "tests/test_a.py::test_%d PASSED                                          [ 50%%]") +
				"tests/test_a.py::test_bad FAILED                                       [100%]\n" +
				"=================================== FAILURES ===================================\n" +
				"___________________________________ test_bad ___________________________________\n\n" +
				"    def test_bad():\n>       assert 1 == 2\nE       assert 1 == 2\n\ntests/test_a.py:5: AssertionError\n" +
				"FAILED tests/test_a.py::test_bad - assert 1 == 2\n========================= 1 failed, 29 passed in 0.12s =========================\n[exit code: 1]",
			want:    []string{"::test_bad FAILED", "E       assert 1 == 2", "tests/test_a.py:5: AssertionError", "1 failed, 29 passed"},
			notWant: []string{"::test_0 PASSED", "test(s) passed"},
		},
		{
			name:    "maven passed",
			command: "./mvnw -B test",
			output: "[INFO] Scanning for projects...\n" + repeatLines(30, "[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/x/lib%d.pom") +
				repeatLines(20, "[INFO] Running com.x.Test%d") +
				repeatLines(20, "[INFO] Tests run: 3, Failures: 0, Errors: 0, Skipped: 0, Time elapsed: 0.05 s - in com.x.Test%d") +
				"[INFO] Results:\n[INFO] Tests run: 60, Failures: 0, Errors: 0, Skipped: 0\n[INFO] BUILD SUCCESS\n[exit code: 0]",
			want:    []string{"[INFO] Tests run: 60, Failures: 0, Errors: 0, Skipped: 0\n", "BUILD SUCCESS"},
			notWant: []string{"Downloading from", "Running com.x", "Time elapsed"},
		},
		{
			name:    "maven failed",
			command: "mvn test",
			output: repeatLines(30, "[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/x/lib%d.pom") +
				"[ERROR] Tests run: 3, Failures: 1, Errors: 0, Skipped: 0, Time elapsed: 0.05 s <<< FAILURE! - in com.x.BTest\n" +
				"[ERROR] testAdd(com.x.BTest)  Time elapsed: 0.01 s  <<< FAILURE!\njava.lang.AssertionError: expected:<1> but was:<2>\n" +
				"[INFO] BUILD FAILURE\n[exit code: 1]",
			want:    []string{"<<< FAILURE! - in com.x.BTest", "java.lang.AssertionError: expected:<1> but was:<2>", "BUILD FAILURE"},
			notWant: []string{"Downloading from"},
		},
		{
			name:    "gradle passed",
			command: "./gradlew test",
			output: repeatLines(10, "> Task :sub%d:compileJava UP-TO-DATE") + "> Task :test\n\n" +
				repeatLines(30, "ATest > case%d() PASSED") + "\nBUILD SUCCESSFUL in 2s\n[exit code: 0]",
			want:    []string{"> Task :test", "BUILD SUCCESSFUL", "gradle: 30 test(s) passed"},
			notWant: []string{"UP-TO-DATE", "case0() PASSED"},
		},
		{
			name:    "rules only act on matching commands",
			command: "cat build.log",
			output:  repeatLines(40, "--- PASS: TestCase%d (0.00s)") + "[exit code: 0]",
			// nothing tidies these lines, so the result is left alone (below)
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tidy, ok := MinimizeToolOutput(shellCall(tc.command), tc.output)
			if len(tc.want) == 0 {
				assert.False(t, ok)
				return
			}
			assert.Require(t, assert.True(t, ok))
			for _, w := range tc.want {
				assert.StrContains(t, tidy, w)
			}
			for _, w := range tc.notWant {
				assert.NotContains(t, tidy, w)
			}
		})
	}
}

func TestLoadOutputRules(t *testing.T) {
	names := func(rules []OutputRule) string {
		var out []string
		for _, r := range rules {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}
	t.Run("built-in only", func(t *testing.T) {
		rules, errs := LoadOutputRules(filepath.Join(t.TempDir(), "missing.yaml"))
		assert.Empty(t, errs)
		assert.Eq(t, "go-test,cargo-test,js-test,pytest,maven,gradle", names(rules))
	})
	t.Run("user then project: override, add, switch off", func(t *testing.T) {
		dir := t.TempDir()
		user, project := filepath.Join(dir, "user.yaml"), filepath.Join(dir, "project.yaml")
		assert.NoErr(t, os.WriteFile(user, []byte(`
rules:
  - name: make-check
    match: '\bmake check\b'
    drop: ['^CHECK OK ']
    count: {passed: '^CHECK OK '}
    summary: 'make check: {passed} ok'
  - name: maven
    enabled: false
`), 0o644))
		assert.NoErr(t, os.WriteFile(project, []byte(`
rules:
  - name: make-check
    match: '\bmake check\b'
    drop: ['^CHECK OK ']
    count: {passed: '^CHECK OK '}
    summary: 'project check: {passed} ok'
`), 0o644))
		rules, errs := LoadOutputRules(user, project)
		assert.Empty(t, errs)
		assert.Eq(t, "go-test,cargo-test,js-test,pytest,gradle,make-check", names(rules))

		m := NewOutputMinimizer(rules)
		tidy, ok := m.Minimize(shellCall("make check"), repeatLines(40, "CHECK OK item %d")+"[exit code: 0]")
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "[exit code: 0]\nproject check: 40 ok", tidy) // the project rule won
		_, ok = m.Minimize(shellCall("mvn test"), repeatLines(40, "[INFO] Running com.x.Test%d")+"[exit code: 0]")
		assert.False(t, ok) // maven switched off
	})
	t.Run("a broken file is reported and skipped", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "bad.yaml")
		assert.NoErr(t, os.WriteFile(bad, []byte("rules:\n  - name: x\n    match: '(unclosed'\n"), 0o644))
		rules, errs := LoadOutputRules(bad)
		assert.Require(t, assert.Len(t, errs, 1))
		assert.StrContains(t, errs[0].Error(), "bad.yaml: rule x:")
		assert.Eq(t, "go-test,cargo-test,js-test,pytest,maven,gradle", names(rules))

		assert.NoErr(t, os.WriteFile(bad, []byte("rules:\n  - match: 'x'\n"), 0o644))
		_, errs = LoadOutputRules(bad)
		assert.Require(t, assert.Len(t, errs, 1))
		assert.StrContains(t, errs[0].Error(), "name is required")
	})
}

func TestRenderSummary(t *testing.T) {
	names := []string{"packages", "passed"}
	summary := "go test: {packages} package(s) ok[, {passed} test(s) passed]"
	assert.Eq(t, "go test: 2 package(s) ok, 5 test(s) passed", renderSummary(summary, names, map[string]int{"packages": 2, "passed": 5}))
	assert.Eq(t, "go test: 2 package(s) ok", renderSummary(summary, names, map[string]int{"packages": 2}))
	assert.Eq(t, "", renderSummary(summary, names, map[string]int{}))
}
