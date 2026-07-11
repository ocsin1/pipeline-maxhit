# CLAUDE.md

This project is pure vibe code — no human wrote a line of it. Claude (Anthropic) generated the entire codebase, including this file.

## Build

```bash
go build -o install/pipeline-maxhit .
```

## Test

```bash
go test -v ./...
```

## Format

```bash
npx prettier --write .
```
