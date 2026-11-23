// Package devenv holds the checks of the development environment's files under
// dev/ and of the Makefile's VM targets that need no tool beyond Go and the
// shell: among them the broken Lima templates the validate-template job of
// .github/workflows/vm-tools.yml must see Lima refuse (issue #137), and the
// swap file dev/lima.yaml provisions (issue #294). It also pins that the shell
// blocks of docs/dev-environment.md hold no `!` interactive zsh would expand
// as a history event (zshhistory_test.go, issue #335), that the host targets
// that load the VM's credentials refuse a host clone .envrc that reads a
// parent, and a .envrc or .env in or above the workspace, before they run
// anything and without printing a value (envrcguard_test.go, issue #402), and
// that dev/infra-rules.sh holds the static rules of infra/'s OpenTofu
// configuration (infrarules_test.go, issue #422).
//
// TestInfraRules runs the script on the real infra/, which must hold every
// rule, and on copies of infra/ in t.TempDir() with exactly one breakage each,
// for which the script must report that rule's [<tag>] and no other. It reads
// files only: no tofu, no network, no credentials. TestInfraTracksNoLocalFiles
// pins that git tracks no file under infra/ that the maintainer's machine keeps
// (a tfvars file, backend.hcl, a state file, anything under .terraform), only
// the .example copies; it reads the repository's index and writes nothing.
//
// TestInfraLockCompare and TestInfraLockCompareUsage (infralockcompare_test.go,
// issue #434) pin dev/infra-lock-compare.sh, which the "Infra tools" workflow
// runs after regenerating infra/site's lock file: pairs of lock files derived
// from the committed one, each differing in one way (a missing or extra hash,
// another version or constraint, a missing provider block), must be refused
// with [lock-hashes], equal and reordered ones pass, and a missing argument or
// file is a usage error. TestTofuDownloadHasTimeouts (tofudownload_test.go,
// issue #434) runs dev/tofu.sh in a temporary tree with a fake curl first on
// PATH and requires --connect-timeout and --max-time on every download. Both
// read and write temporary files only: no tofu, no network.
//
// The swap tests (limaswap_test.go) pin the template's swapSize param and run
// its one system provision step that names PARAM_swapSize under bash, against
// a fake guest root (SHIFT2EU_SWAP_ROOT) with fakes of fallocate, chmod,
// mkswap, swapon, swapoff, sysctl and df first on PATH, unprivileged.
//
// The apt tests (limaapt_test.go, issue #498) read the template's provisioning
// and pin that one system step writes the apt drop-in of timeouts and retries
// before every apt-get call, and that the ubuntuMirror param is read by one
// step that rewrites only the archive URIs of ubuntu.sources before the first
// apt-get update; with the vm-up tests, that make vm-up and make vm-create pass
// UBUNTU_MIRROR to limactl as that param, read raw and validated before
// limactl runs.
//
// TestLimaProbeNeverRunsTheGHWrapper (limaprobe_test.go, issue #479) reads the
// template's readiness probe and pins that no command of it runs the gh
// wrapper, which refuses without the agents App's token that exists only after
// the VM does; that it checks the real gh's attestation subcommand; and that
// it checks the wrapper with test -x and sh -n instead of running it.
//
// Each template in dev/testdata/lima-invalid is dev/lima.yaml with one known
// breakage, and beside it a .expect holds the text Lima's refusal of it must
// contain. TestInvalidLimaTemplatesDifferOnlyByTheirBreakage pins that each
// template is exactly dev/lima.yaml with its one-line edit applied, and that
// its .expect holds its case's refusal text and one newline.
// TestInvalidLimaTemplatesHaveACase pins that the directory holds one template
// and one .expect for every case and nothing else. A template that differed in
// more than its breakage could be refused for another reason, and one left
// behind when dev/lima.yaml changes would no longer test the real template.
// Since dev/lima.yaml itself must pass the same job, a refusal of a template
// can then only come from its breakage, and the job checks it is the one its
// .expect names. With -update the first test writes the templates and the
// .expect files from dev/lima.yaml and the cases instead of comparing.
//
// The package also pins the Makefile's VM targets as rules rather than through a run of one of them
// (makerules_test.go): every recipe that reads LIMA_INSTANCE runs $(LIMA_INSTANCE_CHECK) first and nothing but
// the check expands the name at make level (issue #313), and every recipe line that calls limactl puts
// $(VM_SHELL_ENV) before it (issue #326, item 2). The parser is pinned with synthetic Makefile texts, and the
// LIMA_INSTANCE targets of the behavioural tests come from it, so a target added to the Makefile is tested
// without anyone adding it to a list. TestVMUpSetsTheGitHooksInTheVM pins that vm-up re-sets the main checkout's
// core.hooksPath inside the VM on every start (issue #326, item 1), and
// TestMakeRunsLimactlWithoutTheAgentsAppVariables that the App's variables stay out of the environment of every
// limactl those targets run (item 2). TestProtectMainCheckSaysWhenJqCannotRun (gatecheck_test.go) pins that the
// gate check says when jq itself cannot run, with the jq's path and its error, instead of blaming GitHub's
// answer (issue #321).
//
// Every test that runs the Makefile runs the make SHIFT2EU_TEST_MAKE names
// (make_test.go, issue #314), make when it is unset, so the suite can run under
// the Mac's /usr/bin/make, GNU Make 3.81, which reads some recipes differently
// from the make 4.x of the VM and CI (issue #437). The make-381 job of
// .github/workflows/ci.yml runs the package on macOS with it set to
// /usr/bin/make and SHIFT2EU_TEST_MAKE_VERSION=3.81; with that variable set,
// TestTestMakeIsTheVersionNamed checks that make --version reports that release
// and, for 3.81, that a # line inside a continued recipe ends the recipe there,
// as on the Mac. Without it the test skips. TestTestsRunMakeThroughMakeProgram
// (makeprogramscan_test.go) refuses a literal make handed to exec.Command or
// exec.CommandContext in the package's test files, a guard against a test
// that runs make by accident: it catches the literal "make" or "gmake", not a
// program name built any other way. On
// macOS the fake VM's PATH starts with a GNU-style ln -T shim (sandbox_test.go,
// gnuLnT), as the VM is Linux and BSD ln has no -T.
//
// The package has no production code. The test runs in make test, make check
// and CI's make cover, without the network or credentials; whether Lima
// refuses each template is checked by the job, with the pinned Lima.
package devenv
