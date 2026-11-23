// Command auditexec is the command wrapper of the scheduled security audit,
// installed in the model job as /srv/audit/bin/audit-exec (ADR 0040's
// model-job amendment, issue #277, PRD #175 milestone 2): the one program
// the audit's model may run through Bash. Its rules, below, are the current
// ones; how each came about is in ADR 0040.
//
// # Command map and environment
//
// `audit-exec <name> <args...>` maps name to a fixed absolute path, never
// through PATH, changes into its fixed working directory /srv/audit/work
// (os.Chdir; a change that fails exits 127, as a failed exec does) and
// replaces itself with that program (syscall.Exec). The program's
// environment is exactly the wrapper's own allow list: nothing of the
// caller's is inherited.
//
// # git
//
// git runs as `/usr/bin/git --no-pager -C /srv/audit/checkout <subcommand>
// ...`, with one of the read-only subcommands the ADR lists as the first word
// after git. On its line the wrapper refuses:
//
//   - every long option that names another configuration, git directory,
//     work tree, namespace, attribute source, program or output file, or that
//     changes how git reads the repository (--super-prefix, --attr-source,
//     --bare and --literal-pathspecs among them), alone, with "=" and
//     abbreviated;
//   - --no-index. git diff of two absolute paths outside the checkout runs
//     no-index on its own; the path rule, below, is what keeps both inside
//     the allowed roots, and the explicit refusal is for clarity;
//   - --show-signature, and any word that could ask for a signature's
//     verification (%G with or without git's +, - or space modifier,
//     %(signature, and on a for-each-ref line any word holding signature),
//     wherever it sits, so a grep pattern holding %G is refused too;
//   - a single-dash word that holds -c, -O or -C anywhere in its cluster. A
//     caller's -C would override the wrapper's own and run git in the work
//     area, where a planted .git/config runs a program, so it is refused in
//     every form, git log's copy detection included.
//
// # jq, wc, sha256sum, sort and uniq
//
//   - jq never reaches its module system or a program file: a word holding
//     import, include, modulemeta or get_search_list, -L or -f in any
//     single-dash word, and --library-path or --from-file (alone, with "="
//     and abbreviated) are refused, so the audit's filters come on the line.
//   - No command reads a list of paths from a file the path rule never sees:
//     wc's --files0-from and sha256sum's -c and --check are refused in every
//     form, a cluster included.
//   - sort takes only an allow list of options, which order and compare lines
//     (-k and -t with their values among them), so it never runs a program,
//     reads a file it was not given or writes one.
//   - uniq takes one operand at most, so its OUT never writes a file.
//
// # Word and path rules
//
// Before any command's own rules, the word rules read every word of every
// line: a word whose "/"-separated elements include "..", and a word starting
// with "-" that holds ".." anywhere (-S..), is refused before anything is
// cleaned. A glob the CLI's deny rule could not see, or a link such as
// /dev/fd followed by "..", is so refused once the shell has expanded it; a
// git range (HEAD~3..HEAD) holds no ".." element and passes.
//
// Then every path word of every line must be /srv/audit/checkout,
// /srv/audit/checks or /srv/audit/work, or lie under one of them. A path word
// is:
//
//   - a non-option word holding "/";
//   - every word after "--";
//   - an option's value after its "=" (--format=%h/%s holds %h/%s);
//   - a word starting with "--" that holds a "/" before any "=", whole;
//   - in a single-dash word that holds "/", its suffix from the first "/",
//     which must be an allowed absolute path. No tool's option cluster is
//     modelled, since git reads digits inside one as -NUM (git grep
//     -n5f/tmp/x). "-" alone is stdin.
//
// A relative path word is read from the checkout on a git line and from the
// work directory on any other. The wrapper is defence in depth: what the
// shell opens itself, a redirection, it never sees, and the sandbox is the
// boundary.
//
// # Exit codes
//
// Every other name (a path given as the name included), every other git
// subcommand, every refused option and every path word outside the roots ends
// the run with exit 126, having changed and run nothing, and one line on
// stderr that names the refused word, quoted and cut to 64 bytes. A working
// directory change or an exec that fails exits 127.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-exec to the standard packages it
// imports.
package main
