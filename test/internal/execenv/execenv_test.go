package execenv_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/timothymamo/shift2eu/test/internal/execenv"
)

// These tests pin the two rules of issue #195 on probe sources under testdata
// (their .go.txt names, not .go, keep the go command, golangci-lint and
// test/lint's own scan of every Go file under test/ away from them, and keep
// the environ probes from tripping that scan). Each probe states one way an
// e2e test could hand a child the whole environment of the test process, where
// the agents' keys and tokens live (CLAUDE.md rule 3), or read it without a
// child. test/lint runs the checkers over the repository in
// TestE2ETestsSetExecEnv and TestTestTreeNamesNoProcEnviron.
//
// The rule fails closed: a command the checker cannot follow, a use of it it
// cannot vouch for, and an import it cannot name are findings too. Each probe
// below is one such shape, and the accepted ones are the shapes the tier really
// writes.
//
// A finding's line is the line a reader fixes: the Run, Start, Output or
// CombinedOutput call for CheckExecEnv, the construction, type, assignment or
// import that hands the command on, and the literal for CheckEnvironLiterals.
// Its message is not pinned, only that it says something, so the wording can
// change without touching a probe.
//
// A shape yields at most one finding per line, at the earliest statement a
// reader fixes, even where two rules of the checker meet on it: an exec.Cmd
// composite literal with no Env is refused once as a literal and once as the
// command type it names, and the probe expects the one finding. An Env assigned
// a value that inherits the environment is a finding at the assignment and
// leaves the command's Env unset, so a run after it is a finding too, on its own
// line.

// noExemption refuses no command name, the exemption function of a caller that
// lets nothing inherit the environment.
func noExemption(string, string, []string) bool { return false }

// nameOnlyExemption admits every command named go, whatever its subcommand and
// flags, so a probe shows that a subcommand the source does not write as a
// literal is exempt by nothing.
func nameOnlyExemption(name, _ string, _ []string) bool { return name == "go" }

// toolchainFlags is the allow-list of the go command's flags an exempt call may
// carry (P4), the first three of test/lint's e2eToolchainFlags: after the
// subcommand, every literal argument that starts with "-" must be one of these,
// or the exemption is lost. Its fourth, -ldflags, test/lint admits for a value
// of -X stamps alone, a rule of its own that no probe here needs. A flag the go
// command gains later is refused until it is added here on purpose.
var toolchainFlags = []string{"-trimpath", "-o", "-deps"}

// toolchainExemption admits the go toolchain's two commands whose calls in the
// e2e tier inherit the environment because the go command needs it (`go build`
// and `go list`), keyed on the name and the subcommand after it, and only while
// every literal argument after the subcommand that starts with "-" is one of
// toolchainFlags, as test/lint's rule is. `go run` and `go env` print or hand on
// that environment, so they are no part of it, and neither is a name or a
// subcommand the source builds at run time.
func toolchainExemption(name, sub string, flags []string) bool {
	if name != "go" || (sub != "build" && sub != "list") {
		return false
	}
	for _, flag := range flags {
		if strings.HasPrefix(flag, "-") && !slices.Contains(toolchainFlags, flag) {
			return false
		}
	}
	return true
}

func TestCheckExecEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		probe  string
		exempt func(name, sub string, flags []string) bool
		want   []execenv.Finding
		// wantErr is set for a probe that does not parse: both checkers read
		// the file as Go and return the parse error.
		wantErr bool
	}{
		{
			name:  "Run, Start, Output and CombinedOutput are each refused on a command with no Env",
			probe: "methods.go.txt",
			want:  find("methods.go.txt", 7, 10, 13, 16),
		},
		{
			name:  "a command built by CommandContext is refused as a command built by Command is",
			probe: "commandcontext.go.txt",
			want:  find("commandcontext.go.txt", 10),
		},
		{
			name:  "an Env assigned after the call comes too late",
			probe: "env_after.go.txt",
			want:  find("env_after.go.txt", 7),
		},
		{
			name:  "a chained Command call is refused, though it binds no name to hold an Env",
			probe: "chained.go.txt",
			want:  find("chained.go.txt", 6),
		},
		{
			// No site chains an exempt command to its run method, and the
			// exemption no longer reaches a chained run.
			name:   "an exempt command chained to its run method is refused",
			probe:  "chained_exempt.go.txt",
			exempt: toolchainExemption,
			want:   find("chained_exempt.go.txt", 8),
		},
		{
			name:  "an Env assigned before the call is accepted",
			probe: "env_set.go.txt",
		},
		{
			// The e2e tier's runs assign Dir, Stdout and Stderr before Env
			// (inventory_guide_test.go's runJQ), which hands the checker no
			// reason to doubt the command.
			name:  "fields other than Env assigned before Env are accepted",
			probe: "field_before_env.go.txt",
		},
		{
			// The exemption is keyed on the name and the subcommand, so it
			// admits both commands the e2e tests start with the whole
			// environment and nothing else the go command does.
			name:   "an exempt go build and go list are accepted with no Env",
			probe:  "toolchain.go.txt",
			exempt: toolchainExemption,
		},
		{
			name:  "an unexempted go command is refused, and CommandContext's first argument is its name",
			probe: "toolchain.go.txt",
			want:  find("toolchain.go.txt", 10, 15),
		},
		{
			name:   "a go subcommand the exemption does not name is refused",
			probe:  "subcommand_run.go.txt",
			exempt: toolchainExemption,
			want:   find("subcommand_run.go.txt", 7),
		},
		{
			name:   "go env is refused, the exemption naming build and list alone",
			probe:  "subcommand_env.go.txt",
			exempt: toolchainExemption,
			want:   find("subcommand_env.go.txt", 7),
		},
		{
			name:   "a go subcommand the source builds at run time is refused",
			probe:  "subcommand_variable.go.txt",
			exempt: toolchainExemption,
			want:   find("subcommand_variable.go.txt", 7),
		},
		{
			name:   "a go call with no subcommand is refused",
			probe:  "subcommand_missing.go.txt",
			exempt: toolchainExemption,
			want:   find("subcommand_missing.go.txt", 7),
		},
		{
			// The Path is the command the child runs, so a go build whose
			// Path now names something else is no go build: the exemption
			// covers the command the source wrote, not the one this call
			// starts.
			name:   "a Path assigned on an exempt command takes the exemption away",
			probe:  "path_reassigned.go.txt",
			exempt: toolchainExemption,
			want:   find("path_reassigned.go.txt", 8),
		},
		{
			name:   "Args assigned on an exempt command take the exemption away",
			probe:  "args_reassigned.go.txt",
			exempt: toolchainExemption,
			want:   find("args_reassigned.go.txt", 8),
		},
		{
			// A composite literal is a command the checker never saw built:
			// it states its own Path and, with no Env key or a nil one,
			// inherits the environment. Each literal is one finding, not one
			// for the literal and one for the exec.Cmd type it names.
			name:  "a composite literal of exec.Cmd with no Env is refused, with the value, with the pointer and with a nil Env key",
			probe: "composite.go.txt",
			want:  find("composite.go.txt", 6, 7, 8),
		},
		{
			name:  "a composite literal of exec.Cmd with an Env key is accepted",
			probe: "composite_env.go.txt",
		},
		{
			// No composite type holds a command (P2), so the slice and the
			// map are refused at their type expression, and an element
			// literal that writes an Env proves nothing about the type that
			// holds it.
			name:  "a slice or map of *exec.Cmd is refused, an elided-type element literal no finding of its own",
			probe: "composite_elided.go.txt",
			want:  find("composite_elided.go.txt", 10, 11, 12),
		},
		{
			// A zero value of exec.Cmd is never followed, so no Env is ever
			// assigned to it: the type expression itself is refused, in every
			// shape that names it.
			name:  "a value variable of exec.Cmd is refused",
			probe: "cmd_value_var.go.txt",
			want:  find("cmd_value_var.go.txt", 6),
		},
		{
			name:  "an array of exec.Cmd values is refused",
			probe: "cmd_value_array.go.txt",
			want:  find("cmd_value_array.go.txt", 6),
		},
		{
			name:  "a struct field of the exec.Cmd value type is refused",
			probe: "cmd_value_field.go.txt",
			want:  find("cmd_value_field.go.txt", 6),
		},
		{
			name:  "an embedded exec.Cmd value is refused",
			probe: "cmd_embedded.go.txt",
			want:  find("cmd_embedded.go.txt", 6),
		},
		{
			// The pointer is safe as a field, parameter or var, but as an
			// embedded field its methods are the struct's, and a struct value
			// cannot be followed.
			name:  "an embedded *exec.Cmd field is refused",
			probe: "cmd_embedded_pointer.go.txt",
			want:  find("cmd_embedded_pointer.go.txt", 6),
		},
		{
			name:  "an alias of exec.Cmd is refused",
			probe: "cmd_alias.go.txt",
			want:  find("cmd_alias.go.txt", 5),
		},
		{
			name:  "a type defined on exec.Cmd is refused",
			probe: "cmd_defined_type.go.txt",
			want:  find("cmd_defined_type.go.txt", 5),
		},
		{
			name:  "exec.Cmd as a type argument is refused",
			probe: "cmd_type_argument.go.txt",
			want:  find("cmd_type_argument.go.txt", 10),
		},
		{
			// P2 narrows the exception to a whole declared type: the pointer
			// is refused as the element or key of a composite type, on the
			// right of a type declaration and as a type argument, at the type
			// expression a reader fixes.
			name:  "a composite type holding *exec.Cmd, a type declaration on it and a type argument of it are refused",
			probe: "cmd_composite_types.go.txt",
			want:  find("cmd_composite_types.go.txt", 10, 11, 12, 13, 14, 16, 17, 24),
		},
		{
			// P6: the tier declares no generic function, and one would build a
			// command the check does not follow.
			name:  "a generic function in a file that imports os/exec is refused, at its declaration",
			probe: "generic.go.txt",
			want:  find("generic.go.txt", 9),
		},
		{
			// Reflection and unsafe reach a command's Env out of the check's
			// sight, so in a file that imports os/exec each import is a
			// finding at its import line, as a blank or dot import of os/exec
			// is.
			name:  "a file importing os/exec and reflect is refused at the reflect import",
			probe: "reflect_import.go.txt",
			want:  find("reflect_import.go.txt", 5),
		},
		{
			name:  "a file importing os/exec and unsafe is refused at the unsafe import",
			probe: "unsafe_import.go.txt",
			want:  find("unsafe_import.go.txt", 5),
		},
		{
			// One finding per line: a grouped import holding both is refused
			// at each import line, not once for the group.
			name:  "a grouped import holding both is refused at each import line",
			probe: "reflect_unsafe_import.go.txt",
			want:  find("reflect_unsafe_import.go.txt", 5, 6),
		},
		{
			// The rule is about commands, and this file builds none: it
			// imports no os/exec, so its reflection is nothing to refuse.
			name:  "a file importing reflect without os/exec is accepted",
			probe: "reflect_only.go.txt",
		},
		{
			// new(exec.Cmd) is one of the shapes the type rule covers, and it
			// is one finding, not one for the call and one for the type.
			name:  "new(exec.Cmd) is refused",
			probe: "new_cmd.go.txt",
			want:  find("new_cmd.go.txt", 6),
		},
		{
			// server_test.go's serverRun holds one, and startServerBinary
			// takes one: a nil pointer runs nothing, so a *exec.Cmd field,
			// parameter, result and var are all accepted.
			name:  "a *exec.Cmd field, parameter, result and var are accepted",
			probe: "pointer_cmd.go.txt",
		},
		{
			name:  "a blank import of os/exec is refused, not silently skipped",
			probe: "blank_import.go.txt",
			want:  find("blank_import.go.txt", 3),
		},
		{
			name:  "a dot import of os/exec is refused, not silently skipped",
			probe: "dot_import.go.txt",
			want:  find("dot_import.go.txt", 3),
		},
		{
			name:  "a command passed straight to a helper is refused at the construction",
			probe: "helper_argument.go.txt",
			want:  find("helper_argument.go.txt", 6),
		},
		{
			// The result type, the literal's type and the construction the
			// literal holds are three lines, each refused in its own right:
			// no composite type holds a command (P2).
			name:  "a command stored in a slice element is refused, the result type and the literal's type too",
			probe: "slice_element.go.txt",
			want:  find("slice_element.go.txt", 8, 9, 10),
		},
		{
			name:  "a command assigned to a struct field is refused at the construction",
			probe: "struct_field_target.go.txt",
			want:  find("struct_field_target.go.txt", 10),
		},
		{
			name:  "a command copied to another name before its Env is set is refused at the copy",
			probe: "alias.go.txt",
			want:  find("alias.go.txt", 7),
		},
		{
			name:  "a method value taken before Env is set is refused",
			probe: "method_value.go.txt",
			want:  find("method_value.go.txt", 7),
		},
		{
			name:  "a method expression called on a command before Env is set is refused",
			probe: "method_expression.go.txt",
			want:  find("method_expression.go.txt", 7),
		},
		{
			name:  "a closure that captures a command before Env is set is refused",
			probe: "closure_capture.go.txt",
			want:  find("closure_capture.go.txt", 7),
		},
		{
			name:  "a parenthesised receiver is unwrapped and refused",
			probe: "paren_receiver.go.txt",
			want:  find("paren_receiver.go.txt", 7),
		},
		{
			// The outer cmd is the command that runs, whatever an inner block
			// did to a name of its own.
			name:  "a shadowing command in an inner block sets no Env on the outer one",
			probe: "shadowing.go.txt",
			want:  find("shadowing.go.txt", 12),
		},
		{
			// A nil Env is the value that inherits the environment, so the
			// assignment is a finding of its own and sets no Env: the run
			// after it is refused too, at both the nil and the nil slice.
			name:  "Env assigned nil, or as a nil slice, is refused at the assignment and at the run",
			probe: "env_nil.go.txt",
			want:  find("env_nil.go.txt", 7, 8, 11, 12),
		},
		{
			name:  "Env assigned a nil variable is refused at the assignment and at the run",
			probe: "env_nil_variable.go.txt",
			want:  find("env_nil_variable.go.txt", 9, 10),
		},
		{
			// P1: every assignment to an Env field is proven, whatever the
			// receiver, a command the check follows or not, and whatever the
			// shape of the value: a dereference of the command, an alias, a
			// value copy, a parameter, a struct field, a type-switch variable,
			// a range statement, an unpaired call and a reslice one by one, and
			// an Env key of another struct's literal too.
			// A dereference of the command is the command: the Env the
			// dereference assigns at line 96, after the run method was taken,
			// is refused as the direct assignment would be.
			name:  "every Env assignment is refused unless its value is proven, whatever the receiver",
			probe: "env_receiver.go.txt",
			want:  find("env_receiver.go.txt", 15, 22, 28, 33, 43, 51, 58, 69, 75, 85, 96),
		},
		{
			name:  "Env assigned a dereferenced pointer to a nil slice is refused at the assignment and at the run",
			probe: "env_deref_new.go.txt",
			want:  find("env_deref_new.go.txt", 7, 8),
		},
		{
			// The same rule as an Env assignment, on a composite literal's
			// key: a nil variable there inherits the environment as a
			// missing key does.
			name:  "a composite literal whose Env key is a nil variable is refused",
			probe: "composite_env_variable.go.txt",
			want:  find("composite_env_variable.go.txt", 9),
		},
		{
			// The command's own Env is set and its run is accepted, so only a
			// rule about the address itself can refuse these two.
			name:  "the address of an Env field is refused wherever it appears",
			probe: "env_address.go.txt",
			want:  find("env_address.go.txt", 10),
		},
		{
			name:  "a pointer to an Env field is refused wherever it appears",
			probe: "env_address_var.go.txt",
			want:  find("env_address_var.go.txt", 10),
		},
		{
			// Run order can no longer differ from source order once the
			// command has gone somewhere the check cannot follow.
			name:  "an Env assigned after the command was deferred is refused",
			probe: "env_after_capture.go.txt",
			want:  find("env_after_capture.go.txt", 11),
		},
		{
			name:  "an Env assigned after the command was aliased is refused, an accepted value too",
			probe: "env_after_alias.go.txt",
			want:  find("env_after_alias.go.txt", 12),
		},
		{
			// The field assignment is a use of a captured command; the
			// assignment still counts as setting Env, the check following the
			// source, so the run after it is accepted.
			name:  "an Env assigned to a command a closure captured is refused",
			probe: "env_in_closure.go.txt",
			want:  find("env_in_closure.go.txt", 10),
		},
		{
			// The values the e2e tier's sites assign, which the check must
			// prove are not nil: an empty composite literal, a make call and
			// an append whose first argument is an accepted literal.
			name:  "an Env that is an empty literal, a make or an append of a literal is accepted",
			probe: "env_accepted_forms.go.txt",
		},
		{
			// assess_test.go's emptyEnv: a call to a function of the same file
			// whose return expression is an accepted literal.
			name:  "an Env that a helper of the same file returns as a literal is accepted",
			probe: "env_helper.go.txt",
		},
		{
			// P5: the declaration is what a reader fixes, and a helper that
			// could hand back a zero value proves nothing, so the assignment
			// that reads it and the run after it are refused as an unproven Env
			// and an unset one. Such a helper is refused for a named result
			// alone (namedEnv) as well as for a defer (recoveredEnv).
			name:  "a helper with a named result or a defer proves no Env and is refused at its declaration",
			probe: "env_helper_shapes.go.txt",
			want:  find("env_helper_shapes.go.txt", 12, 17, 24, 25, 28, 29, 35, 41, 42),
		},
		{
			// The accepted counterparts of env_receiver.go.txt: the same
			// receivers with the values the e2e tier writes, and a type
			// assertion, whose type is one of the places a *exec.Cmd may be
			// declared (P2).
			name:  "a proven Env assigned through any receiver is accepted",
			probe: "env_receiver_accepted.go.txt",
		},
		{
			// P3: a selector Command or CommandContext that is no call names a
			// function the check cannot follow, which hands back a fresh command
			// with a nil Env wherever it is called.
			name:  "exec.Command and exec.CommandContext as a function value are refused",
			probe: "exec_function_value.go.txt",
			want:  find("exec_function_value.go.txt", 10, 15, 18),
		},
		{
			// The accepted gap: the checker follows the source, not the
			// value, so an Env assigned anywhere in the function counts.
			name:  "an Env assigned in a branch that never runs is accepted, the check following the source",
			probe: "env_if_false.go.txt",
		},
		{
			// The shape server_test.go's startServerBinary has: Env is set
			// first, and the command is then stored, captured and started.
			name:  "a command used after its Env is set is accepted, stored in a struct and captured too",
			probe: "struct_after_env.go.txt",
		},
		{
			name:  "the Env of another command in the same function does not count",
			probe: "other_env.go.txt",
			want:  find("other_env.go.txt", 10),
		},
		{
			name:  "a command whose name is no literal is refused, whatever the exemption admits",
			probe: "name_variable.go.txt",
			want:  find("name_variable.go.txt", 7),
		},
		{
			// exec.Command hands back a fresh *exec.Cmd, with a nil Env, so an
			// Env assigned before the variable was rebuilt says nothing about
			// the command that runs. The rebuild assigns to a name that exists
			// already, which declares nothing, so it is refused at the
			// construction, the run after it being no command the check can
			// follow.
			name:  "an Env assigned before the command was rebuilt is refused, at the rebuild",
			probe: "reassigned.go.txt",
			want:  find("reassigned.go.txt", 9),
		},
		{
			// test/e2e is a multi-file package, and the check resolves one
			// file on its own: the target of this assignment names no object,
			// so the construction is one the check cannot follow.
			name:  "a command assigned to a package variable another file declares is refused",
			probe: "package_var_other_file.go.txt",
			want:  find("package_var_other_file.go.txt", 8),
		},
		{
			// A declaration cannot be seen by the closure written above it, so
			// only a declaration counts as a binding and this assignment to a
			// name that exists already is refused.
			name:  "a command bound to a name a closure above it captured is refused at the binding",
			probe: "closure_before_binding.go.txt",
			want:  find("closure_before_binding.go.txt", 13),
		},
		{
			name:  "a command bound below the loop that runs it is refused at the binding",
			probe: "loop_before_binding.go.txt",
			want:  find("loop_before_binding.go.txt", 19),
		},
		{
			// A followed name that is assigned something else holds no
			// command the check built, so the rebinding is refused.
			name:  "a followed name rebound to a command the file did not build is refused at the rebinding",
			probe: "rebind_non_command.go.txt",
			want:  find("rebind_non_command.go.txt", 11),
		},
		{
			name:  "a function literal in a package-level var is a function of its own and is checked",
			probe: "funclit_var.go.txt",
			want:  find("funclit_var.go.txt", 7),
		},
		{
			name:  "a function literal nested in a function body is checked as a function of its own",
			probe: "funclit_nested.go.txt",
			want:  find("funclit_nested.go.txt", 8),
		},
		{
			// The exemption is keyed on the name and the subcommand, both
			// literals, so a call whose subcommand the source builds at run
			// time is exempt by no name, however the caller's function is
			// written.
			name:   "an exemption that admits on the name alone exempts no non-literal subcommand",
			probe:  "name_only_exemption.go.txt",
			exempt: nameOnlyExemption,
			want:   find("name_only_exemption.go.txt", 10),
		},
		{
			name:   "a parenthesised exempt command is the same command and is accepted",
			probe:  "paren_exempt.go.txt",
			exempt: toolchainExemption,
		},
		{
			// The shapes test/e2e runs: the flags it passes are none of the
			// ones that spoil the exemption, and a positional argument the
			// source builds at run time (an output path, a package) is no
			// reason to lose it. A non-literal argument holding -toolexec is
			// beyond a rule of literals, and review holds it.
			name:   "an exempt go build and go list keep the exemption with non-literal positional arguments",
			probe:  "toolchain_arguments.go.txt",
			exempt: toolchainExemption,
		},
		{
			// Each of these hands the go command's environment to a program
			// the arguments name, so the exemption is lost and the run is
			// refused as any command with a nil Env is.
			name:   "a -toolexec argument, apart or with =, takes the exemption away",
			probe:  "toolchain_toolexec.go.txt",
			exempt: toolchainExemption,
			want:   find("toolchain_toolexec.go.txt", 9, 14),
		},
		{
			name:   "a -exec argument, apart or with =, takes the exemption away",
			probe:  "toolchain_exec.go.txt",
			exempt: toolchainExemption,
			want:   find("toolchain_exec.go.txt", 9, 14),
		},
		{
			name:   "a -overlay argument, apart or with =, takes the exemption away",
			probe:  "toolchain_overlay.go.txt",
			exempt: toolchainExemption,
			want:   find("toolchain_overlay.go.txt", 9, 14),
		},
		{
			name:   "a spread tail takes the exemption away",
			probe:  "toolchain_spread.go.txt",
			exempt: toolchainExemption,
			want:   find("toolchain_spread.go.txt", 9),
		},
		{
			// P4: the exemption is an allow-list, so any flag it does not name
			// takes it away: a -ldflags that names another linker and its
			// -extld, apart or with =, a -C that runs the go command elsewhere
			// and a -pgo that reads a profile.
			name:   "a flag the allow-list does not name takes the exemption away",
			probe:  "toolchain_flags.go.txt",
			exempt: toolchainExemption,
			want:   find("toolchain_flags.go.txt", 13, 18, 23, 28),
		},
		{
			name:    "a source that does not parse is an error, not a finding",
			probe:   "syntaxerror.go.txt",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join("testdata", tc.probe))
			if err != nil {
				t.Fatalf("read the probe: %v", err)
			}

			exempt := tc.exempt
			if exempt == nil {
				exempt = noExemption
			}

			findings, err := execenv.CheckExecEnv("testdata/"+tc.probe, src, exempt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("CheckExecEnv(%s) = %v, want an error", tc.probe, findings)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckExecEnv(%s): %v", tc.probe, err)
			}

			for _, f := range findings {
				if f.Msg == "" {
					t.Errorf("finding %s:%d gives no message, want one that says what to fix", f.File, f.Line)
				}
			}

			// EquateEmpty: a probe with nothing to refuse may come back as nil
			// or as an empty slice.
			if diff := cmp.Diff(tc.want, findings, cmpopts.IgnoreFields(execenv.Finding{}, "Msg"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("findings for %s (-want +got):\n%s", tc.probe, diff)
			}
		})
	}
}

func TestCheckEnvironLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		probe string
		// tier is the part of the tree the file is read in: TierTest is the
		// zero value, so a case that names none is held to the whole-value
		// rule of the rest of test/.
		tier    execenv.Tier
		want    []execenv.Finding
		wantErr bool
	}{
		{
			name:  "self, a pid and thread-self are each refused",
			probe: "environ_literals.go.txt",
			want:  find("environ_literals.go.txt", 5, 6, 7),
		},
		{
			name:  "another /proc path of the same process is accepted",
			probe: "environ_status.go.txt",
		},
		{
			name:  "the path in a comment is no literal and is accepted",
			probe: "environ_comment.go.txt",
		},
		{
			// The widened rule reads path.Clean(value), so a path that only
			// reaches the same file through a doubled slash, a dot segment,
			// a deeper /proc entry or no leading slash is refused too, and a
			// raw string is read as the quoted one.
			name:  "the whole-value rule reads a cleaned path, so a nested, doubled, dotted or relative one is refused",
			probe: "environ_tiers.go.txt",
			want:  find("environ_tiers.go.txt", 5, 10, 11, 12, 13, 14),
		},
		{
			// The e2e tier has no fixture literals, so it holds every literal
			// to the segment: no path into /proc and none into /dev/fd, /proc
			// itself and /proc/self/status included.
			name:  "the e2e tier refuses any literal with an environment path segment",
			probe: "environ_tiers.go.txt",
			tier:  execenv.TierE2E,
			want:  find("environ_tiers.go.txt", 5, 6, 7, 8, 10, 11, 12, 13, 14),
		},
		{
			name:    "a source that does not parse is an error, not a finding",
			probe:   "syntaxerror.go.txt",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join("testdata", tc.probe))
			if err != nil {
				t.Fatalf("read the probe: %v", err)
			}

			findings, err := execenv.CheckEnvironLiterals("testdata/"+tc.probe, src, tc.tier)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("CheckEnvironLiterals(%s) = %v, want an error", tc.probe, findings)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckEnvironLiterals(%s): %v", tc.probe, err)
			}

			for _, f := range findings {
				if f.Msg == "" {
					t.Errorf("finding %s:%d gives no message, want one that says what to fix", f.File, f.Line)
				}
			}

			if diff := cmp.Diff(tc.want, findings, cmpopts.IgnoreFields(execenv.Finding{}, "Msg"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("findings for %s in tier %v (-want +got):\n%s", tc.probe, tc.tier, diff)
			}
		})
	}
}

// find returns the findings a probe file is expected to produce, one per line,
// in line order.
func find(probe string, lines ...int) []execenv.Finding {
	findings := make([]execenv.Finding, 0, len(lines))
	for _, line := range lines {
		findings = append(findings, execenv.Finding{File: "testdata/" + probe, Line: line})
	}
	return findings
}
