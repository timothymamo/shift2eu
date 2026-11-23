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
// tree, program or output file, nor --show-signature, nor any word that could
// ask for a signature's verification (%G with or without git's +, - or space
// modifier, %(signature, and on a for-each-ref line any word holding
// signature), wherever it sits on the line, so a grep pattern holding %G is
// refused too, nor with a single-dash word that holds -c or -O anywhere in its
// cluster. sort takes only an allow list of options, which
// order and compare lines (-k and -t with their values among them), so it never
// runs a program, reads a file it was not given or writes one; uniq takes one
// operand at most, so its OUT never writes a file. On every command's line,
// a word that names a path into /proc, /sys or /dev is refused (the wrapper's
// path rule, issue #277 round F): a non-option word holding "/", the value of
// an option after its "=", and git's -C value, attached or the next word,
// each cleaned as filepath.Clean cleans it and read from "/" when relative,
// so a glob the CLI's deny rule could not see is refused once the shell has
// expanded it. Every
// other name, a path given as the name included, every other git subcommand
// and every refused option ends the run with exit 126 and one line on stderr
// that names the refused word, quoted and cut to 64 bytes.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-exec to the standard packages it
// imports.
package main
