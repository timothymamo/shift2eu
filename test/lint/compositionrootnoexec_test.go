package lint_test

import "testing"

// The group composition-root-no-exec (compositionRootTags in
// compositionroot_test.go) is forbidigo's group for the calls the composition
// root cmd/shift2eu may not make (issue #111): it starts no process, replaces
// none, makes no raw system call, loads no library whose code would run, takes no
// pointer apart for a system call, and starts no program at all. It has no
// exception: no linters.exclusions.rules rule narrows on one of its entries'
// msgs, which TestCompositionRootExclusions and compositionRootRuleProblems
// check, so the set of names its entries refuse is the only thing that holds it.
//
// TestCompositionRootForbidigoPatterns pins those names by group, so a name
// cannot leave the group unseen; this file pins them entry by entry, so a name
// cannot move from one entry's pattern into another's either, and the pattern
// that must refuse each name is named. The group holds for every file under
// cmd/shift2eu/, _test.go files included.

// The msgs of the group's entries, in the order .golangci.yml holds them, each
// the name of one entry: a msg alone names none of them here, as every entry's
// msg differs, but the pkg a pin matches is what tells two entries with one msg
// apart in a group that has them (composition-root-env).
const (
	compositionRootNoExecOSMsg         = "composition-root-no-exec: a composition root starts no process and hands over no environment, where credentials live; take what it needs as a flag or a named variable"
	compositionRootNoExecSyscallMsg    = "composition-root-no-exec: a composition root starts or replaces no process and hands over no environment, where credentials live"
	compositionRootNoExecWindowsMsg    = "composition-root-no-exec: a composition root starts no windows process, loads no library whose code would run and hands over no environment, where credentials live"
	compositionRootNoExecRawSyscallMsg = "composition-root-no-exec: a composition root makes no raw system call, which starts a process as well as syscall.Exec does"
	compositionRootNoExecUnsafeMsg     = "composition-root-no-exec: a composition root takes no pointer apart, which is how a raw system call is handed an address; package unsafe is refused as a whole"
	compositionRootNoExecExecMsg       = "composition-root-no-exec: a composition root runs no program, such as a cloud CLI; os/exec is refused as a whole"
)

// compositionRootNoExecEntries lists every entry of the group, named by its msg
// and the package its pkg matches, with the exact set of names its pattern must
// refuse: the os entry (a process start and the whole environment), the unix
// syscall entry (the process starts and replacements and the environment), the
// windows entry (the windows process starts and the library loads), the raw
// system calls and the SYS_* numbers they take, package unsafe as a whole, and
// package os/exec as a whole.
var compositionRootNoExecEntries = []forbidGroupEntry{
	{
		msg: compositionRootNoExecOSMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.StartProcess"},
			{"os", "os.Environ"},
		},
	},
	{
		msg: compositionRootNoExecSyscallMsg,
		pkg: "syscall",
		refuses: []rootName{
			{"syscall", "syscall.Exec"},
			{"syscall", "syscall.ForkExec"},
			{"syscall", "syscall.StartProcess"},
			{"syscall", "syscall.Environ"},
		},
	},
	{
		msg: compositionRootNoExecWindowsMsg,
		pkg: "syscall",
		refuses: []rootName{
			{"syscall", "syscall.CreateProcess"},
			{"syscall", "syscall.CreateProcessAsUser"},
			{"syscall", "syscall.GetEnvironmentStrings"},
			{"syscall", "syscall.LoadDLL"},
			{"syscall", "syscall.MustLoadDLL"},
			{"syscall", "syscall.NewLazyDLL"},
			{"syscall", "syscall.LoadLibrary"},
			{"syscall", "syscall.GetProcAddress"},
		},
	},
	{
		msg: compositionRootNoExecRawSyscallMsg,
		pkg: "syscall",
		refuses: []rootName{
			{"syscall", "syscall.Syscall"},
			{"syscall", "syscall.Syscall6"},
			{"syscall", "syscall.Syscall9"},
			{"syscall", "syscall.Syscall12"},
			{"syscall", "syscall.Syscall15"},
			{"syscall", "syscall.Syscall18"},
			{"syscall", "syscall.SyscallN"},
			{"syscall", "syscall.RawSyscall"},
			{"syscall", "syscall.RawSyscall6"},
			{"syscall", "syscall.AllThreadsSyscall"},
			{"syscall", "syscall.AllThreadsSyscall6"},
			{"syscall", "syscall.SYS_EXECVE"},
			{"syscall", "syscall.SYS_CLONE"},
		},
	},
	{
		msg: compositionRootNoExecUnsafeMsg,
		pkg: "unsafe",
		refuses: []rootName{
			{"unsafe", "unsafe.Pointer"},
			{"unsafe", "unsafe.Sizeof"},
		},
	},
	{
		msg: compositionRootNoExecExecMsg,
		pkg: "os/exec",
		refuses: []rootName{
			{"os/exec", "exec.Command"},
			{"os/exec", "exec.CommandContext"},
			{"os/exec", "exec.LookPath"},
			{"os/exec", "exec.Cmd"},
		},
	},
}

// TestCompositionRootNoExecEntryPatterns checks the entries of the
// composition-root-no-exec group one by one: the entry each pin names refuses
// every name the pin lists and no other listed name, no entry refuses a name
// noExecAllowed lists, and every entry of the group is pinned. Without it a name
// could be moved from one entry's pattern into another's - os.StartProcess into
// the syscall entry, syscall.ForkExec into the windows entry, a raw system call
// into the unix entry - and lint would stay green for that pattern while the
// entry the name left refuses less than its msg says.
func TestCompositionRootNoExecEntryPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupEntries(t, "composition-root-no-exec", compositionRootNoExecEntries, noExecAllowed)
}
