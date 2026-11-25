# Hostile inventories

The scheduled security audit's corpus for row 13, the CLI on hostile inventory files (PRD #175,
[ADR 0040](../../../../docs/adr/0040-audit-the-repository-and-the-latest-release-on-a-schedule.md)). The
checks job copies every `*.json` file here, with the files it generates at run time (an oversized file,
too many resources, deep nesting, malformed UTF-8, a byte order mark, a NUL byte, a huge string, an
empty file, a directory and links to `/proc/self/environ`, `/dev/zero` and `/dev/urandom`), into a
container of the latest release's image that never starts, commits it, and runs `shift2eu assess` on
each file in a read-only container without network. The raw output goes only to the encrypted results.

Every value is sanitised (CLAUDE.md rule 2): `example.com`, `192.0.2.0/24` and `123456789012` only.
Each file holds one kind of hostility; a file the decoder refuses at its first problem holds that one
problem alone, so the refusal is the one under test.
