# Parser conformance corpus

`TestConformance` parses every corpus case with Krate (`astprint.Parse`),
prints it back (`astprint.Print`) and compares the result against the original
source. Both sides are normalized through esbuild (JSX lowered to `h(...)` /
`F` calls, whitespace minified, comments and quote style canonicalized), so a
case passes only if Krate produced the same program, not merely no errors.
If the strict normalized outputs differ, a second comparison with esbuild's
syntax minification enabled is tried so printer style (added braces, empty
statements, key quoting) is not counted as a miscompile. Dropped or reordered
code still fails. Must-error cases pass when Krate reports at least one error.
Positive cases that esbuild itself rejects are skipped.

## Suites

- `test262/*`: a curated sample of tc39/test262 `test/language/**` (BSD-3,
  see `testdata/test262/LICENSE`). Frontmatter is read by the runner:
  `negative: phase: parse` becomes a must-error case, `onlyStrict` prepends
  `"use strict";`. Module, async-flag and `includes` tests were excluded when
  sampling.
- `babel-jsx/*`: Babel parser JSX fixtures (MIT, `testdata/babel-jsx/LICENSE`);
  everything under `errors/` is must-error.
- `jsx-handwritten/*`, `es-handwritten/*`: `testdata/handwritten/*.txt`. Each
  case starts with `=== name` (or `=== name !` for must-error) followed by
  source.

## Baseline and regressions

`testdata/baseline.json` lists the IDs of passing cases. The test fails if a
baseline case no longer passes, and logs newly passing cases. After fixing
parser bugs, re-baseline:

    go test ./internal/conformance -run TestConformance -update
    # or KRATE_CONFORMANCE_UPDATE=1

## Environment

- `KRATE_CONFORMANCE_SUMMARY=path.json`: write per-suite counts, pass rate and
  the list of failures with reasons.
- `KRATE_CONFORMANCE_MARKDOWN=path.md`: write the pass-rate table (used by CI
  for `$GITHUB_STEP_SUMMARY`).
- `KRATE_CONFORMANCE_SHOW=substr`: log source and printed output for cases
  whose ID contains `substr` (run with `-v`).
