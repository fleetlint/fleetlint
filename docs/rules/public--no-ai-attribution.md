# public/no-ai-attribution

Public history carries no AI attribution

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

No commit in the recent history (`params.commits`, default 200) names an AI tool as author or co-author or carries a "generated with" footer. The owner's policy is that public repositories show human authorship only.

## Why

Attribution trailers are added by default by agent tooling; preventing them is a settings matter, removing them is a history rewrite.

## Applies when

```cel
repo.public && repo.has_git
```

## Check

For each `item` in:

```cel
commits(params.commits)
```

```cel
!(item.email.endsWith("@anthropic.com")
  || item.author.matches("(?i)\\b(claude|copilot|chatgpt|openai|gemini|cursor|codex|devin|aider)\\b")
  || item.trailers.matches("(?i)co-authored-by:.*(claude|copilot|chatgpt|openai|gemini|cursor|codex|devin|aider|noreply@anthropic|noreply@github\\.com>\\s*$)")
  || item.body.matches("(?i)(generated|written) (with|by) \\[?(claude|copilot|chatgpt|cursor|codex|aider)"))
```

## Parameters

- `commits`: `200`

## Fix

Turn off commit attribution in the agent's settings; rewrite only unpushed history.

Agent instruction: Stop adding Co-Authored-By trailers. Report the offending commits; rewriting pushed history needs the user's explicit decision.

See: docs/baseline.md#public-repositories
