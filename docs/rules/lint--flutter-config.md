# lint/flutter-config

Dart analysis is strict

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | flutter |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

analysis_options.yaml enables strict-casts, strict-inference and strict-raw-types and does not disable use_build_context_synchronously or unawaited_futures.

## Why

Those three switches are what turn Dart's analyzer from advisory into a type checker.

## Check

```cel
file("analysis_options.yaml") && has(yaml("analysis_options.yaml").analyzer.language) && yaml("analysis_options.yaml").analyzer.language["strict-casts"] == true
```

## Fix

Set analyzer.language strict-* to true and include very_good_analysis.

Agent instruction: Edit analysis_options.yaml: include very_good_analysis, set analyzer.language strict-casts/strict-inference/strict-raw-types true, remove rules that disable use_build_context_synchronously or unawaited_futures.
