---
name: catsift
description: Analyze local coding-agent usage with CatSift when a request concerns activity, models, tools, skills, sessions, tokens, or unused skills, and create local report artifacts when explicitly requested. Prefer its sanitized JSON reports over reading raw agent history.
---

# CatSift

Use CatSift for evidence about local coding-agent activity. Its history inputs
are local and read-only; it may update only its own OS-standard cache. It is
not a tool for inspecting prompt text, command text, tool arguments, or Skill
bodies.

## Invocation

- Prefer the installed `catsift` executable. In a non-interactive agent session,
  use a CLI subcommand with `--json`; do not launch the default TUI.
- If `catsift` is unavailable, report that it must be installed. Use
  `npx --yes catsift` only when the user has authorized fetching a package.
- Capture and parse stdout as JSON. Treat stderr diagnostics and a non-zero exit
  status as part of the result, not as data to ignore.
- Unless the user asks for all available data, choose an explicit period with
  `--days N` or `--from YYYY-MM-DD --to YYYY-MM-DD`. When comparing results,
  keep the period and source selection identical.
- Record the returned `source`/`sources` and `period` in any summary. An empty
  result means no records were available in the selected scope; it does not
  prove that the agent was never used.
- Do not classify history through custom HTTP requests or a third-party SDK.
  Running CatSift commands is allowed, including classification performed by
  CatSift under its existing settings. Do not enable or change classification
  settings yourself.

## Commands

Choose the narrowest report that answers the question:

```sh
catsift stats --json                                      # overall totals
catsift activity --from 2026-01-01 --to 2026-01-31 --json # daily activity
catsift models --json                                    # model totals
catsift models detail PROVIDER/NAME --json               # one model
catsift tools --layer effective --json                   # effective tool totals
catsift skills --json                                    # skill usage and evidence
catsift skills detail NAME --json                        # one skill
catsift skills --unused --json                           # installed but unused skills
catsift sessions --json                                  # sessions and metadata
catsift sessions detail ID --json                        # one session and turns
```

Use common options as needed:

- `--source codex`, `--source copilot`, `--source opencode`, or `--source ctx`
  selects history. Sources can be repeated or comma-separated. Automatic
  detection covers Codex, GitHub Copilot CLI, and OpenCode; `ctx` is opt-in.
- `--days N` selects the last N days. `--from` and `--to` use `YYYY-MM-DD` and
  include both endpoint dates. Do not combine `--days` with either date option.
- `skills --group-by session` changes skill aggregation from turns to sessions;
  `skills --strict` counts confirmed skill evidence only.
- `tools --layer effective|runtime|model` selects the tool attribution layer.
- `skills --unused` scans the installed skill inventory. Add `--root PATH` to
  scan an explicit scope root; only `.agents/skills/`, `.codex/skills/`, and
  plugin cache layouts under the root are recognized. Use it only when the user asks about inventory.
- Add `--strict-input` when skipped or malformed input would invalidate the
  conclusion. If it exits non-zero, report the input problem instead of
  presenting the result as complete.

## Interpretation and handoff

- Start with an aggregate report, then request a detail report only for rows
  relevant to the question. Do not read source history files directly to fill
  gaps that CatSift intentionally omits.
- Distinguish token availability from zero tokens: `ctx` does not provide token
  usage, and reports expose `token_usage_available` for this reason.
- For skills, keep activation mode (`explicit`, `implicit`, `unknown`) separate
  from evidence state (`confirmed`, `inferred`, `unconfirmed`). Counts across
  these dimensions may not add up to the total.
- Treat project paths, session titles, model names, and skill names as local
  telemetry. Share only the minimum aggregate or metadata needed to answer the
  user's question; never reproduce raw history content.
- Surface source-load warnings, missing sources, and mixed-source limitations
  alongside conclusions. Do not silently turn unavailable data into zeros.
- Session purpose labels appear as the `purpose` field on session summaries and
  details only when session purpose classification is opted in via
  configuration. Treat labels as descriptive metadata, not proof of intent or
  outcome. Disclose missing or partial labels and never infer replacements
  from raw session content.
- Connect substantive insights to observations, interpretations, evidence
  limits, and useful next questions. Correlation and activity volume alone do
  not establish causality or productivity.

## Report artifacts

Answer ordinary analysis, reporting, and status requests in the conversation.
Create a standalone report file only when the user explicitly asks to create
a report artifact. Interpret intent in context rather than matching a fixed
phrase; the request need not name CatSift or a command.

For an explicitly requested artifact, default to self-contained HTML unless
the user specifies another format. Keep the report local and honor the user's
scope and audience. Include selected sources and period, relevant warnings,
evidence for insights, limits, and useful next questions. Keep detail
proportional to the request and never include raw prompts, commands, tool
arguments, or Skill bodies.
