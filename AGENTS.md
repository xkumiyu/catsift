# AGENTS.md

## Repository

- Go CLI module: `github.com/xkumiyu/catsift`.
- CLI entrypoint: `cmd/catsift`; implementation packages: `internal/`.
- Keep Go tests beside packages as `*_test.go`; fuzz tests use `*_fuzz_test.go`.
- `npm/` contains the Node.js distribution wrapper and its tests.

## Git and worktrees

- `main` is the current default branch. Do not make tracked or commit-target changes directly on it.
- Use at most one task worktree per agent session. If already in a suitable non-default task worktree, continue there; creating an additional worktree requires the user's explicit approval.
- For isolation, prefer `wt switch --create <task-branch> --no-cd --format=json`; otherwise use `git worktree add`. Use the resulting absolute path and verify the worktree root and branch before editing.
- Preserve existing user changes; never stash, reset, overwrite, or clean them automatically.

## Development

- Use `mise` tasks and tool versions. After code, configuration, or npm wrapper changes, run `mise run check`.
- Use `mise run fmt` for Go formatting. For `.goreleaser.yaml` changes, run `mise run release-check`.

## Planning and specifications

- Create an OpenSpec change before implementation, even without an explicit user request, when the change introduces a substantial user-facing capability, changes the meaning or compatibility of existing behavior, affects data/privacy/schema boundaries or architecture, requires migration, or demands substantial cross-package work with multiple independent tasks.
- Small, localized changes that do not materially affect those areas normally do not require an OpenSpec change.
- When an OpenSpec change is required, use the deployed skills such as `openspec-new-change`, `openspec-propose`, `openspec-apply-change`, and `openspec-verify-change`, and validate it with `openspec validate <change-name> --strict`.

## Tests and data

- Use synthetic or sanitized repository-local data for tests, fixtures, examples, and generated artifacts; never commit real external history or personal data.
- Runtime may read user-selected external history, but all inputs remain read-only and local-only; never send them externally. Reports and caches contain normalized fields only; never persist or display raw prompts, commands, tool arguments, or Skill bodies.

## Documentation

- Keep `README.md` concise and user-facing. It is not an exhaustive reference, changelog, design document, or implementation note.
- Document only stable behavior needed to install, run, or understand CatSift. Prefer `catsift --help` for detailed options and omit internal details, exhaustive TUI operations, and rare edge cases.
- Keep `README.md` and `README.ja.md` semantically aligned. Limit changes to affected sections, update both files, and run `git diff --check`.
- `openspec/` artifacts follow `openspec/config.yaml`: Japanese prose; common technical terms remain English.

## Generated files and dependencies

- `.agents/skills/` is materialized by APM from `apm.yml` and `apm.lock.yaml`; never hand-edit deployed skills. Update lock/output through APM.
- `go.mod` is dependency manifest; update `go.sum` with Go tooling. `apm.lock.yaml` is generated; update it with APM tooling.

## Pull requests

- Before opening a PR, run `openspec list --json` and check for completed changes still outside `openspec/changes/archive/`.
- Archive completed changes with OpenSpec tooling, for example `openspec archive <change-name>`, before opening the PR.
