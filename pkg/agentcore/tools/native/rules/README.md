# Built-in shell output rules

These files are the rules of the output minimizer (`tools.minimize_output`): a
rule applies when its `match` regex matches the shell command; it drops the
lines that only say something passed and keeps everything that may matter. The
original output is always kept under a ref.

## Organisation

One file per language or tool family, all embedded into the binary
(`pkg/agentcore/tools/native/output_rules.go`), which reads them in the fixed
order listed there:

| file | rules |
| --- | --- |
| `go.yaml` | `go-test` |
| `rust.yaml` | `cargo-test` |
| `js.yaml` | `js-test` (jest, vitest, mocha, the package managers' test scripts) |
| `python.yaml` | `pytest` |
| `java.yaml` | `maven`, `gradle` |

The order matters: every rule whose `match` matches the command is applied in
turn, so it is kept as it was when all rules lived in one `output_rules.yaml`.
A new rule goes into the file of its language; a language that needs none gets
no file. Add a file to the order list in `output_rules.go` (a test fails
otherwise).

## Fields

```yaml
rules:
  - name: go-test          # the rule's name (unique; how a user overrides it)
    enabled: true          # false turns this rule (of that name) off
    match: '\bgo test\b'   # regex on the command
    drop: []               # lines removed (outside a failing block)
    block:                 # per-test grouping: lines matching `body` after
      start: ''            # `start`, or after a `pass` line, belong to that
      body: ''             # test; a `pass` line drops them, a `fail` line keeps
      pass: ''             # them
      fail: ''
    failure: []            # lines that mean something failed (kept; no summary then)
    count: {}              # name -> regex; matching lines are counted for the summary
    sum: {}                # name -> regex with a capture group; the numbers of the
                           # matching lines are added up (e.g. maven's "Tests run")
    fold:                  # runs of repeated noise collapsed into one line: start
      - start: ''          # matches the first line of the run, body each further
        body: ''           # line, text is the line printed instead with
        text: ''           # {lines} = how many lines went (empty text drops the run)
    summary: ''            # printed when nothing failed and a count is > 0;
                           # {name} = count, a [part] whose counts are all 0 is
                           # left out
```

## Overriding

Users override or add rules by name (a rule with the name of a built-in one
replaces it, a new name is appended; later files win):

- `$SUGA_HOME/output-rules.yaml`
- `<repo>/.suga/output-rules.yaml`

Both hold the same `rules:` shape, `enabled: false` turns a built-in rule off.
