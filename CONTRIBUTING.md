# CatSift Contribution Guide

Suggestions, bug reports, and Pull Requests are welcome.
Keep changes focused and follow existing specifications and privacy policies.

## Development environment

The following tools are required:

- Go
- golangci-lint
- Node.js
- npm

Tool versions are defined in [`mise.toml`](mise.toml).

If you use [mise](https://mise.jdx.dev/), run `mise install` to install them.

## Repository structure

| Path | Contents |
| --- | --- |
| `cmd/catsift` | CLI entry point |
| `internal/` | Go implementation and package tests |
| `npm/` | npm distribution wrapper and tests |
| `openspec/` | Specifications and change artifacts |
| `docs/` | Documentation assets |

## Change notes

- Add or update tests for each change. Place Go tests near their target package in `*_test.go` files, and use `*_fuzz_test.go` for fuzz tests.
- When changing specifications or CLI behavior, check the related specs under `openspec/` and update them when needed.
- When changing the README, keep `README.md` and `README.ja.md` in sync.

## Quality checks

Run checks after making changes and before opening a Pull Request. If you use mise, run the following command for the full check:

```sh
mise run check
```

To run individual checks with mise:

```sh
mise run fmt       # Format Go code
mise run lint      # golangci-lint
mise run vet       # go vet
mise run test      # Race-enabled Go tests
mise run npm-test  # npm wrapper tests
```

If you do not use mise, run the commands listed in the [CI configuration](.github/workflows/ci.yml).

## Test data and privacy

- Use only synthetic or sanitized data in fixtures, tests, and examples.
- Never commit real Codex, ctx, or OpenCode history or personal data.
- Do not store raw data such as prompt text, command text, Tool arguments, Skill bodies, or provider payloads in test fixtures or reports.
- Preserve the assumption that runtime does not modify input or send data externally, even when reading user-selected history.

## Pull Requests

- Explain the reason for the change, summarize the main changes, and list the checks you ran.
- Keep unrelated changes out of the same Pull Request.
- Commit message prefixes such as `feat:`, `fix:`, `docs:`, and `chore:` are recommended to make the change type clear.
