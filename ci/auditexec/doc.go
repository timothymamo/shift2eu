// Command auditexec is the command wrapper of the scheduled security audit,
// installed in the model job as /srv/audit/bin/audit-exec (ADR 0040's
// milestone-2 amendment, issue #277, PRD #175 milestone 2): the one program
// the audit's model may run through Bash.
//
// `audit-exec <name> <args...>` maps name to a fixed absolute path, never
// through PATH, and replaces itself with that program (syscall.Exec), with an
// environment of exactly its own allow list: nothing of the caller's is
// inherited. git runs as `/usr/bin/git --no-pager -C /srv/audit/checkout
// <subcommand> ...` with one of the read-only subcommands the ADR lists, and
// never with an option that names another configuration, git directory, work
// tree, program or output file; sort never writes a file of its own. Every
// other name, a path given as the name included, every other git subcommand
// and every refused option ends the run with exit 126 and one line on stderr
// that names the refused word, quoted and cut to 64 bytes.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-exec to the standard packages it
// imports.
package main
