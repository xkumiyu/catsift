---
name: catsift-report
description: Write a local CatSift report only when the user includes the exact phrase `レポートを作成して`. Do not activate for other report-related wording or paraphrases.
---

# CatSift report writing

Use this skill only when the user's request contains the exact phrase
`レポートを作成して`. Do not activate for translations, paraphrases, or other
report-related wording. Follow any additional scope or audience the user gives.

## Collect data

Follow the `catsift` Skill for executable selection, command choice, source and
period consistency, JSON parsing, warning handling, and privacy. Do not repeat
or override its collection rules. If that Skill is unavailable, ask the user to
make it available before collecting data.

Use only CatSift's normalized reports for analysis. Do not read raw history to
fill gaps. Keep reports local. Never include raw prompts, commands, tool
arguments, or Skill bodies in a report. Do not classify history through custom
HTTP requests or a third-party SDK. Running CatSift commands is allowed,
including any classification performed by CatSift under its existing settings.
Do not enable or change classification settings yourself.

## Analyze

Make every substantive insight evidence-backed, using this chain:

1. **Observation:** State the relevant pattern and its scope from the report.
2. **Interpretation:** Explain what the pattern may mean, without overstating it.
3. **Uncertainty or limit:** Name missing, incomplete, or ambiguous evidence.
4. **Useful next question:** Suggest a concrete question that could clarify or
   test the interpretation.

Do not merely paraphrase counts. Do not claim that a tool, model, skill, or
workflow caused an outcome or changed productivity unless the available data
directly supports that claim. Correlation and activity volume alone do not
establish causality or productivity.

Use per-session purpose labels when CatSift provides them. Treat labels as
descriptive metadata, not proof of intent or outcome. If labels are unavailable
or incomplete, disclose that limitation and do not infer replacement labels
from raw session content.

## Write the report

Default to a self-contained HTML report. If the user specifies Markdown or
another format, use that format instead. Keep the report local and include the
selected sources and period, relevant warnings, evidence for each insight,
limits, and useful next questions. Keep detail proportional to the request and
avoid exposing unnecessary local telemetry.
