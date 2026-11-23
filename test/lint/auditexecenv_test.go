package lint_test

import "testing"

// The group audit-exec-env (auditExecEnvTag in exclusions_test.go), pinned by
// TestForbidigoExclusionsDropOneGroup with the two rules that scope it, is
// forbidigo's group for the calls the audit's command wrapper, ci/auditexec, may
// not make (issue #277, ADR 0040's model-job amendment). depguard's audit-exec
// allows os and syscall there, which the wrapper exits with and execs through;
// os also reads and hands over the environment, where credentials live (CLAUDE.md
// rule 3), locates a home, config, cache or temp directory from it, and starts a
// process, and syscall reaches the same environment and process starts one call
// lower.
//
// The wrapper's one way out is syscall.Exec of a listed command with an
// environment it builds itself, the one call ci/auditexec/main.go passes to run;
// that name has an entry of its own, because the ci/auditexec/main.go rule drops
// its msg in that file alone. Everything else is refused: another way to read a
// variable or the whole environment, every process start through os or syscall,
// the windows library loads, and the raw system calls an exec is built of.
//
// Scope: every file under ci/auditexec/, _test.go files included, as the group's
// two linters.exclusions.rules rules say and TestForbidigoExclusionsDropOneGroup
// checks.

// auditExecEnvRefused lists every name the group must refuse, as forbidigo's
// analyze-types matching renders it: a package, the type the call is made on, and
// the function. A name is refused when an entry of the group matches its pkg and
// its rendered name. The refusal names the entry that must refuse it:
// auditExecExecMsg for syscall.Exec, the exception entry the ci/auditexec/main.go
// rule narrows on, and forbidGroupAnyEntry for the rest, which none of the
// group's exceptions may refuse.
var auditExecEnvRefused = []forbidGroupRefusal{
	{rootName{"os", "os.Getenv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.LookupEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.ExpandEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Environ"}, forbidGroupAnyEntry},
	{rootName{"os", "os.StartProcess"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserHomeDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserConfigDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserCacheDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.TempDir"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Getenv"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.GetEnvironmentVariable"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.GetEnvironmentStrings"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Environ"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Exec"}, auditExecExecMsg},
	{rootName{"syscall", "syscall.ForkExec"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.StartProcess"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.CreateProcess"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.CreateProcessAsUser"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.LoadDLL"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.MustLoadDLL"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.NewLazyDLL"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.LoadLibrary"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.GetProcAddress"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall6"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall9"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall12"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall15"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.Syscall18"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.SyscallN"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.RawSyscall"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.RawSyscall6"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.AllThreadsSyscall"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.AllThreadsSyscall6"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.SYS_EXECVE"}, forbidGroupAnyEntry},
	{rootName{"syscall", "syscall.SYS_CLONE"}, forbidGroupAnyEntry},
}

// auditExecEnvExceptions lists the group's exception entries: the one whose msg
// the ci/auditexec/main.go rule quotes, and the one name it may refuse. It may
// refuse syscall.Exec and no other, because that rule drops its whole msg in that
// file, and a name it refused as well would pass there too.
var auditExecEnvExceptions = []forbidGroupException{
	{auditExecExecMsg, rootName{"syscall", "syscall.Exec"}},
}

// auditExecEnvAllowed are names no entry of the group may refuse: the os calls
// the wrapper exits and writes with, its working directory (which
// tests-no-process-state's Chdir entry, not this group, holds), the signals and
// the process names it may read, the names that only extend a refused one
// (os.Getenvs, os.LookupEnvs, os.ExpandEnvs, os.Environments,
// os.StartProcesses, os.UserHomeDirs, os.UserConfigDirs, os.UserCacheDirs,
// os.TempDirs, syscall.Getenvs, syscall.GetEnvironmentVariables,
// syscall.Environments, syscall.StartProcesses, syscall.Execve,
// syscall.CreateProcessX, syscall.Syscalls, syscall.RawSyscalls,
// syscall.AllThreadsSyscalls, syscall.SYSCTL), and a package named os or syscall
// elsewhere.
var auditExecEnvAllowed = []rootName{
	{"os", "os.Args"},
	{"os", "os.Chdir"},
	{"os", "os.Exit"},
	{"os", "os.File"},
	{"os", "os.Interrupt"},
	{"os", "os.O_APPEND"},
	{"os", "os.O_CREATE"},
	{"os", "os.O_WRONLY"},
	{"os", "os.OpenFile"},
	{"os", "os.ReadFile"},
	{"os", "os.Stderr"},
	{"os", "os.Stdout"},
	{"os", "os.WriteFile"},
	{"os", "os.Getenvs"},
	{"os", "os.LookupEnvs"},
	{"os", "os.ExpandEnvs"},
	{"os", "os.Environments"},
	{"os", "os.StartProcesses"},
	{"os", "os.UserHomeDirs"},
	{"os", "os.UserConfigDirs"},
	{"os", "os.UserCacheDirs"},
	{"os", "os.TempDirs"},
	{"syscall", "syscall.SIGTERM"},
	{"syscall", "syscall.SIGHUP"},
	{"syscall", "syscall.Kill"},
	{"syscall", "syscall.Getpid"},
	{"syscall", "syscall.Wait4"},
	{"syscall", "syscall.Getenvs"},
	{"syscall", "syscall.GetEnvironmentVariables"},
	{"syscall", "syscall.Environments"},
	{"syscall", "syscall.StartProcesses"},
	{"syscall", "syscall.Execve"},
	{"syscall", "syscall.CreateProcessX"},
	{"syscall", "syscall.Syscalls"},
	{"syscall", "syscall.RawSyscalls"},
	{"syscall", "syscall.AllThreadsSyscalls"},
	{"syscall", "syscall.SYSCTL"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros", "os.LookupEnv"},
	{"example.com/othersyscall", "syscall.Exec"},
	{"example.com/othersyscall", "syscall.Getenv"},
}

// TestAuditExecEnvForbidigoPatterns checks the entries of the audit-exec-env
// group: every name the wrapper may not use is refused by the entry its refusal
// names, syscall.Exec's exception entry refuses that name and no other, no name it
// may use is refused by an entry, and every entry refuses a listed name. Without
// it, syscall.ForkExec or os.Environ could be moved into the syscall.Exec
// exception entry by widening that entry's pattern, and lint would stay green,
// while the ci/auditexec/main.go rule, which narrows on its msg, would let either
// through in ci/auditexec/main.go.
func TestAuditExecEnvForbidigoPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupPatterns(t, auditExecEnvTag, auditExecEnvRefused, auditExecEnvExceptions, auditExecEnvAllowed)
}
