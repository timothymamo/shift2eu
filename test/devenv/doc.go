// Package devenv holds the checks of the development environment's files under
// dev/ and of the Makefile's VM targets that need no tool beyond Go and the
// shell: among them the broken Lima templates the validate-template job of
// .github/workflows/vm-tools.yml must see Lima refuse (issue #137), and the
// swap file dev/lima.yaml provisions (issue #294). It also pins that the shell
// blocks of docs/dev-environment.md hold no `!` interactive zsh would expand
// as a history event (zshhistory_test.go, issue #335), and that
// dev/infra-rules.sh holds the static rules of infra/'s OpenTofu configuration
// (infrarules_test.go, issue #422).
//
// TestInfraRules runs the script on the real infra/, which must hold every
// rule, and on copies of infra/ in t.TempDir() with exactly one breakage each,
// for which the script must report that rule's [<tag>] and no other. It reads
// files only: no tofu, no network, no credentials. TestInfraTracksNoLocalFiles
// pins that git tracks no file under infra/ that the maintainer's machine keeps
// (a tfvars file, backend.hcl, a state file, anything under .terraform), only
// the .example copies; it reads the repository's index and writes nothing.
//
// The swap tests (limaswap_test.go) pin the template's swapSize param and run
// its one system provision step that names PARAM_swapSize under bash, against
// a fake guest root (SHIFT2EU_SWAP_ROOT) with fakes of fallocate, chmod,
// mkswap, swapon, swapoff, sysctl and df first on PATH, unprivileged.
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
// The package has no production code. The test runs in make test, make check
// and CI's make cover, without the network or credentials; whether Lima
// refuses each template is checked by the job, with the pinned Lima.
package devenv
