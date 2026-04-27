---
summary: "Encrypted Google account backups"
read_when:
  - Adding a new gog backup service adapter
  - Changing encrypted backup layout, manifest fields, or age identity handling
  - Debugging backup-gog push, status, or verify
---

# Encrypted Backups

`gog backup` writes Google account data into a Git repository as age-encrypted
JSONL gzip shards. The intended repository is private, for example
`https://github.com/steipete/backup-gog`, but service payloads are encrypted
before Git sees them.

## Commands

Initialize local config, create an age identity if needed, seed the backup repo,
and print the public recipient:

```bash
gog backup init \
  --repo ~/Projects/backup-gog \
  --remote https://github.com/steipete/backup-gog.git
```

Back up all supported services:

```bash
gog backup push --services all --account steipete@gmail.com
```

Back up only Gmail:

```bash
gog backup push --services gmail --account steipete@gmail.com
```

For a bounded smoke run:

```bash
gog backup push --services gmail --account steipete@gmail.com --query 'newer_than:7d' --max 25
```

Inspect cleartext manifest metadata:

```bash
gog backup status
```

Decrypt every shard and verify hashes and row counts:

```bash
gog backup verify
```

Decrypt one shard to stdout:

```bash
gog backup cat data/gmail/<account-hash>/labels.jsonl.gz.age --pretty
```

Write an unencrypted local copy for easy reading on the Mac:

```bash
gog backup export --out ~/Documents/gog-backup-export
```

Use `--no-push` on `init` or `push` to commit locally without pushing to the
remote.

Supported services:

- `gmail`: labels and raw MIME messages.
- `calendar`: calendar list entries and all events, including deleted events.
- `contacts`: People API contacts and other contacts.
- `tasks`: task lists and tasks, including completed, deleted, hidden, and
  assigned tasks.
- `drive`: shared drives and Drive file metadata. File contents are not copied
  by the Drive adapter yet.

`all` expands to every supported service. Pushing a subset updates that subset
and preserves existing shards for services that were not selected, as long as
the age recipients are unchanged.

## Files

Local config:

```text
~/.gog/backup.json
~/.gog/age.key
```

Backup repo:

```text
README.md
manifest.json
data/gmail/<account-hash>/labels.jsonl.gz.age
data/gmail/<account-hash>/messages/YYYY/MM/part-0001.jsonl.gz.age
data/calendar/<account-hash>/...
data/contacts/<account-hash>/...
data/drive/<account-hash>/...
data/tasks/<account-hash>/...
```

`manifest.json` is intentionally cleartext. It contains format version, export
time, public age recipients, service names, account hashes, shard paths, row
counts, encrypted byte sizes, and plaintext hashes used for verification. It
does not contain email subjects, senders, recipients, bodies, raw message IDs,
or labels.

Plaintext export directory:

```text
README.md
manifest.json
gmail/<account-hash>/labels.json
gmail/<account-hash>/messages/index.jsonl
gmail/<account-hash>/messages/YYYY/MM/<timestamp>-<message-id>.eml
raw/<service>/...
```

`gog backup export` decrypts and verifies the manifest-backed shards before
writing files. Gmail messages become `.eml` files that open in Mail and other
mail clients. Other services are written as verified JSONL under `raw/`. The
export is not encrypted; do not place it inside the backup Git repository, and
keep it out of synced/shared folders unless that is intentional.

## Encryption

Backups use the Go `filippo.io/age` library with X25519 age identities. There
is no backup password. The private identity is stored locally:

```text
~/.gog/age.key
```

The matching public recipient starts with `age1...` and is safe to store in
`~/.gog/backup.json` and `manifest.json`. The private `AGE-SECRET-KEY-...`
value must stay local or in a password manager.

For each shard, `gog backup push`:

1. Exports deterministic JSONL rows.
2. Gzip-compresses the JSONL with a fixed gzip timestamp.
3. Encrypts the compressed bytes with age for every configured recipient.
4. Writes only encrypted `*.jsonl.gz.age` files to Git.
5. Writes `manifest.json` with cleartext metadata for status and verification.

`gog backup verify` decrypts each shard with the local age identity, gunzips it,
checks the plaintext SHA-256 hash from the manifest, and verifies row counts.
`gog backup cat` and `gog backup export` use the same verification path before
returning plaintext.

## Security Boundary

The encrypted shards protect Google content from GitHub and anyone else without
the local age identity. That includes email bodies, subjects, senders,
recipients, raw MIME payloads, labels, Drive filenames, contacts, event titles,
and similar service data.

The manifest is not secret. It leaks operational metadata by design:

- Export time.
- Public age recipients.
- Service names.
- Account hashes.
- Shard paths and month buckets.
- Row counts.
- Encrypted byte sizes.
- Plaintext shard hashes used by `verify`.
- Backup cadence and which shards changed in Git history.

The account hash is not anonymity. It is useful to avoid putting the literal
email address in paths, but someone who can guess the address can compute and
compare the same hash.

Current trust model:

- Confidentiality: good for a private GitHub backup repo as long as
  `~/.gog/age.key` stays private.
- Integrity against random corruption: `age` authentication, gzip decoding,
  plaintext SHA-256, and row-count verification catch damaged shards.
- Integrity against repository writers: limited. Anyone with push access can
  replace encrypted backup data with different data encrypted to the public
  recipient. Keep repo write access restricted and review unexpected commits.
- Key compromise: if `AGE-SECRET-KEY-...` leaks, historical shards in Git
  history may be readable. Rotate recipients, re-encrypt, and treat old Git
  history as exposed unless it is rewritten and all copies are removed.

Future hardening ideas:

- Store only ciphertext hashes in the public manifest and move plaintext hashes
  into encrypted shard metadata.
- Sign manifests or commits with a local signing key so `verify` can prove who
  created the backup, not just that the files are internally consistent.
- Add optional shard padding or disable gzip for deployments that care more
  about size side channels than repository size.

## Service Adapters

The Gmail adapter backs up:

- Gmail labels.
- Raw Gmail messages from `users.messages.get(format=raw)`.

Raw message payloads stay base64url encoded inside encrypted JSONL. This
preserves the RFC 2822 message content while keeping the shard format text
friendly.

`--include-spam-trash` defaults to true. Use `--query` and `--max` for bounded
test exports; omit them for a full mailbox scan.

The Calendar adapter backs up calendar list entries and all events from each
calendar. The Contacts adapter backs up contacts and other contacts. The Tasks
adapter backs up task lists and tasks. The Drive adapter backs up shared drives
and file metadata, including names, owners, parents, links, checksums, export
links, and selected custom properties. Drive file contents are intentionally
left for a later adapter pass because they need format choices, bandwidth
limits, and resume/checkpoint behavior.

## Adding Services

Keep one backup engine and add service adapters. A service adapter should:

1. Resolve the authenticated account through normal `gog` auth.
2. Export stable rows without writing Google data.
3. Store sensitive identifiers, titles, names, and content inside encrypted
   shards only.
4. Add cleartext manifest counts and account hashes only.
5. Support bounded smoke flags when the service can be huge.

Good next adapters: Drive file content export, Docs/Sheets/Slides native
exports, Chat, Forms, Classroom, and Apps Script.
