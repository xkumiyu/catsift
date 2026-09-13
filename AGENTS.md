# AGENTS.md

## Repository

- Go CLI module: `github.com/xkumiyu/catsift`.
- CLI entrypoint: `cmd/catsift`; implementation packages: `internal/`.
- Keep Go tests beside packages as `*_test.go`; fuzz tests use `*_fuzz_test.go`.
- `npm/` contains the Node.js distribution wrapper and its tests.

## Git and worktrees

- `main` is the current default branch. Do not make tracked or commit-target changes directly on it.
- If already in a suitable non-default task worktree, continue there; do not create another.
- For isolation, use `wt switch --create <task-branch> --no-cd --format=json` when available, otherwise `git worktree add`; honor configured and returned paths, never guess.
- After creating a worktree, do not assume the session has switched to it. Use the new worktree's absolute path explicitly and verify its repository root and branch before editing.
- Preserve existing user changes; never stash, reset, overwrite, or clean them automatically.

## Development

- Use `mise` tasks and tool versions. After code, configuration, or npm wrapper changes, run `mise run check`.
- Use `mise run fmt` for Go formatting. For `.goreleaser.yaml` changes, run `mise run release-check`.
- If check fails only with `parallel golangci-lint is running` or `no go files to analyze`, rerun `mise run lint` alone after competing processes finish, then rerun `mise run check`; report unresolved environment failures.

## Tests and data

- Tests, fixtures, examples, and generated artifacts use synthetic or sanitized repository-local data; never commit real external history.
- Runtime may read user-selected external data, but Codex, ctx, and OpenCode inputs remain read-only and must not be sent externally.
- Reports and caches contain only normalized fields; never persist or display raw prompts, commands, tool arguments, or Skill bodies.

## Documentation and generated files

- Keep `README.md` and `README.ja.md` content-equivalent and update both; synchronize commands, options, examples, section coverage, and code fences.
- For README updates, run `git diff --check` and compare headings, options, examples, and code fences.
- `openspec/` artifacts follow `openspec/config.yaml`: Japanese prose; common technical terms remain English.
- `.agents/skills/` is materialized by APM from `apm.yml` and `apm.lock.yaml`; never hand-edit deployed skills. Update lock/output through APM.
- `go.mod` is dependency manifest; update `go.sum` with Go tooling. `apm.lock.yaml` is generated; update it with APM tooling.

## Pull requests

- Before opening a PR, run `openspec list --json` and check for completed changes still outside `openspec/changes/archive/`.
- Archive completed changes with OpenSpec tooling, for example `openspec archive <change-name>`, before opening the PR.
