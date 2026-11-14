# Assess an inventory

`shift2eu assess` reads an inventory file and reports, per resource, what each EU target offers for it:
one target's assessment or a comparison of several, as Markdown or as versioned JSON, offline and
without credentials. This page is the command's manual: its flags, its report formats, the comparison
and its exit codes.

## Usage

```text
shift2eu assess --inventory <file> --target <targets> [--format <format>] [--output <file>]
```

`<targets>` is one of `ovhcloud`, `scaleway` or `upcloud`, a comma-separated list of them, or `all` for
every target; `shift2eu help` lists them. For example:

```bash
make build
./bin/shift2eu assess --inventory inventory.json --target ovhcloud
./bin/shift2eu assess --inventory inventory.json --target scaleway --output report.md
./bin/shift2eu assess --inventory inventory.json --target upcloud
./bin/shift2eu assess --inventory inventory.json --target ovhcloud,upcloud
./bin/shift2eu assess --inventory inventory.json --target all --output comparison.md
./bin/shift2eu assess --inventory inventory.json --target scaleway --format json --output report.json
```

`assess` reads an inventory file, compares every resource with the target's catalog and writes a
Markdown report: per resource its taxonomy category, best offering, fit (direct, partial, none) and the
reasons, then the required changes and the blockers. Without `--output` the report goes to standard
output. With it, the report is written to the file with mode 0600, and an existing regular file is
replaced atomically, only once the whole report is rendered; a failed or cancelled run leaves it as it
was. A symbolic link to a regular file, or a dangling link, at that path is replaced, not followed.
`--output` naming anything else (a directory, a named pipe, a socket, a device, or a symbolic link to
one of them, such as `/dev/stdout`) or the inventory file itself (the same path, a hard link or a
symbolic link to it) is refused with exit code 1 before the inventory is read; leave out `--output` to
write to standard output.

`--format` chooses the report format: `markdown` (the default) or `json`. The JSON report holds the same
content as the Markdown one, as a versioned document (`"schemaVersion": 1`, `"kind": "assessment"`, or
`"comparison"` for several targets) for scripts and other tools. Its format, its JSON Schemas
(`internal/adapter/report/jsonreport/schema/`) and its versioning rules are in
[ADR 0018](../adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md).

With two or more targets, `assess` reads the inventory once and writes one comparison report. The
targets are compared in sorted key order, whatever order they are given in. The report starts with a
comparison: the fits counted per target, one row per resource with its fit and best offering on each
target, and the resources that are blockers on every target. Each target's own report follows, one
heading level lower. The comparison states facts per target; it does not rank the targets or recommend
one. Its start looks like this:

```markdown
# Migration assessment: `OVHcloud`, `Scaleway`, `UpCloud`

- Inventory file: `inventory.json`
- Source: `aws`
- Catalog: `OVHcloud` snapshot from `https://www.ovhcloud.com/en-ie/public-cloud/prices/`, retrieved 2026-11-14
- Catalog: `Scaleway` snapshot from `https://www.scaleway.com/en/pricing/`, retrieved 2026-11-13
- Catalog: `UpCloud` snapshot from `https://upcloud.com/pricing/`, retrieved 2026-11-13

## Comparison

### Summary

| Fit | `OVHcloud` | `Scaleway` | `UpCloud` |
|---|---|---|---|
| direct | 6 | 5 | 5 |
| partial | 2 | 5 | 3 |
| none | 3 | 1 | 3 |
| total | 11 | 11 | 11 |

### Resources

| ID | Category | `OVHcloud` fit | `OVHcloud` best offering | `Scaleway` fit | ... |
|---|---|---|---|---|---|
| `fn-example` | `serverless.function` | none |  | direct | ... |

### Blockers on every target

- `misc-example`
  - `OVHcloud`: not classified
  ...
```

A target named twice, an unknown target, an empty element (`ovhcloud,,scaleway`) or `all` together
with another target is a usage error. If any selected target's catalog fails, the run fails with exit
code 1 and writes no report.

The inventory file is versioned JSON, described in
[ADR 0006](../adr/0006-json-inventory-file-format-v1.md) (version 1) and
[ADR 0025](../adr/0025-json-inventory-file-format-v2.md) (version 2, which adds sizes, usage,
workloads, relations and the source's cost; the report does not show them yet);
[Build an inventory from your cloud](inventory.md) makes one from AWS, GCP or Azure. `--inventory` takes a path; standard input
is not supported. The targets are `ovhcloud` (OVHcloud Public Cloud), `scaleway` and `upcloud`, each
served from a dated snapshot of the provider's EU regions (for UpCloud, its zones in EU member states),
embedded in the binary, so `assess` runs offline and needs no credentials.

| Exit code | Meaning |
|---|---|
| 0 | The assessment ran. Blockers are a result, not a failure. |
| 1 | The inventory, the catalog, the report or the output failed, or the run was interrupted (SIGINT, SIGTERM, SIGHUP): one `shift2eu: ...` line on standard error. |
| 2 | Usage error: missing or unknown flag, command or target, or a malformed target list. Usage text on standard error. |

SIGQUIT is not part of the exit-code contract: it is the Go runtime's debugging signal, which dumps
every goroutine to standard error and exits with code 2.

`shift2eu version` prints the version, `shift2eu help` the usage.
