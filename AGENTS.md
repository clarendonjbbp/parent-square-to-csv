# AGENTS.md

## Overview

This repository contains a small Go CLI that logs into ParentSquare, discovers class sections for a specific school, fetches students and parent contact data, and writes a CSV suitable for downstream import workflows.

The codebase is intentionally small:

- `cmd/parent-square-to-csv/parentsquare_to_csv.go`: the entire application entrypoint and scraping logic.
- `Makefile`: build, `go mod tidy`, and `golangci-lint` targets.
- `.golangci.yml`: lint and formatter policy.
- `README.md`: currently minimal and not a source of operational detail.

## Current Behavior

The binary:

- prompts interactively for `Email` and `Password`
- logs into `https://www.parentsquare.com`
- scrapes class names from `/schools/884/users`
- fetches students from `/api/v2/sections/<id>/students`
- fetches parent profile pages to extract `mailto:` links
- emits CSV rows with header `Name,Email,Email2,Group`

Important implementation details:

- The school ID `884` is hard-coded in multiple helper paths.
- HTML parsing is regex-based, so markup changes on ParentSquare can break the tool.
- `getParentEmails` pads to two email columns, so callers assume exactly two parent-email fields.
- There are no automated tests beyond `go test ./...` compilation coverage.

## Working Agreements

- Keep changes small and practical; this repo is a single-purpose utility.
- Preserve the interactive CLI behavior unless the task explicitly changes UX.
- Be careful with credentials: never hard-code real usernames, passwords, cookies, or tokens.
- Prefer improving resilience around scraping and error handling over adding abstractions for their own sake.
- If you change output format or login flow, update this file and `README.md`.

## Commands

Use these from the repository root:

- `make build`: builds `./cmd/parent-square-to-csv` into `./parent-square-to-csv`
- `go test ./...`: compile-only verification right now; there are no test files
- `make tidy`: runs `go mod tidy`
- `make lint`: runs `golangci-lint` using the pinned version in `.build/`

Notes:

- `make lint` downloads `golangci-lint` via `curl` if it is not already present.
- On restricted sandboxes, Go commands may need writable access to the Go build cache outside the repo.

## Code Conventions

- Stay within the existing standard-library-first style unless there is a clear payoff.
- Keep new code in the existing command package unless a refactor is justified by repeated logic or testability needs.
- Prefer explicit error handling with actionable messages.
- Keep CSV shape stable unless the task explicitly asks for a schema change.
- If you touch scraping regexes, verify them against the exact response shape they are meant to parse.

## Risk Areas

- Login and CSRF token handling assume the `/signin` page contains a matching meta tag.
- Several regex matches are used without defensive length checks, so malformed upstream responses can panic.
- ParentSquare endpoint and HTML structure changes are the highest maintenance risk in this repo.
- The hard-coded school ID means this tool is not yet general-purpose.

## Good Next Improvements

- Add table-driven tests around parsing helpers using saved fixture responses.
- Replace brittle regex parsing with HTML parsing where possible.
- Move the school ID to a flag or config value if multi-school support is needed.
- Close output files explicitly when writing to `-o`.

## Verification Status

Verified on April 11, 2026:

- `make build` passed
- `go test ./...` passed with `"[no test files]"`
