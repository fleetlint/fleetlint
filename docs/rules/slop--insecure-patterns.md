# slop/insecure-patterns

No disabled TLS verification or similar shortcuts

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No globally disabled certificate verification, `eval(`, `chmod 777` or wildcard CORS origin in non-test code.

## Why

Each one was added to make something work once and removes a protection for every user afterwards.

## Check

For each `item` in:

```cel
codegrep("nontest", r'badCertificateCallback|InsecureSkipVerify:\s*true|verify\s*=\s*False|rejectUnauthorized:\s*false|NODE_TLS_REJECT_UNAUTHORIZED|danger_accept_invalid_certs|\beval\(|chmod\s+(-R\s+)?777|Allow-Origin[\x22\x27]?\s*[:,]\s*[\x22\x27]\*')
```

```cel
false
```

## Fix

Scope the exception to one host or one path with explicit opt-in, or remove it.

Agent instruction: Report each occurrence with what it disables; replace only when the secure equivalent is evident.

See: docs/baseline.md#slop
