# Slop catalog

The traces careless or generated code leaves behind. Experienced reviewers notice them within minutes, and most of them predict real bugs. Each entry names the rule that finds it, or says that it is a review item.

The rules are in two places. `fleetlint:recommended` carries the four that are never acceptable in a commit (`slop/focused-tests`, `slop/conflict-markers`, `slop/scratch-scripts`, `slop/report-documents`). The preset `fleetlint:slop` carries the heuristics: add it next to another preset.

```yaml
extends: [fleetlint:recommended, fleetlint:slop]
```

The heuristics are regular expressions over tracked source files (vendored, generated and build paths are skipped). They produce false positives: treat their findings as a review list, use exceptions with a reason for the ones that are intended, and let the stack's linter (see [stacks.md](stacks.md)) enforce what it can enforce exactly.

## Comments and text

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Narration comments: "Now uses X", "Updated to…", "Fixed:", "NEW:", "as requested" | describes a diff or a conversation, not the code; stale immediately | `slop/narration-comments`. Delete; the why goes in a comment only if it is not obvious, what changed goes in the commit message |
| Restating comments: `// increment counter`, doc comments that repeat the name | noise that hides the comments that matter | review. Keep comments for constraints, workarounds, invariants, links |
| Banner and divider comments | sign of a file doing too many things | `slop/banner-comments`. Split the file at the banners |
| Emoji and decorative symbols in code and logs | log noise, encoding problems | `slop/emoji-in-code`, `slop/typographic-chars` |
| Placeholder or hedge comments: "In a real app…", "for simplicity", "not implemented yet" | admits the code is not real | `slop/placeholder-comments`. Implement it, or remove the feature and open an issue |
| TODO/FIXME without an issue reference | never gets done; nobody owns it | `slop/todo-without-issue`. Format `TODO(#123): …` |
| Commented-out code | dead weight; git already remembers | `slop/commented-out-code`. Delete |
| Vague errors: "Something went wrong", "An error occurred" | users and logs learn nothing | `slop/vague-errors`. Name the operation and the input that failed |

## Code structure

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Variant copies: `fetchDataV2`, `parseNew`, `handlerFixed`, `utils_old.py`, `Foo copy.ts` | two code paths, one of them wrong; fixes land in only one | `slop/variant-names`, `slop/variant-files`. Change the original, update callers, delete the copy |
| Near-duplicate functions across files | the same bug in several places | review; the stack's duplication tool |
| `utils` / `helpers` / `common` / `misc` dumping grounds | no owner, grows forever, hidden coupling | `slop/dumping-ground-files`. Move each function to the module that owns its concept |
| Pass-through layers that only delegate; single-implementation interfaces without a test seam | indirection without meaning | review. Collapse layers that add nothing |
| Arbitrary sleeps and retry loops around flaky behaviour | hides race conditions; slow and still flaky | `slop/arbitrary-delays`. Await the real event, fix the ordering |
| Defensive overkill: null checks on non-null values, defaults everywhere, try/catch around everything | masks bugs and turns crashes into silent wrong behaviour | review. Validate once at the boundary |
| Options, flags and settings nobody uses; compatibility shims for data that never shipped | more states to test | review. Delete until there is a real need |
| Stub implementations returning hard-coded data in production code | silent wrong behaviour | `slop/placeholder-comments`, `slop/placeholder-values`. Implement or remove |
| Reinvented standard library; JSON, URLs, HTML or dates parsed with regular expressions | edge cases wrong | review. Use the standard library or a proper parser |
| Two HTTP clients, two date libraries, three state approaches | every change needs to know all of them | review. Pick one, record it in an ADR, migrate the rest |

## Errors, types and robustness

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Empty catches, `except: pass`, `_ = err`, `.catch(() => {})` | failures vanish | `slop/swallowed-errors` (single-line forms); the stack's linter for the rest |
| Escape hatches: `any`, `@ts-ignore`, `# type: ignore`, `!!`, `.unwrap()`, `// ignore:`, `nolint` | turns the checker off exactly where it would help | `slop/escape-hatches` lists them. Fix the type; justified suppressions carry a reason |
| Debug-print sprawl | log noise, leaks data | `slop/debug-output`. Structured logger |
| Disabled TLS verification, `eval`, `chmod 777`, wildcard CORS | a shortcut that removes a protection for everyone | `slop/insecure-patterns` |
| Fire-and-forget async, missing awaits, listeners and timers never cancelled | leaks, lost errors | review; unawaited-future lints |

## Tests

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Assertion-free tests, `expect(true)`, tests that only check "no exception" | coverage without protection | `slop/trivial-asserts`; mutation testing exposes the rest |
| Mocking the unit under test, or re-implementing its logic to compute the expected value | the test cannot fail | review. Fakes only at outer boundaries; hard-code expected values |
| Expected values changed to match buggy output | the bug is locked in | review of test diffs |
| Skipped tests (`.skip`, `@Ignore`, `t.Skip`) | silent gaps | `slop/skipped-tests`. A quarantined test references an issue |
| Focused tests (`.only`, `fit`) | disables the rest of the suite | `slop/focused-tests` (error) |
| Sleep-based timing in tests | flaky | review. Fake clocks, wait for conditions |

## Dependencies

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Invented or typo-squatted package names | supply-chain attacks target exactly these | review of every new dependency: exists, maintained, right publisher |
| A dependency for a few lines of code; several libraries for the same job | attack surface and churn | review |
| Forked or vendored copies without documentation | cannot be patched | review. Document upstream, base version and changes, or switch back |
| Missing lockfile | unreproducible builds | `repo/lockfile-committed` |

## Repository and files

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Junk files: `.DS_Store`, `*.bak`, `*.orig`, `*.log`, build output | noise; sometimes leaks data | `repo/no-tracked-junk` |
| Scratch scripts in the root: `fix_imports.py`, `debug-auth.js`, `tmp_check.sh` | unmaintained code paths | `slop/scratch-scripts`. Real tools go in `scripts/` |
| Report documents: `IMPLEMENTATION_SUMMARY.md`, `FIXES.md`, `REFACTORING_PLAN.md`, `NEXT_STEPS.md` | always stale | `slop/report-documents` |
| Tracked `.env` files, keys, tokens | credential leak | `repo/no-tracked-env`, `hooks/secret-scan`. Rotate what leaked |
| Agent instruction files in public repositories | a public repository shows what its maintainers wrote | `public/no-agent-files` |
| Merge conflict markers | the file means what neither side intended | `slop/conflict-markers` (error) |
| Lint rules disabled in configuration to get green | hides real bugs | review. Ratchet with a baseline instead |

## Documentation

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Marketing adjectives (the list is in the rule) | says nothing a reader can act on | `quality/no-slop-markers` |
| Emoji headings | break anchors and search | `slop/emoji-headings` |
| Feature lists the code does not implement; badges for services that do not exist | misleading | review. Every claim is true today |
| Overview, Conclusion and Summary filler; the same content in several files | walls of text nobody reads | review. One place per fact |
| Instructions and examples that do not run | wrong documentation is worse than none | review. Run them; use doc tests where the stack has them |

## Commits and history

| Smell | Why it matters | Rule / clean up |
|---|---|---|
| Vague messages: "fix", "update", "wip", "changes" | the history is useless for debugging | `slop/vague-commit-messages`; `hooks/conventional-commits` prevents new ones |
| Subjects that are not Conventional Commits | changelog and version cannot be computed | `slop/non-conventional-commits` |
| Huge commits mixing many concerns | cannot be reviewed or reverted | `slop/huge-commits` |
| A message that does not match the diff | misleading history | review. Write the message from the diff |
| AI author, co-author trailers or "generated with" footers in public repositories | a public history shows human authorship | `public/no-ai-attribution` |
