// Command auditexec is the command wrapper of the scheduled security audit,
// installed in the model job as /srv/audit/bin/audit-exec (ADR 0040's
// milestone-2 amendment, issue #277, PRD #175 milestone 2): the one program
// the audit's model may run through Bash.
//
// `audit-exec <name> <args...>` maps name to a fixed absolute path, never
// through PATH, changes into its fixed working directory /srv/audit/work
// (os.Chdir; a change that fails exits 127, as a failed exec does) and
// replaces itself with that program (syscall.Exec), with an environment of
// exactly its own allow list: nothing of the caller's is inherited. git runs
// as `/usr/bin/git --no-pager -C /srv/audit/checkout <subcommand> ...` with
// one of the read-only subcommands the ADR lists, and never with an option
// that names another configuration, git directory, work tree, namespace,
// attribute source, program or output file (--super-prefix, --attr-source,
// --bare and --literal-pathspecs among them), nor --no-index, nor
// --show-signature, nor any word that could
// ask for a signature's verification (%G with or without git's +, - or space
// modifier, %(signature, and on a for-each-ref line any word holding
// signature), wherever it sits on the line, so a grep pattern holding %G is
// refused too, nor with a single-dash word that holds -c, -O or -C anywhere in
// its cluster: a caller's -C, separate, attached or clustered, would override
// the wrapper's own and run git in the work area, where a planted .git/config
// runs a program, so it is refused in every form, git log's copy detection
// included (issue #277 round H). jq never reaches its module system or a
// program file: a jq word holding import or include, -L or -f in any
// single-dash word, and --library-path or --from-file (alone, with "=" and
// abbreviated) are refused, so the audit's filters come on the line. No
// command reads a list of paths from a file the path rule never sees: wc's
// --files0-from and sha256sum's -c and --check are refused in every form, a
// cluster included (issue #277 round I). sort takes only an allow list of
// options, which
// order and compare lines (-k and -t with their values among them), so it never
// runs a program, reads a file it was not given or writes one; uniq takes one
// operand at most, so its OUT never writes a file. On every command's line,
// every path word is held to an allow list, /srv/audit/checkout,
// /srv/audit/checks and /srv/audit/work and what lies under them (the
// wrapper's path rule, issue #277 rounds F and G): a path word is a
// non-option word holding "/" or that is "..", every word after "--", an
// option's value after its "=", the rest of a single-dash word after its
// first letter when that rest holds "/" ("-" alone is stdin). A relative path word is read from the
// checkout on a git line and from the work directory on any other; a word
// holding a ".." element is refused before anything is cleaned, so a glob
// the CLI's deny rule could not see, or a link such as /dev/fd followed by
// "..", is refused once the shell has expanded it. The wrapper is defence in
// depth: what the shell opens itself, a redirection, it never sees, and the
// sandbox is the boundary. Every other name, a path given as the name
// included, every other git subcommand, every refused option and every path
// word outside the roots ends the run with exit 126, having changed and run
// nothing, and one line on stderr that names the refused word, quoted and cut
// to 64 bytes.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-exec to the standard packages it
// imports.
package main
