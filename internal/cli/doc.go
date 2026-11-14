// Package cli is the driving adapter: it parses the command line, calls the
// analysis use case and prints the result. It contains no business rules and
// constructs no adapter: the composition root (cmd/shift2eu) passes the adapters
// in through Deps, and Run builds them only once the command line is valid.
//
// # Commands
//
//	shift2eu assess --inventory <file> --target <targets> [--format <format>] [--output <file>]
//	                [--region <target>=<region>,...] [--no-estimate]
//	shift2eu version
//	shift2eu help
//
// assess reads the inventory file, loads the catalog of the target, assesses
// every resource and writes the report. --target takes one key of Deps.Targets,
// a comma-separated list of distinct keys, or all for every key. --format takes
// one key of Deps.Formats, markdown by default, and selects the reporters of
// that Format. One target runs analysis.Analysis with the format's
// NewReporter, exactly as a single-target run always has. Two or more run
// analysis.Comparison with the format's NewComparisonReporter, in
// sorted key order whatever order they were given in, reading the inventory once
// (docs/adr/0016). Only the selected targets' catalog sources are built. --inventory is a file path: standard
// input is not supported, so a source is always a file the inventory-file
// adapter reads once per run (ADR 0006). The report names the file by its base
// name only (analysis.ReportMeta). Without --output the report goes to standard
// output; with it, to the named file.
//
// Estimates are on by default (PRD #190): the run matches each sized resource
// to a plan and estimates its monthly cost (analysis.Options.Estimate,
// analysis.ComparisonOptions.Estimate); --no-estimate turns them off, and the
// report then holds no price at all. --region takes comma-separated
// <target>=<region> pairs, spaces around each pair, key and region dropped,
// each naming one selected target at most once: that
// target is assessed and estimated in that one region (analysis.Options.Region,
// analysis.Target.Region), every other in all its regions with estimates in its
// default region. Whether the target lists the region is checked by the use
// case against the loaded catalog, before the inventory is read; an
// *analysis.RegionError is a usage error naming the region and the target's
// regions. Flags are parsed by the standard library
// flag package, so -flag and --flag are the same.
//
// # Exit codes
//
//   - 0: the command ran. For assess, blockers are a result, not a failure.
//     Only the command's output is written, to standard output or to the
//     --output file; nothing goes to standard error.
//   - 1: a run-time error: the inventory, the catalog, the report or the output
//     failed, or the run was cancelled (SIGINT, SIGTERM or SIGHUP cancel the
//     context in cmd/shift2eu). Exactly one error line goes to standard error and
//     nothing to standard output.
//   - 2: a usage error: no command or an unknown one, a missing --inventory or
//     --target, an unknown, repeated or empty target in the --target list, or
//     all with another target (each error lists the supported targets), an
//     unknown or empty --format (the error lists the supported formats), a
//     malformed --region pair, a --region target --target does not select or
//     one named twice, an unknown flag or an extra argument. An error line and
//     the usage text go to standard error, nothing goes to standard output, and
//     no adapter is built. A --region the target's catalog does not list is a
//     usage error too: it is found once the catalogs are loaded, but before the
//     inventory is read or the --output file is written, and it prints the same
//     way.
//
// Help (help, -h, --help, or -h after assess) prints the usage text to standard
// output and exits 0.
//
// SIGQUIT is not part of this contract: it is the Go runtime's debugging
// signal, which dumps every goroutine to standard error and exits with code 2
// without running the command to its end.
//
// # Errors
//
// Every error, run-time or usage, is printed by printError as one line,
// "shift2eu: " and the message passed through sanitise. sanitise escapes control,
// format (Cf), line separator (Zl) and paragraph separator (Zp) characters and
// invalid UTF-8 bytes as strconv does (\n, \x1b, \u202e), so a message never
// breaks the line, moves the cursor or reorders the text, whatever the input
// file, the OS or an adapter put in it. The escaped message is capped at 1 KiB,
// then a truncation marker; the cut falls between characters, never inside an
// escape. The flag package prints nothing itself:
// its errors also go through printError.
//
// # Writing the report
//
// The report is rendered into memory in full before anything is written, so a
// failed run leaves no partial report anywhere. Without --output it is written
// to standard output once complete. With --output it is written to a temporary
// file created in the destination's directory only after rendering, set to
// mode 0600, synced and closed, then renamed onto the destination. On any error
// the temporary file is removed and an existing destination is left untouched,
// never truncated. A run cancelled before the report is written writes nothing.
//
// What --output may name is checked before the inventory is read:
//
//   - a path that does not exist yet: the report is created there;
//   - an existing regular file: it is replaced atomically, in one rename;
//   - a symbolic link to a regular file, or a dangling link: the link itself
//     is replaced, never followed, so its target keeps its content;
//   - anything else (a directory, a named pipe, a socket, a device, or a
//     symbolic link to one of them): refused with exit code 1, so --output
//     /dev/stdout, a link to the terminal or a pipe, is never replaced by a
//     regular file; leave out --output to write to standard output;
//   - the inventory file itself, by the same path, a hard link or a symbolic
//     link to it: refused with exit code 1, so a report never replaces the
//     inventory it assesses.
//
// A refused destination is left as it was and no temporary file is created.
package cli
