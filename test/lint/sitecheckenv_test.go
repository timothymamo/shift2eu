package lint_test

import "testing"

// The group site-check-env (siteCheckEnvTag in exclusions_test.go), pinned by
// TestForbidigoExclusionsDropOneGroup with the one rule that scopes it, is
// forbidigo's group for the calls the live site check, ci/sitecheck, may not
// make (PRD #297 milestone 3). depguard's site-check-main allows os in the entry
// point, which reads the files its flags name and exits; os also reads the
// environment, where credentials live (CLAUDE.md rule 3), locates a home,
// config, cache or temp directory from it, and starts a process. Its sibling
// rule, site-check-library, leaves os, os/exec, syscall and unsafe out of the
// library altogether, so the group's os entry holds the entry point and its
// tests.
//
// The group has no exception: no linters.exclusions.rules rule narrows on one of
// its entries' msgs, its one rule dropping the group by tag outside ci/sitecheck/
// (TestForbidigoExclusionsDropOneGroup), so the set of names its entries refuse is
// the only thing that holds it. Its syscall entry lets the entry point name
// syscall.SIGTERM, the signal an interrupt cancels the run with, and refuses
// every other syscall name it lists.
//
// Scope: every file under ci/sitecheck/, _test.go files included, as the group's
// linters.exclusions.rules rule says and TestForbidigoExclusionsDropOneGroup
// checks.

// The msgs of the group's two entries, which name them here: a pin names an
// entry by its msg and the pkg it matches, as no linters.exclusions.rules rule
// narrows on either msg.
const (
	siteCheckEnvOSMsg      = "site-check-env: the live site check reads no environment variable, locates no home, config, cache or temp directory and starts no process; it reads the files its flags name and fetches the site (CLAUDE.md rule 3)"
	siteCheckEnvSyscallMsg = "site-check-env: the live site check reads no environment variable, starts or replaces no process, loads no library and makes no raw system call through syscall; its entry point names only syscall.SIGTERM (CLAUDE.md rule 3)"
)

// siteCheckEnvEntries lists every entry of the group, named by its msg and the
// package its pkg matches, with the exact set of names its pattern must refuse:
// the os reads of the whole environment, the process start through os, the four
// lookups that locate a directory from an unnamed variable, and the syscall
// reads, process starts and replacements, windows process starts and library
// loads, and raw system calls. The syscall entry's pattern admits every
// syscall.SYS_* number as well (SYS_\w+); two of them pin that, as
// composition-root-no-exec's entry does, and each of the pattern's other
// alternatives is pinned by name.
var siteCheckEnvEntries = []forbidGroupEntry{
	{
		msg: siteCheckEnvOSMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.Getenv"},
			{"os", "os.LookupEnv"},
			{"os", "os.ExpandEnv"},
			{"os", "os.Environ"},
			{"os", "os.StartProcess"},
			{"os", "os.UserHomeDir"},
			{"os", "os.UserConfigDir"},
			{"os", "os.UserCacheDir"},
			{"os", "os.TempDir"},
		},
	},
	{
		msg: siteCheckEnvSyscallMsg,
		pkg: "syscall",
		refuses: []rootName{
			{"syscall", "syscall.Getenv"},
			{"syscall", "syscall.GetEnvironmentVariable"},
			{"syscall", "syscall.Environ"},
			{"syscall", "syscall.Exec"},
			{"syscall", "syscall.ForkExec"},
			{"syscall", "syscall.StartProcess"},
			{"syscall", "syscall.CreateProcess"},
			{"syscall", "syscall.CreateProcessAsUser"},
			{"syscall", "syscall.GetEnvironmentStrings"},
			{"syscall", "syscall.LoadDLL"},
			{"syscall", "syscall.MustLoadDLL"},
			{"syscall", "syscall.NewLazyDLL"},
			{"syscall", "syscall.LoadLibrary"},
			{"syscall", "syscall.GetProcAddress"},
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
}

// siteCheckEnvAllowed are names no entry of the group may refuse: the os calls
// the entry point and its tests read, write and exit with, the signal it catches,
// the syscall process names it may read, the names that only extend a refused one
// (os.Getenvs, os.LookupEnvs, os.ExpandEnvs, os.Environments, os.StartProcesses,
// os.UserHomeDirs, os.UserConfigDirs, os.UserCacheDirs, os.TempDirs,
// syscall.Getenvs, syscall.GetEnvironmentVariables, syscall.Environments,
// syscall.StartProcesses, syscall.Execve, syscall.CreateProcessX,
// syscall.Syscalls, syscall.RawSyscalls, syscall.AllThreadsSyscalls), and a
// package named os or syscall elsewhere.
var siteCheckEnvAllowed = []rootName{
	{"os", "os.Args"},
	{"os", "os.Exit"},
	{"os", "os.Interrupt"},
	{"os", "os.Open"},
	{"os", "os.Stdin"},
	{"os", "os.Stdout"},
	{"os", "os.Stderr"},
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
	{"syscall", "syscall.Getenvs"},
	{"syscall", "syscall.GetEnvironmentVariables"},
	{"syscall", "syscall.Environments"},
	{"syscall", "syscall.StartProcesses"},
	{"syscall", "syscall.Execve"},
	{"syscall", "syscall.CreateProcessX"},
	{"syscall", "syscall.Syscalls"},
	{"syscall", "syscall.RawSyscalls"},
	{"syscall", "syscall.AllThreadsSyscalls"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros", "os.LookupEnv"},
	{"example.com/otheros", "os.Environ"},
	{"example.com/othersyscall", "syscall.Exec"},
	{"example.com/othersyscall", "syscall.Getenv"},
}

// TestSiteCheckEnvEntryPatterns checks the entries of the site-check-env group
// one by one: the entry each pin names refuses every name the pin lists and no
// other listed name, no entry refuses a name siteCheckEnvAllowed lists, and every
// entry of the group is pinned. Without it an entry could lose a name - os.Environ
// out of the os pattern, syscall.SYS_EXECVE out of the syscall one - or gain one,
// and lint would stay green for the name that moved while the msg the entry
// carries says more than its pattern holds.
func TestSiteCheckEnvEntryPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupEntries(t, siteCheckEnvTag, siteCheckEnvEntries, siteCheckEnvAllowed)
}
