# lint/flutter-strict-modes

The Dart analyzer runs in all three strict modes

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | flutter |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

`analyzer.language` in analysis_options.yaml sets every mode in `params.required` to true.

## Why

Each mode closes a different hole through which `dynamic` enters the program unnoticed.

## Applies when

```cel
file("analysis_options.yaml") && yaml("analysis_options.yaml") != null
```

## Check

For each `item` in:

```cel
params.required
```

```cel
has(yaml("analysis_options.yaml").analyzer) && has(yaml("analysis_options.yaml").analyzer.language) && item in yaml("analysis_options.yaml").analyzer.language && yaml("analysis_options.yaml").analyzer.language[item] == true
```

## Parameters

- `required`: `[strict-casts strict-inference strict-raw-types]`

## Fix

Set the missing modes to true under analyzer.language.

Agent instruction: Set each reported mode to true under analyzer.language and list the analyzer errors that appear instead of adding ignores.

See: docs/stacks.md#flutter--dart
