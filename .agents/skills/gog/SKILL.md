---
name: gog
description: Use when gog should fill Google connector gaps for Gmail, Calendar, Drive, Docs, Sheets, Chat, or Contacts.
---

# gog

Use `gog` as a local Google services CLI when built-in connectors are missing a feature, need shell-friendly JSON, or need account/auth inspection.

## Sources

- Repo: `~/Projects/gogcli`
- CLI: `gog`
- Config: `~/Library/Application Support/gogcli/config.json`
- OAuth/keyring: `~/Library/Application Support/gogcli`

## Auth

Inspect accounts before assuming auth is blocked:

```bash
gog auth list
gog auth status
```

Use `--json` for scriptable output and `--dry-run` for write planning where supported.

## Common Surfaces

```bash
gog gmail search "from:example@example.com"
gog calendar events list --json
gog drive search "name contains 'deck'" --json
gog contacts search "Name" --json
gog sheets --help
```

Prefer native Gmail/Calendar/Drive/Slack connectors first when they cover the task. Use `gog` for gaps, local automation, or auth/debug state.

## Safety

Do not send email, create/update/delete files, change calendar events, or alter contacts unless the user explicitly asks. For writes, summarize the target account and intended mutation first unless the user already gave a concrete command.

## Verification

For repo edits:

```bash
make test
make
```

Smoke:

```bash
gog --version
gog auth status --json
```
