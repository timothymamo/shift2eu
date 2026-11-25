package check

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// The scheduled security audit's workflow of PRD #175 milestone 2 (ADR 0040):
// what it is read for.
const (
	// auditGuardCondition is the one guard condition, verbatim in every job's
	// if: and inside the concurrency group: main only, a run's first attempt
	// only, the schedule or the repository's owner.
	auditGuardCondition = "github.ref == 'refs/heads/main' && github.run_attempt == 1 && " +
		"(github.event_name == 'schedule' || github.triggering_actor == github.repository_owner)"
	// auditGroup is the concurrency group of a run whose jobs run; a run the
	// guard skips takes a group of its own.
	auditGroup = "${{ " + auditGuardCondition + " && 'scheduled-security-audit' || format('skipped-{0}', github.run_id) }}"
	// auditIf is every job's if:.
	auditIf = "${{ " + auditGuardCondition + " }}"

	auditWorkflowName = "Scheduled security audit"
	auditEnvironment  = "security-audit"
	// auditRunner is the runner image ADR 0040 recorded: its GNU coreutils
	// and its bubblewrap are what the wrapper's option analysis assumes.
	auditRunner = "ubuntu-24.04"
	// auditJobLimit is GitHub's limit on a job's timeout-minutes, which every
	// job stays below so that it ends on the workflow's own terms.
	auditJobLimit = 360

	auditCheckout = "actions/checkout"
	auditUpload   = "actions/upload-artifact"
	auditAppToken = "actions/create-github-app-token"

	auditChecksArtifact = "checks-result"
	auditModelArtifact  = "model-result"

	// auditGitHubToken is the job's own token, which a step reads only in its
	// step-level env:, and only where its spec says it reads GitHub's API.
	auditGitHubToken = "github.token"
	// auditAppTokenOutput is the audit App's token, the App token step's
	// output, which only the publish step reads.
	auditAppTokenOutput = "steps.app.outputs.token" //nolint:gosec // an expression's name, no credential
)

// auditStep is one step of the workflow, as the check holds it: its name, the
// action it uses, the values it may read, and the commands its script must
// hold.
type auditStep struct {
	name string
	// uses is the action the step runs, without its commit; "" for a run: step.
	uses string
	// artifact is the name the step uploads, for an upload step.
	artifact string
	// env and with are the credentials and variables the step reads, each one
	// key whose value is exactly one expression.
	env, with []auditRead
	// envMatch are env: variables whose value a pattern holds, not one
	// expression: a pin Renovate moves, such as an image's digest.
	envMatch []auditEnvMatch
	// githubToken is a step that reads GitHub's API with the job's token, as
	// GH_TOKEN in its step-level env:.
	githubToken bool
	// envNames, when not nil, are the only names its env: may set.
	envNames []string
	// runs are the commands its script must hold.
	runs []auditCommand
	// refuses are the commands its script must not hold, comments aside.
	refuses []auditCommand
	// order are commands of runs its script must hold in this order.
	order []auditCommand
	// check is the coverage ID of a step that reads the release's files or
	// its image (rows 7, 8, 9, 11 and 13, review round D of PR 5c): every
	// command of auditBounded it runs is under a timeout and records its
	// exit, and it records a failure as "<check> tool-failed" and a timeout as
	// "<check> timed-out" in not-run.txt, so that nothing the release holds
	// ends the step and the run's report.
	check string
	// budget is a step whose loop stops at an if [ "${SECONDS}" -ge <n> ]
	// below its own timeout-minutes, so that the loop, not the step's
	// timeout, ends it.
	budget bool
	// guardsChecksums is a download step whose every checksum check is a
	// guard that names the tool and fails the step (PRD #175, "A failed run
	// is visible").
	guardsChecksums bool
	// verifies is row 7's step (round E of PR 5c): each of its verifications
	// records its outcome in one branch, a failed verification as the finding
	// "<check> verification-failed", never as a check that did not run. Since
	// review round I its release file checks are gated on the download, and an
	// image digest it could not resolve records the verifications it skipped.
	verifies bool
	// outcomes are tool runs each followed by its outcome branch (review round
	// I of PR 5c, L3): a timeout recorded as "<check> timed-out" and any other
	// non-zero exit as "<check> tool-failed", both in not-run.txt.
	outcomes []auditOutcome
}

// auditOutcome is a tool run, by its first line, and the check its outcome
// branch records.
type auditOutcome struct {
	run   auditCommand
	check string
}

// auditRead is one env: or with: key and the one expression it reads.
type auditRead struct {
	key, value string
}

// auditEnvMatch is one env: variable whose value re must match; want names
// the value in a message.
type auditEnvMatch struct {
	key  string
	re   *regexp.Regexp
	want string
}

// auditCommand is a command a step's script must hold: re matches it, want
// names it in a message.
type auditCommand struct {
	re   *regexp.Regexp
	want string
}

// command returns an auditCommand that matches pattern.
func command(pattern, want string) auditCommand {
	return auditCommand{re: regexp.MustCompile(pattern), want: want}
}

// auditJob is one job of the workflow, as the check holds it.
type auditJob struct {
	key, name string
	// needs is the job it needs, "" for none.
	needs string
	// permissions are its GITHUB_TOKEN scopes, each read.
	permissions []string
	steps       []auditStep
	// downloadsModules names a step that must run go mod download: every Go
	// command after it runs offline.
	downloadsModules string
	// stepsWithinJob is a job whose steps' timeout-minutes add up to no more
	// than its own.
	stepsWithinJob bool
	// boundsTools is a job each of whose tool runs (auditToolRun) is under a
	// timeout of its own with its exit recorded, in every step but the ones
	// that download and verify the tools (round F of PR 5c).
	boundsTools bool
}

// secretValue is the expression that reads the secret name.
func secretValue(name string) string { return "${{ secrets." + name + " }}" }

// varValue is the expression that reads the variable name.
func varValue(name string) string { return "${{ vars." + name + " }}" }

// The scripts' commands the steps must hold.
var (
	checksumCheck   = command(`sha256sum --check --strict`, "sha256sum --check --strict")
	modDownload     = command(`(?m)(?:^|[\s;&|])go mod download(?:$|\s)`, "go mod download")
	stdinIdentity   = command(`age --decrypt --identity - `, "age --decrypt --identity - (the private key on stdin)")
	artifactsModel  = command(`auditpublish"? artifacts -job model `, "auditpublish artifacts -job model")
	artifactsPublic = command(`auditpublish"? artifacts -job publish `, "auditpublish artifacts -job publish")

	// The download's binding to the checked artifact (review round 1 of PR
	// 5b, findings A4 and R3): the ID and digest the artifacts subcommand
	// wrote to artifact.json, the zip of exactly that ID, its digest checked
	// before unzip reads it, and one file in it.
	artifactFile   = command(`artifact="\$\{RUNNER_TEMP\}/artifact\.json"`, `artifact="${RUNNER_TEMP}/artifact.json", the file the artifacts subcommand wrote`)
	artifactID     = command(`id="\$\(jq -er '\.id' "\$\{artifact\}"\)"`, `id="$(jq -er '.id' "${artifact}")", the ID from artifact.json`)
	artifactDigest = command(`digest="\$\(jq -er '\.digest' "\$\{artifact\}"\)"`, `digest="$(jq -er '.digest' "${artifact}")", the digest from artifact.json`)
	artifactZip    = command(`gh api "repos/\$\{GITHUB_REPOSITORY\}/actions/artifacts/\$\{id\}/zip" > `, `gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip", the checked artifact's zip`)
	// zipDigest is a whole line of its own (review round 4, M1): at the end of
	// another command's line it would be part of that command's conditions.
	zipDigest = command(`(?m)^[ \t]*echo "\$\{digest#sha256:\}  \$\{RUNNER_TEMP\}/[a-z-]+\.zip" \| sha256sum --check --strict -$`,
		`sha256sum --check --strict of the zip against the checked digest, on a line of its own`)
	zipUnpack = command(`unzip -q "\$\{RUNNER_TEMP\}/[a-z-]+\.zip" -d `, "unzip -q of the checked zip")
	// artifactShape refuses an ID or a digest of the wrong shape before either
	// reaches a URL or sha256sum (review round 6): its branch is held whole.
	artifactShape = command(`(?m)^[ \t]*if \[\[ ! "\$\{id\}" =~ \^\[0-9\]\+\$ \]\] \|\| \[\[ ! "\$\{digest\}" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]`+refusalBranch,
		`the shape guard of the artifact's ID and digest: if [[ ! "${id}" =~ ^[0-9]+$ ]] || [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
	zipOneFile = command(`(?m)^[ \t]*if \[ "\$\(ls -A "\$\{RUNNER_TEMP\}/[a-z-]+"\)" != [a-z-]+\.age \]`+refusalBranch,
		`the single-file check (ls -A) of the unzipped ciphertext: if [ "$(ls -A ...)" != <name>.age ]; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
	// secondListing is a download step that lists the run's artifacts again,
	// so that what it downloads is no longer what the check accepted.
	secondListing = command(`actions/runs/`, "a second listing of the run's artifacts (actions/runs/...), want the ID and digest from ${RUNNER_TEMP}/artifact.json, the checked listing's")

	// The attestation step's order: the digest read as a string alone and
	// refused unless it is exactly "" or one sha256 digest (the step trusts no
	// earlier step's read, review round 2, G3), the tag's shape whatever the
	// digest, then an empty digest skipped with a fixed notice (the run pulled
	// no image), then the digest's shape, and only then the verification.
	digestRead = command(`(?m)^[ \t]*if ! digest="\$\(jq -er '\.image_digest \| select\(type == "string"\) \| select\(\. == "" or test\("\^sha256:\[0-9a-f\]\{64\}\\\\z"\)\)' "\$\{RUNNER_TEMP\}/result\.json"\)"`+refusalBranch,
		`string-and-shape read of the digest: if ! digest="$(jq -er '.image_digest | select(type == "string") | select(. == "" or test("^sha256:[0-9a-f]{64}\\z"))' ...)"; then, `+
			`echo "::error ...", exit 1 and fi, each on a line of its own`)
	// latestTag is the checks job's lookup of the latest release's tag.
	latestTag = command(`releases/latest`, "the latest release's tag (releases/latest)")
	// imageLine names the image the attestation step verifies: this
	// repository's own, on a line of its own.
	imageLine = command(`(?m)^[ \t]*image="ghcr\.io/\$\{GITHUB_REPOSITORY,,\}"$`,
		`image="ghcr.io/${GITHUB_REPOSITORY,,}", this repository's image, on a line of its own`)
	tagShape = command(`(?m)^[ \t]*if \[\[ ! "\$\{tag\}" =~ \^v\[0-9\]\+\\\.\[0-9\]\+\\\.\[0-9\]\+\$ \]\]`+refusalBranch,
		`the tag's shape check: if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
	emptyDigest = command(`(?m)^[ \t]*if \[ -z "\$\{digest\}" \]; then\n[ \t]+echo "image not pulled[^"\n]*"\n[ \t]+exit 0\n[ \t]*fi$`,
		`the empty digest's branch: if [ -z "${digest}" ]; then, echo "image not pulled: ...", exit 0 and fi, each on a line of its own`)
	digestShape = command(`(?m)^[ \t]*if \[\[ ! "\$\{digest\}" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]`+refusalBranch,
		`the digest's shape check: if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
	verify = command(verifyPattern(), `gh attestation verify "oci://${image}@${digest}" as one command, each line but the last ending in \: --repo "${GITHUB_REPOSITORY}", `+
		`--signer-workflow "${GITHUB_REPOSITORY}/.github/workflows/release.yml", --source-ref "refs/tags/${tag}" and --deny-self-hosted-runners last, `+
		`alone or as if ! ... --deny-self-hosted-runners > /dev/null; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
)

// refusalBranch is the rest of a guard's if, from its condition's ; then:
// an echo "::error ..." line, an exit 1 line and a fi line, each whole (review
// round 5, L1). A branch whose exit 1 is dropped or turned into exit 0, or
// whose lines are run together, matches no more.
const refusalBranch = `; then\n[ \t]+echo "::error [^"\n]*"\n[ \t]+exit 1\n[ \t]*fi$`

// verifyPattern matches the attestation step's verification as one contiguous
// command (review round 4, M1): its first line through every flag's line, each
// ending in a \ continuation, to --deny-self-hosted-runners, which ends its
// line, either as the command alone (set -e fails the step) or as the
// condition of an if ! ... > /dev/null with its refusalBranch (review round 5,
// L1). A flag cut off by a ;, a dropped \ or a comment is then no part of it,
// and the command matches no more.
func verifyPattern() string {
	const next = ` \\\n[ \t]+`

	body := `gh attestation verify "oci://\$\{image\}@\$\{digest\}"` +
		next + `--repo "\$\{GITHUB_REPOSITORY\}"` +
		next + `--signer-workflow "\$\{GITHUB_REPOSITORY\}/\.github/workflows/release\.yml"` +
		next + `--source-ref "refs/tags/\$\{tag\}"` +
		next + `--deny-self-hosted-runners`

	return `(?m)^[ \t]*(?:if ! ` + body + ` > /dev/null` + refusalBranch + `|` + body + `$)`
}

// guard returns the pattern of a whole-line guard: the if whose condition is
// cond, verbatim, and its refusalBranch.
func guard(cond string) string {
	return `(?m)^[ \t]*` + regexp.QuoteMeta("if "+cond) + refusalBranch
}

// wholeLine returns the pattern of text, verbatim, as a line of its own.
func wholeLine(text string) string {
	return `(?m)^[ \t]*` + regexp.QuoteMeta(text) + `$`
}

// auditContainerFlags are the flags every container the checks job starts is
// created with (PR 5c): a read-only root filesystem, no network, no
// capability and no new privilege.
const auditContainerFlags = "docker create --read-only --network none --cap-drop ALL --security-opt no-new-privileges"

// auditFuzzTmpfs is the one tmpfs of the workflow, row 14's: the size, mode
// and flags its go test needs, and nothing else writable in its container.
const auditFuzzTmpfs = "--tmpfs /scratch:rw,exec,nosuid,size=4g,mode=1777"

// Rows 3 and 5's exit codes (round G of PR 5c), as the pinned tools' sources
// write them: a branch that reads a tool's error as a finding records a check
// that ran when none did, and one that reads a finding as an error loses what
// the tool found.
var (
	// govulncheck exits 0 with -format json whatever it finds (only its text
	// mode exits 3), so each of its branches reads every non-zero exit as the
	// tool that failed, and no branch excuses another exit.
	govulncheckFailed = command(wholeLine(`elif [ "${rc}" -ne 0 ]; then`),
		`elif [ "${rc}" -ne 0 ]; then, on a line of its own: govulncheck -format json exits 0 whatever it finds, so every non-zero exit is the tool that failed (PRD #175 milestone 2, ADR 0040)`)
	govulncheckExcuse = command(`(?m)^[ \t]*elif \[ "\$\{rc\}" -ne 0 \] &&`,
		`a govulncheck branch that excuses a non-zero exit: govulncheck -format json exits 0 whatever it finds (3 is its text mode's), so no exit but 0 is a finding (PRD #175 milestone 2, ADR 0040)`)
	// gitleaks exits 1 both on a leak and on an error unless told otherwise, so
	// its run is told --exit-code 2 and only 2 is a finding.
	gitleaksExitCode = command(`(?m)^[ \t]*timeout \S+ gitleaks git [^\n]* --exit-code 2 [^\n]*$`,
		`gitleaks git ... --exit-code 2: its default leak exit, 1, is its error exit too, so an error that wrote no report would read as clean (PRD #175 milestone 2, ADR 0040)`)
	gitleaksFailed = command(wholeLine(`elif [ "${rc}" -ne 0 ] && [ "${rc}" -ne 2 ]; then`),
		`elif [ "${rc}" -ne 0 ] && [ "${rc}" -ne 2 ]; then, on a line of its own: run with --exit-code 2, gitleaks exits 2 on a leak and every other non-zero exit is the tool that failed (PRD #175 milestone 2, ADR 0040)`)
	// zizmor exits 11 to 14 by the highest finding's severity and 1 on an error.
	zizmorFailed = command(wholeLine(`elif [ "${rc}" -ne 0 ] && { [ "${rc}" -lt 11 ] || [ "${rc}" -gt 14 ]; }; then`),
		`elif [ "${rc}" -ne 0 ] && { [ "${rc}" -lt 11 ] || [ "${rc}" -gt 14 ]; }; then, on a line of its own: zizmor exits 11 to 14 by its highest finding's severity and 1 on an error, which is the tool that failed (PRD #175 milestone 2, ADR 0040)`)
)

// The checks job's rows 6 to 15 (PR 5c): the commands their steps must hold.
var (
	// Row 6: scorecard's report goes to the results directory alone, and the
	// CLI publishes nothing.
	scorecardOut = command(wholeLine(`out="${results}/scorecard"`), `out="${results}/scorecard", scorecard's folder of the results directory`)
	scorecardRun = command(`(?m)^[ \t]*timeout 15m scorecard --repo "github\.com/\$\{GH_REPO\}" [^\n]*> "\$\{out\}/scorecard\.json" 2> "\$\{out\}/scorecard\.stderr"`,
		`timeout 15m scorecard --repo "github.com/${GH_REPO}" ... > "${out}/scorecard.json" 2> "${out}/scorecard.stderr": its report in the results directory, never on the public log`)
	scorecardPublish = command(`(?:^|[\s;&|(])scorecard\s[^\n]*--publish`, "scorecard --publish, which sends the result where the run keeps nothing: the report stays in the results directory")

	// Row 7: the recorded tag, read and its shape refused before it reaches a
	// command line.
	tagRead = command(wholeLine(`tag="$(sed -n 's/^release_tag=//p' "${results}/target.txt")"`),
		`tag="$(sed -n 's/^release_tag=//p' "${results}/target.txt")", the tag row 1 recorded`)

	// Rows 8, 11 and 13: the image digest row 7 recorded, its shape refused
	// before it reaches a docker command.
	recordedDigest = command(wholeLine(`digest="$(cat "${results}/release-integrity/image-digest.txt")"`),
		`digest="$(cat "${results}/release-integrity/image-digest.txt")", the image digest the release's verification recorded`)
	imageDigestShape = command(guard(`[[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]`),
		`the recorded image digest's shape guard: if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then, echo "::error ...", exit 1 and fi, each on a line of its own`)
	anonymousConfig = command(wholeLine(`export DOCKER_CONFIG="${RUNNER_TEMP}/docker-anonymous"`),
		`export DOCKER_CONFIG="${RUNNER_TEMP}/docker-anonymous", an empty Docker configuration: the image is pulled anonymously`)
	imagePull    = command(regexp.QuoteMeta(`docker pull --platform linux/amd64 "${image}@${digest}"`), `docker pull --platform linux/amd64 "${image}@${digest}", the image by its recorded digest`)
	imageInspect = command(regexp.QuoteMeta(`docker image inspect "${image}@${digest}"`), `docker image inspect "${image}@${digest}", the pulled image by its recorded digest`)

	// Row 9: govulncheck is no go command, so its offline settings are its own.
	binaryGovulncheck = command(regexp.QuoteMeta(`GOFLAGS=-mod=readonly GOPROXY=off timeout 5m govulncheck -mode=binary `),
		"GOFLAGS=-mod=readonly GOPROXY=off timeout 5m govulncheck -mode=binary: the binary scan runs offline")

	// Row 7 (review round D of PR 5c): checksums.txt is the release's, so
	// only its well-formed lines reach sha256sum.
	// The filtered list is the results directory's (round E), so neither the
	// release's own listing nor its attestation loop ever sees it.
	checksumFilter = command(wholeLine(`timeout 1m grep -E '^[0-9a-f]{64}  [A-Za-z0-9._+-]+$' "${rel}/checksums.txt" > "${out}/checksums.filtered" 2> "${out}/checksums-filter.stderr" || rc=$?`),
		`timeout 1m grep -E '^[0-9a-f]{64}  [A-Za-z0-9._+-]+$' "${rel}/checksums.txt" > "${out}/checksums.filtered" ... || rc=$?, on a line of its own: `+
			`only checksums.txt's well-formed lines reach sha256sum, never a /dev/zero it lists, and the filtered list is the results directory's, never the release's`)
	checksumFiltered = command(regexp.QuoteMeta(`timeout 5m sha256sum --check --strict "${out}/checksums.filtered")`),
		`timeout 5m sha256sum --check --strict "${out}/checksums.filtered": the release's files against checksums.txt's filtered lines alone`)

	// Row 9: each archive's member streamed through a size cap.
	unpackTar = command(wholeLine(`*.tar.gz) timeout 2m tar -xOzf "${archive}" shift2eu 2> "${out}/${name}.unpack.stderr" | head -c 134217728 > "${dir}/shift2eu" || rc=$? ;;`),
		`*.tar.gz) timeout 2m tar -xOzf "${archive}" shift2eu ... | head -c 134217728 > "${dir}/shift2eu" || rc=$? ;;: the member alone, through the 128 MiB size cap`)
	unpackZip = command(wholeLine(`*.zip) timeout 2m unzip -p "${archive}" shift2eu.exe 2> "${out}/${name}.unpack.stderr" | head -c 134217728 > "${dir}/shift2eu.exe" || rc=$? ;;`),
		`*.zip) timeout 2m unzip -p "${archive}" shift2eu.exe ... | head -c 134217728 > "${dir}/shift2eu.exe" || rc=$? ;;: the member alone, through the 128 MiB size cap`)

	// Row 10: trivy scans offline, from the database row 2 downloaded: no
	// database, Java database, check bundle or version download, no telemetry.
	trivyScans = append(trivyFlags("--skip-db-update", "--skip-java-db-update", "--skip-check-update", "--offline-scan", "--disable-telemetry"),
		command(regexp.QuoteMeta(`trivy image "${offline[@]}" `), `trivy image "${offline[@]}", the image scan with the offline flags`),
		command(regexp.QuoteMeta(`trivy sbom "${offline[@]}" `), `trivy sbom "${offline[@]}", the SBOM scan with the offline flags`))

	// Row 11: the version command, in a container with the four flags and
	// row 13's limits, created under a timeout; the image's filesystem listed
	// with tar's own warnings in the results directory, never on the log.
	runtimeCreate = command(wholeLine(`cid="$(timeout 2m `+auditContainerFlags+` --memory 512m --pids-limit 64 "${image}@${digest}" version)" || rc=$?`),
		`cid="$(timeout 2m `+auditContainerFlags+` --memory 512m --pids-limit 64 "${image}@${digest}" version)" || rc=$?, on a line of its own: `+
			`the image's container read-only, with no network, no capability, no new privilege, its memory (--memory) and pid (--pids-limit) limits, and a create that cannot hang`)
	runtimeListing = command(regexp.QuoteMeta(`| tar -tvf - > "${out}/filesystem.txt" 2> "${out}/filesystem.stderr" || rc=$?`),
		`| tar -tvf - > "${out}/filesystem.txt" 2> "${out}/filesystem.stderr": tar's warnings name the image's entries, so they go to filesystem.stderr, never to the public log`)

	// Row 13: the inputs reach the image by docker cp into a staging container
	// that is never started, which docker commit turns into the image each
	// run's container starts from (ADR 0040); each run's container has the
	// four flags and its limits.
	hostileCorpus = command(regexp.QuoteMeta(`cp -P .github/security-audit/testdata/hostile-inventories/*.json "${stage}/"`),
		`cp -P .github/security-audit/testdata/hostile-inventories/*.json "${stage}/": a link the corpus holds is copied as a link, never followed on the runner`)
	hostileCopy   = command(regexp.QuoteMeta(`docker cp "${stage}" "${staging}:/work/"`), `docker cp "${stage}" "${staging}:/work/", the inputs copied into the staging container`)
	hostileCommit = command(regexp.QuoteMeta(`docker commit "${staging}" "audit-hostile:${GITHUB_RUN_ID}"`), `docker commit "${staging}" "audit-hostile:${GITHUB_RUN_ID}", the image the runs start from`)
	hostileCreate = command(regexp.QuoteMeta(`cid="$(timeout 2m `+auditContainerFlags+` --memory 512m --pids-limit 64 "audit-hostile:${GITHUB_RUN_ID}" assess `),
		`cid="$(timeout 2m `+auditContainerFlags+` --memory 512m --pids-limit 64 "audit-hostile:${GITHUB_RUN_ID}" assess ...: each run read-only, with no network, no capability, no new privilege and its limits`)

	// Row 14: the Go image by digest, each target's shape refused before it
	// reaches the container, and the container with the four flags, its one
	// tmpfs and the step's offline settings.
	goImage       = command(wholeLine(`image="golang:${GO_IMAGE}"`), `image="golang:${GO_IMAGE}", the Go image the step's env: pins by digest`)
	fuzzDiscovery = command(regexp.QuoteMeta(`grep -oE '^func Fuzz[A-Z0-9_][A-Za-z0-9_]*\(f \*testing\.F\)' "${file}"`),
		`grep -oE '^func Fuzz[A-Z0-9_][A-Za-z0-9_]*\(f \*testing\.F\)' "${file}": the Fuzz targets by their signature alone, never a helper such as Fuzzy`)
	// fuzzShape records a target of another shape and goes on (review round D
	// of PR 5c, G): its branch is held whole, continue included.
	fuzzShape = command(`(?m)^[ \t]*`+regexp.QuoteMeta(`if [[ ! "${name}" =~ ^Fuzz[A-Z0-9_][A-Za-z0-9_]*$ ]] || [[ ! "${pkg}" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] || [[ "${pkg}" == *..* ]]; then`)+
		`\n[ \t]+`+regexp.QuoteMeta(`echo "fuzzing unexpected-target" >> "${results}/not-run.txt"`)+`\n[ \t]+continue\n[ \t]*fi$`,
		`the fuzz target's shape guard: if [[ ! "${name}" =~ ^Fuzz[A-Z0-9_][A-Za-z0-9_]*$ ]] || [[ ! "${pkg}" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] || [[ "${pkg}" == *..* ]]; then, `+
			`echo "fuzzing unexpected-target" >> "${results}/not-run.txt", continue and fi, each on a line of its own: a target of another shape is recorded, `+
			`and ends neither the step nor the run's report`)
	// fuzzCreate holds the fuzz container's create whole, through its last
	// line (review round D of PR 5c, B1): a flag added after the pinned ones
	// would override them.
	fuzzCreate = command(fuzzCreatePattern(),
		`the fuzz container's create, whole and each line exactly: cid="$(timeout 2m `+auditContainerFlags+` --memory 10g --pids-limit 512 --user 65534:65534 \, `+
			auditFuzzTmpfs+` \, its --env lines, "audit-fuzz:${GITHUB_RUN_ID}" \ and its sh -c command, closed by )" || rc=$?: read-only, with no network, no capability, no new privilege, `+
			`its memory (--memory) and pid (--pids-limit) limits, as nobody, no flag past the pinned ones, and a create that cannot hang`)
	fuzzTmpfs = command(`(?m)`+regexp.QuoteMeta(`--user 65534:65534 \`)+`\n[ \t]+`+regexp.QuoteMeta(auditFuzzTmpfs+` \`)+`$`,
		auditFuzzTmpfs+` \, exactly, on the line after --user 65534:65534 \: the fuzz container's one writable path`)
	fuzzEnv = command(`(?m)`+regexp.QuoteMeta(auditFuzzTmpfs+` \`)+`\n[ \t]+`+regexp.QuoteMeta(`--env GOFLAGS --env GOPROXY `),
		"--env GOFLAGS --env GOPROXY on the line after the tmpfs: the step's GOFLAGS=-mod=readonly and GOPROXY=off handed to go test in the container")
	fuzzTime = command(`-fuzztime 10m `, "-fuzztime 10m, each target's fuzzing budget")

	// Row 15: each ruleset ID's shape refused before it reaches a URL, and
	// recorded, never the end of the step (review round I of PR 5c, L6): an ID
	// the API answers in another shape is that read not run, and the step goes
	// on to its other reads.
	rulesetShape = command(`(?m)^[ \t]*`+regexp.QuoteMeta(`if [[ ! "${id}" =~ ^[0-9]+$ ]]; then`)+
		`\n[ \t]+`+regexp.QuoteMeta(`echo "supply-chain unexpected-ruleset" >> "${results}/not-run.txt"`)+`\n[ \t]+continue\n[ \t]*fi$`,
		`the ruleset ID's shape guard: if [[ ! "${id}" =~ ^[0-9]+$ ]]; then, echo "supply-chain unexpected-ruleset" >> "${results}/not-run.txt", continue and fi, `+
			`each on a line of its own: an ID of another shape is recorded and never reaches a URL, and it ends neither the step nor the run's report (PRD #175 milestone 2, ADR 0040)`)
	rulesetRead = command(regexp.QuoteMeta(`read_api "ruleset-${id}" "repos/${GH_REPO}/rulesets/${id}"`), `read_api "ruleset-${id}" "repos/${GH_REPO}/rulesets/${id}", the ruleset's read`)
	// The variables endpoints return each variable's value, which no read
	// keeps (review round D of PR 5c, D): read_api passes its third argument to
	// gh api as the jq filter, and each names read passes the one that keeps
	// the names alone.
	readAPIFilter = command(regexp.QuoteMeta(`local name="$1" endpoint="$2" filter="${3:-.}" rc=0`),
		`local name="$1" endpoint="$2" filter="${3:-.}" rc=0 in read_api: its third argument is the jq filter, "." when none is given`)
	readAPIJq = command(regexp.QuoteMeta(`timeout 2m gh api "${endpoint}" --jq "${filter}" > "${out}/api/${name}.json"`),
		`timeout 2m gh api "${endpoint}" --jq "${filter}" > "${out}/api/${name}.json" in read_api: every read through its filter`)
	namesReads = []auditCommand{
		namesRead(`read_api repository-variable-names "repos/${GH_REPO}/actions/variables" '[.variables[].name]'`),
		namesRead(`read_api "environment-${env}-variable-names" "repos/${GH_REPO}/environments/${env}/variables" '[.variables[].name]'`),
		namesRead(`read_api repository-secret-names "repos/${GH_REPO}/actions/secrets" '[.secrets[].name]'`),
		namesRead(`read_api "environment-${env}-secret-names" "repos/${GH_REPO}/environments/${env}/secrets" '[.secrets[].name]'`),
	}

	// anyTmpfs is a tmpfs in a container that needs none.
	anyTmpfs = command(`--tmpfs`, "--tmpfs, a writable path in a container that needs none: row 14's is the workflow's one tmpfs")

	// goImageDigest is row 14's Go image pin: a tag and its @sha256: digest.
	goImageDigest = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*@sha256:[0-9a-f]{64}$`)
)

// Review round I of PR 5c: the reviewer's and the auditor's findings, each a
// command the checks job's steps must hold.
var (
	// L2: row 7's release download records its outcome and clears rel_ok on a
	// timeout, a failure and a download with no checksums.txt, so that the
	// release's file checks, which the rel_ok gate holds, never read an absent
	// list as a mismatch.
	releaseDownloadOutcome = command(`(?m)^[ \t]*timeout \S+ gh release download "\$\{tag\}" [^\n]* \|\| rc=\$\?\n`+
		wholeLines(
			`echo "${rc}" > "${out}/download.exit"`,
			`rel_ok=1`,
			`if [ "${rc}" -eq 124 ]; then`,
			`echo "release-integrity timed-out" >> "${results}/not-run.txt"`,
		)+`\n[ \t]+echo "release-integrity: [^"\n]+" >> "\$\{results\}/limits\.txt"\n`+
		wholeLines(
			`rel_ok=0`,
			`elif [ "${rc}" -ne 0 ]; then`,
			`echo "release-integrity tool-failed" >> "${results}/not-run.txt"`,
			`rel_ok=0`,
			`elif [ ! -s "${rel}/checksums.txt" ]; then`,
			`echo "release-integrity results-missing" >> "${results}/not-run.txt"`,
		)+`\n[ \t]+echo "release-integrity: [^"\n]+" >> "\$\{results\}/limits\.txt"\n`+
		wholeLines(`rel_ok=0`, `fi`)+`$`,
		`the release download's outcome right after it, each on a line of its own: echo "${rc}" > "${out}/download.exit", rel_ok=1, `+
			`if [ "${rc}" -eq 124 ]; then (release-integrity timed-out, its limits line, rel_ok=0), elif [ "${rc}" -ne 0 ]; then (release-integrity tool-failed, rel_ok=0), `+
			`elif [ ! -s "${rel}/checksums.txt" ]; then (release-integrity results-missing, its limits line, rel_ok=0) and fi: `+
			`a failed, partial or empty download is a check that did not run, never a checksum finding (PRD #175 milestone 2, ADR 0040)`)
	// releaseFileGate is the if that holds the release's file checks to a
	// download that brought checksums.txt.
	releaseFileGate = command(wholeLine(`if [ "${rel_ok}" -eq 1 ]; then`), `if [ "${rel_ok}" -eq 1 ]; then, on a line of its own`)
	// releaseFileChecks are the commands the gate holds: the filter, the
	// checksum check, the listing's diff and each release file's attestation.
	releaseFileChecks = []auditCommand{
		checksumFilter, checksumFiltered,
		command(regexp.QuoteMeta(`diff "${out}/listed.txt" "${out}/assets.txt"`), `diff "${out}/listed.txt" "${out}/assets.txt", the listing's comparison`),
		command(regexp.QuoteMeta(`gh attestation verify "${file}"`), `gh attestation verify "${file}", each release file's attestation`),
	}

	// L5: the three values latest.txt holds beside the tag, each blanked unless
	// it is of its one shape, before latest.txt is written.
	tagDigestShape     = blankUnless("tag_digest", `^sha256:[0-9a-f]{64}$`)
	latestDigestShape  = blankUnless("latest_digest", `^sha256:[0-9a-f]{64}$`)
	latestReleaseShape = blankUnless("latest_release", `^v[0-9]+\.[0-9]+\.[0-9]+$`)
	latestRecord       = command(regexp.QuoteMeta(`"${tag}" "${latest_release}" "${tag_digest}" "${latest_digest}" > "${out}/latest.txt"`),
		`printf ... "${tag}" "${latest_release}" "${tag_digest}" "${latest_digest}" > "${out}/latest.txt", the release's latest record`)

	// S4: the if that verifies the image when its digest resolved; its else
	// records the verifications it skipped.
	tagDigestBranch = command(wholeLine(`if [ -n "${tag_digest}" ]; then`), `if [ -n "${tag_digest}" ]; then, on a line of its own`)

	// L4: row 9 counts an archive's member only when it holds bytes.
	binaryFind = command(wholeLine(`bin="$(find "${dir}" -maxdepth 1 -type f -size +0 \( -name shift2eu -o -name shift2eu.exe \) -print -quit)"`),
		`bin="$(find "${dir}" -maxdepth 1 -type f -size +0 \( -name shift2eu -o -name shift2eu.exe \) -print -quit)", on a line of its own: `+
			`the binary counts only when it holds bytes (-size +0), so an archive whose member is missing leaves head's empty file, which is results-missing, never a scan (PRD #175 milestone 2, ADR 0040)`)

	// L3: rows 6, 8 and 10's tool runs, each followed by its outcome branch.
	scorecardOutcome = command(`^[ \t]*timeout \S+ scorecard `, "scorecard's run (timeout 15m scorecard ...)")
	pullOutcome      = command(`^[ \t]*timeout \S+ docker pull `, "the image's pull (timeout 8m docker pull ...)")
	trivyOutcomes    = []auditCommand{
		command(`^[ \t]*timeout \S+ trivy image `, `the image scan (timeout 8m trivy image ...)`),
		command(`^[ \t]*timeout \S+ trivy sbom `, `the SBOM scan (timeout 4m trivy sbom ...)`),
	}
)

// wholeLines returns the pattern of lines, each verbatim and whole, one after
// the other, each after any indent; the first has no line start of its own,
// so it follows the pattern before it.
func wholeLines(lines ...string) string {
	quoted := make([]string, 0, len(lines))
	for _, l := range lines {
		quoted = append(quoted, `[ \t]*`+regexp.QuoteMeta(l))
	}

	return strings.Join(quoted, `\n`)
}

// blankUnless returns the command of row 7's shape check of name: the value
// blanked unless it matches shape, each line whole (review round I of PR 5c,
// L5).
func blankUnless(name, shape string) auditCommand {
	cond := `if [[ ! "${` + name + `}" =~ ` + shape + ` ]]; then`

	return command(`(?m)^[ \t]*`+regexp.QuoteMeta(cond)+`\n[ \t]+`+regexp.QuoteMeta(name+`=""`)+`\n[ \t]*fi$`,
		cond+`, `+name+`="" and fi, each on a line of its own, before latest.txt is written: a registry's or the API's answer of another shape is recorded as no value, `+
			`never as one (PRD #175 milestone 2, ADR 0040)`)
}

// The ::error lines of PRD #175's "A failed run is visible" (round E of PR
// 5c): each step that can fail with one of the box's causes names the cause's
// kind and the runbook step that recovers from it, and no secret and no
// finding's detail.
var (
	// The hardening step's three commands record their own failure, so its
	// branch, not set -e's own line, ends the step.
	hardeningCommands = []auditCommand{
		command(wholeLine(`sudo systemctl stop docker.service docker.socket || rc=$?`), `sudo systemctl stop docker.service docker.socket || rc=$?, on a line of its own`),
		command(wholeLine(`sudo systemctl disable docker.service docker.socket || rc=$?`), `sudo systemctl disable docker.service docker.socket || rc=$?, on a line of its own`),
		command(wholeLine(`sudo rm -f /etc/sudoers.d/runner || rc=$?`), `sudo rm -f /etc/sudoers.d/runner || rc=$?, on a line of its own`),
	}
	hardeningError = failureBranch(`(?m)^[ \t]*if \[ "\$\{rc\}" -ne 0 \]`, `if [ "${rc}" -ne 0 ]`,
		"::error title=Audit model::the runner could not be hardened: see runbook step 10")
	// The launcher prints one constant class, so an expired or revoked token,
	// the usage limit, a failed preflight and a managed-settings path read
	// alike here: the step's line names what the maintainer recovers from.
	runError = failureBranch(`(?m)^[ \t]*if ! (?:ci/|"\$\{RUNNER_TEMP\}/bin/)auditrun"? -claude-tarball [^\n]*`, `if ! <auditrun> -claude-tarball ...`,
		"::error title=Audit model::the audit did not run: see runbook step 10")
	modelArtifactError = failureBranch(`(?m)^[ \t]*if ! (?:ci/|"\$\{RUNNER_TEMP\}/bin/)auditpublish"? artifacts -job model [^\n]*`, `if ! <auditpublish> artifacts -job model ...`,
		"::error title=Audit model::the run's artifacts are unexpected: see runbook step 11")
	publishArtifactError = failureBranch(`(?m)^[ \t]*if ! (?:ci/|"\$\{RUNNER_TEMP\}/bin/)auditpublish"? artifacts -job publish [^\n]*`, `if ! <auditpublish> artifacts -job publish ...`,
		"::error title=Audit publish::the run's artifacts are unexpected: see runbook step 11")
	publishError = failureBranch(`(?m)^[ \t]*if ! (?:ci/|"\$\{RUNNER_TEMP\}/bin/)auditpublish"? publish [^\n]*`, `if ! <auditpublish> publish ...`,
		"::error title=Audit publish::the publish was refused: see runbook step 11")
	// The install script is shared with the AI reviewer and its own two ::error
	// lines name no runbook step (round F of PR 5c), so the model job's call of it
	// is held whole under a guard that names the step: a pin the pin check holds to
	// the registry's integrity, or a managed policy on the runner image.
	installError = failureBranch(`(?m)^[ \t]*if ! \.github/scripts/install-claude-code\.sh "[^"\n]+" "[^"\n]+" "[^"\n]+"`,
		`if ! .github/scripts/install-claude-code.sh "<dir>" "<package>" "<integrity>"`,
		"::error title=Audit model::the Claude Code CLI did not install: see runbook step 11")
)

// failureBranch returns the command of a step's failure branch: the if whose
// condition cond matches, up to its ; then, an echo of exactly the ::error
// line msg, exit 1 and fi, each on a line of its own.
func failureBranch(cond, condText, msg string) auditCommand {
	return command(cond+`; then\n[ \t]+`+regexp.QuoteMeta(`echo "`+msg+`"`)+`\n[ \t]+exit 1\n[ \t]*fi$`,
		condText+`; then, echo "`+msg+`", exit 1 and fi, each on a line of its own: a failed run names the cause's kind and the runbook step that recovers from it (PRD #175, a failed run is visible)`)
}

// namesRead returns the command of one of row 15's names reads, whole on a
// line of its own: the jq filter at its end keeps the names alone.
func namesRead(line string) auditCommand {
	filter := line[strings.LastIndexByte(line[:len(line)-1], '\'')+1 : len(line)-1]

	return command(wholeLine(line), line+": the names alone, through the jq filter "+filter+", never a value")
}

// trivyFlags returns one command per flag the offline=(...) array of row 10
// must hold.
func trivyFlags(flags ...string) []auditCommand {
	out := make([]auditCommand, 0, len(flags))

	for _, f := range flags {
		out = append(out, command(`(?m)^[ \t]*offline=\([^\n]*`+regexp.QuoteMeta(" "+f)+`[ )]`,
			f+` in trivy's offline=(...) flags: the scans download nothing and send nothing`))
	}

	return out
}

// fuzzCreateLines are row 14's docker create, line by line (review round D of
// PR 5c, B1).
// Since review round I (M2) the create runs under a timeout of its own and
// records its exit after the command substitution's close.
var fuzzCreateLines = []string{
	`cid="$(timeout 2m ` + auditContainerFlags + ` --memory 10g --pids-limit 512 --user 65534:65534 \`,
	auditFuzzTmpfs + ` \`,
	`--env GOFLAGS --env GOPROXY --env GOTOOLCHAIN=local --env GOMODCACHE=/modcache --env GOCACHE=/scratch/go-build \`,
	`--env GOTMPDIR=/scratch --env TMPDIR=/scratch --env HOME=/scratch --env FUZZ_PKG="${pkg}" --env FUZZ_NAME="${name}" \`,
	`"audit-fuzz:${GITHUB_RUN_ID}" \`,
	`sh -c 'cp -R /src /scratch/src && cd /scratch/src && go test -run "^$" -fuzz "^${FUZZ_NAME}\$" -fuzztime 10m "./${FUZZ_PKG}" >&2; rc=$?; ` +
		`tar -C /scratch/src -cf - "${FUZZ_PKG}/testdata/fuzz" 2> /dev/null; exit "${rc}"')" || rc=$?`,
}

// fuzzCreatePattern matches fuzzCreateLines as one contiguous command, each
// line whole.
func fuzzCreatePattern() string {
	quoted := make([]string, 0, len(fuzzCreateLines))
	for _, l := range fuzzCreateLines {
		quoted = append(quoted, regexp.QuoteMeta(l))
	}

	return `(?m)^[ \t]*` + strings.Join(quoted, `\n[ \t]+`) + `$`
}

// auditJobs are the workflow's three jobs, in order, with their steps in
// order: the closed list a step is recognised by, by name.
var auditJobs = []auditJob{
	{
		key:  "checks",
		name: "Checks",
		// The four scopes its rows read with (PR 5c): contents for the
		// checkout and the release, attestations for row 7's verification,
		// actions for scorecard's reads of the workflows' runs, issues for
		// Renovate's Dependency Dashboard. No packages scope (the image is
		// pulled anonymously) and no id-token (scorecard runs as the CLI).
		permissions:      []string{"contents", "attestations", "actions", "issues"},
		downloadsModules: "Download the Go modules",
		stepsWithinJob:   true,
		boundsTools:      true,
		steps: []auditStep{
			{name: "Check out the audited commit", uses: auditCheckout},
			{
				name: "Record the audited commit and the latest release tag", githubToken: true,
				// The tag's shape is checked right after the lookup, before the
				// tag reaches any command line.
				runs:  []auditCommand{latestTag, tagShape},
				order: []auditCommand{latestTag, tagShape},
			},
			{name: "Download the pinned tools and verify their checksums", runs: []auditCommand{checksumCheck}, guardsChecksums: true},
			{
				name: "Download the Go modules",
				runs: []auditCommand{modDownload, command(`go -C tools mod download`, "go -C tools mod download")},
			},
			{
				name:    "govulncheck",
				runs:    []auditCommand{command(`govulncheck .*\./\.\.\.`, "govulncheck ./..."), govulncheckFailed},
				refuses: []auditCommand{govulncheckExcuse},
			},
			{name: "osv-scanner", runs: []auditCommand{command(`osv-scanner scan source `, "osv-scanner scan source")}},
			{
				name: "gitleaks on the history",
				runs: []auditCommand{
					command(`gitleaks git .*--redact`, "gitleaks git --redact"),
					command(`gitleaks git .*--ignore-gitleaks-allow`, "gitleaks git --ignore-gitleaks-allow (no allow comment in the history hides a secret)"),
					gitleaksExitCode, gitleaksFailed,
				},
			},
			// Two steps: only zizmor's online audits read GitHub's API, so only
			// its step holds the job's token.
			{name: "zizmor", githubToken: true, runs: []auditCommand{command(`zizmor `, "zizmor"), zizmorFailed}},
			{name: "actionlint", runs: []auditCommand{command(`actionlint`, "actionlint")}},
			// Rows 6 to 15 (PR 5c). The job's token only on the steps that read
			// GitHub's API: scorecard, the release's verification and the
			// supply-chain reads; no container step holds it.
			{
				name: "OpenSSF scorecard", githubToken: true,
				runs:     []auditCommand{scorecardOut, scorecardRun},
				refuses:  []auditCommand{scorecardPublish},
				order:    []auditCommand{scorecardOut, scorecardRun},
				outcomes: []auditOutcome{{run: scorecardOutcome, check: "scorecard"}},
			},
			{
				name: "Verify the release's files, signature and attestations", githubToken: true,
				check: "release-integrity", budget: true, verifies: true,
				runs: []auditCommand{
					tagRead, tagShape, releaseDownloadOutcome, releaseFileGate, checksumFilter, checksumFiltered,
					tagDigestShape, latestDigestShape, latestReleaseShape, latestRecord, tagDigestBranch,
				},
				order: []auditCommand{
					tagRead, tagShape, releaseDownloadOutcome, releaseFileGate, checksumFilter, checksumFiltered,
					tagDigestShape, latestDigestShape, latestReleaseShape, latestRecord, tagDigestBranch,
				},
			},
			{
				name:     "Pull the image anonymously by digest",
				check:    "image-pull",
				runs:     []auditCommand{anonymousConfig, recordedDigest, imageDigestShape, imagePull},
				order:    []auditCommand{anonymousConfig, recordedDigest, imageDigestShape, imagePull},
				outcomes: []auditOutcome{{run: pullOutcome, check: "image-pull"}},
			},
			{
				name:   "govulncheck on the release binaries",
				check:  "binary-vulnerabilities",
				budget: true,
				runs:   []auditCommand{binaryGovulncheck, unpackTar, unpackZip, binaryFind},
				order:  []auditCommand{binaryFind, binaryGovulncheck},
			},
			{
				name: "trivy on the image and its SBOM", runs: trivyScans,
				outcomes: []auditOutcome{{run: trivyOutcomes[0], check: "image-scan"}, {run: trivyOutcomes[1], check: "image-scan"}},
			},
			{
				name:    "Check the image's runtime settings",
				check:   "image-runtime",
				runs:    []auditCommand{recordedDigest, imageDigestShape, imageInspect, runtimeCreate, runtimeListing},
				refuses: []auditCommand{anyTmpfs},
				order:   []auditCommand{recordedDigest, imageDigestShape, imageInspect, runtimeCreate, runtimeListing},
			},
			{
				name:    "Run the CLI on hostile inventory files",
				check:   "hostile-inventories",
				budget:  true,
				runs:    []auditCommand{recordedDigest, imageDigestShape, imageInspect, hostileCorpus, hostileCopy, hostileCommit, hostileCreate},
				refuses: []auditCommand{anyTmpfs},
				order:   []auditCommand{recordedDigest, imageDigestShape, imageInspect, hostileCorpus, hostileCopy, hostileCommit, hostileCreate},
			},
			{
				name: "Fuzz the committed Fuzz targets",
				// Its staging reads the tree, the module cache and the Go image
				// (review round I of PR 5c, M2): bounded like row 13's, and its
				// loop stopped by a budget.
				check:  "fuzzing",
				budget: true,
				env:    []auditRead{{key: "GOFLAGS", value: "-mod=readonly"}, {key: "GOPROXY", value: "off"}},
				envMatch: []auditEnvMatch{{
					key: "GO_IMAGE", re: goImageDigest,
					want: `a tag and its @sha256:<64 hex> digest, so the image is the one Renovate pinned`,
				}},
				runs:  []auditCommand{fuzzDiscovery, goImage, fuzzShape, fuzzCreate, fuzzTmpfs, fuzzEnv, fuzzTime},
				order: []auditCommand{fuzzDiscovery, goImage, fuzzShape, fuzzCreate, fuzzTmpfs, fuzzEnv},
			},
			{
				name: "Supply-chain reads", githubToken: true,
				runs:  append([]auditCommand{readAPIFilter, readAPIJq, rulesetShape, rulesetRead}, namesReads...),
				order: []auditCommand{readAPIFilter, rulesetShape, rulesetRead},
			},
			{
				name: "Encrypt the checks' results to the model key",
				env:  []auditRead{{key: "AUDIT_MODEL_PUBLIC_KEY", value: varValue("AUDIT_MODEL_PUBLIC_KEY")}},
				runs: []auditCommand{command(`age --encrypt --recipient "\$\{AUDIT_MODEL_PUBLIC_KEY\}"`, `age --encrypt --recipient "${AUDIT_MODEL_PUBLIC_KEY}"`)},
			},
			{name: "Upload the checks' ciphertext", uses: auditUpload, artifact: auditChecksArtifact},
		},
	},
	{
		key:              "model",
		name:             "Model",
		needs:            "checks",
		permissions:      []string{"contents", "actions"},
		downloadsModules: "Download the Go modules",
		steps: []auditStep{
			{name: "Check out the audited commit", uses: auditCheckout},
			{name: "Download the Go modules", runs: []auditCommand{modDownload}},
			// The step that reads the Claude Code pin file into its outputs, for
			// the install step's env: (test/lint, issue #277 round J).
			{name: "Read the Claude Code pin", runs: []auditCommand{command(`\.github/claude-code\.yaml`, ".github/claude-code.yaml")}},
			{
				name:            "Install the pinned tools",
				runs:            []auditCommand{checksumCheck, command(`dpkg -i `, "dpkg -i"), installError},
				guardsChecksums: true,
			},
			{
				name: "Load the AppArmor profile for bubblewrap",
				runs: []auditCommand{command(`apparmor_parser -r \.github/security-audit/bwrap\.apparmor`, "apparmor_parser -r .github/security-audit/bwrap.apparmor")},
			},
			{name: "Check the GNU coreutils", runs: []auditCommand{command(`GNU coreutils`, "a check for GNU coreutils")}},
			{
				name: "Build and install the audit's command wrapper",
				runs: []auditCommand{
					command(`go build -trimpath .*\./ci/auditexec`, "go build -trimpath ./ci/auditexec"),
					command(`install -o root -g root -m 0555 .*/srv/audit/bin/audit-exec`, "install -o root -g root -m 0555 ... /srv/audit/bin/audit-exec"),
				},
			},
			{
				name: "List the run's artifacts and download the checks' ciphertext", githubToken: true,
				runs:    []auditCommand{artifactsModel, modelArtifactError, artifactFile, artifactID, artifactDigest, artifactShape, artifactZip, zipDigest, zipUnpack, zipOneFile},
				refuses: []auditCommand{secondListing},
				order:   []auditCommand{artifactsModel, artifactFile, artifactID, artifactDigest, artifactShape, artifactZip, zipDigest, zipUnpack, zipOneFile},
			},
			{
				name: "Decrypt the checks' results into the audit directory",
				env:  []auditRead{{key: "AUDIT_MODEL_PRIVATE_KEY", value: secretValue("AUDIT_MODEL_PRIVATE_KEY")}},
				runs: []auditCommand{
					stdinIdentity,
					command(`tar -tvf "\$\{RUNNER_TEMP\}/checks\.tar"`, "tar -tvf of the archive, whose entries' types are checked before it is unpacked"),
					command(`tar -C /srv/audit/checks --no-same-owner --no-same-permissions -xf `, "tar -C /srv/audit/checks --no-same-owner --no-same-permissions -xf: never the archive's owners or modes"),
					command(`rm -f "\$\{RUNNER_TEMP\}/checks\.tar" "\$\{RUNNER_TEMP\}/checks\.types" "\$\{RUNNER_TEMP\}/checks\.names"`,
						`rm -f of the step's temporary files "${RUNNER_TEMP}/checks.tar", checks.types and checks.names`),
				},
			},
			{name: "Copy the checkout into the audit directory", runs: []auditCommand{command(`/srv/audit/checkout`, "/srv/audit/checkout")}},
			{
				name: "Stop Docker and remove the runner user's sudo rule",
				runs: append([]auditCommand{
					command(`systemctl stop docker`, "systemctl stop docker"),
					command(`rm -f /etc/sudoers\.d/runner`, "rm -f /etc/sudoers.d/runner"),
					command(`sudo -n true`, "a check that sudo -n true fails"),
					hardeningError,
				}, hardeningCommands...),
				// The branch reads the three commands' recorded failure, so it
				// comes after them.
				order: append(append([]auditCommand{}, hardeningCommands...), hardeningError),
			},
			{
				name:     "Run the audit in Claude Code's sandbox",
				env:      []auditRead{{key: "CLAUDE_CODE_OAUTH_TOKEN", value: secretValue("CLAUDE_CODE_OAUTH_TOKEN")}},
				envNames: []string{"CLAUDE_CODE_OAUTH_TOKEN", "SHIFT2EU_AUDIT_CANARY", "CLAUDE_CODE_PIN_INTEGRITY"},
				runs: []auditCommand{
					command(`auditrun"? -claude-tarball \S+ -claude-integrity \S+`, "auditrun -claude-tarball <path> -claude-integrity <integrity>"),
					// The launcher writes the result bytes it checked, and only
					// those, out of the sandbox's reach (review round 1, R1).
					command(`auditrun"? .* -result-out "\$\{RUNNER_TEMP\}/result\.json"`, `auditrun ... -result-out "${RUNNER_TEMP}/result.json", the checked result's bytes`),
					runError,
				},
			},
			{
				name: "Encrypt the audit's result to the publish key",
				env:  []auditRead{{key: "AUDIT_PUBLISH_PUBLIC_KEY", value: varValue("AUDIT_PUBLISH_PUBLIC_KEY")}},
				runs: []auditCommand{
					command(`age --encrypt --recipient "\$\{AUDIT_PUBLISH_PUBLIC_KEY\}" --output "\$\{RUNNER_TEMP\}/model-result\.age" "\$\{RUNNER_TEMP\}/result\.json"`,
						`age --encrypt --recipient "${AUDIT_PUBLISH_PUBLIC_KEY}" --output "${RUNNER_TEMP}/model-result.age" "${RUNNER_TEMP}/result.json", the launcher's checked bytes`),
				},
				refuses: []auditCommand{command(`/srv/audit/work/result\.json`,
					"/srv/audit/work/result.json, the work area's result read again by its path, want ${RUNNER_TEMP}/result.json, the bytes the launcher checked (-result-out)")},
			},
			{name: "Upload the model's ciphertext", uses: auditUpload, artifact: auditModelArtifact},
		},
	},
	{
		key:         "publish",
		name:        "Publish",
		needs:       "model",
		permissions: []string{"contents", "actions", "attestations"},
		steps: []auditStep{
			{name: "Check out the audited commit", uses: auditCheckout},
			{name: "Download the pinned tools and verify their checksums", runs: []auditCommand{checksumCheck}, guardsChecksums: true},
			{
				name: "List the run's artifacts and download the model's ciphertext", githubToken: true,
				runs:    []auditCommand{artifactsPublic, publishArtifactError, artifactFile, artifactID, artifactDigest, artifactShape, artifactZip, zipDigest, zipUnpack, zipOneFile},
				refuses: []auditCommand{secondListing},
				order:   []auditCommand{artifactsPublic, artifactFile, artifactID, artifactDigest, artifactShape, artifactZip, zipDigest, zipUnpack, zipOneFile},
			},
			{
				name: "Decrypt the model's result",
				env:  []auditRead{{key: "AUDIT_PUBLISH_PRIVATE_KEY", value: secretValue("AUDIT_PUBLISH_PRIVATE_KEY")}},
				runs: []auditCommand{stdinIdentity},
			},
			{name: "Check the image digest and its release", githubToken: true, runs: []auditCommand{command(`auditpublish"? image `, "auditpublish image")}},
			{
				name: "Verify the image's provenance attestation", githubToken: true,
				runs: []auditCommand{
					// verify holds every flag: one contiguous command, so no flag
					// needs a pattern of its own.
					imageLine, digestRead, tagShape, emptyDigest, digestShape, verify,
				},
				order: []auditCommand{imageLine, digestRead, tagShape, emptyDigest, digestShape, verify},
			},
			// The scan runs before the App's token is minted: a result gitleaks
			// refuses has no write token minted for it (review round 1, A6).
			{
				name: "Scan the text it is about to publish",
				runs: []auditCommand{
					command(`gitleaks stdin `, "gitleaks stdin"),
					command(`gitleaks stdin .*--ignore-gitleaks-allow`, "gitleaks stdin --ignore-gitleaks-allow"),
					command(`gitleaks stdin .*--redact`, "gitleaks stdin --redact"),
				},
			},
			{
				name: "Mint the audit App's token",
				uses: auditAppToken,
				with: []auditRead{
					{key: "client-id", value: varValue("AUDIT_APP_CLIENT_ID")},
					{key: "private-key", value: secretValue("AUDIT_APP_PRIVATE_KEY")},
				},
			},
			{
				name: "Publish the report",
				env: []auditRead{
					{key: "GH_TOKEN", value: "${{ " + auditAppTokenOutput + " }}"},
					{key: "AUDIT_FINGERPRINT_KEY", value: secretValue("AUDIT_FINGERPRINT_KEY")},
				},
				runs: []auditCommand{command(`auditpublish"? publish `, "auditpublish publish"), publishError},
			},
		},
	},
}

// auditAppPermissions are the App token step's permission-* inputs, exactly
// ADR 0040's Decision 1: the audit App's four write permissions, nothing else.
var auditAppPermissions = []string{
	"permission-contents", "permission-pull-requests", "permission-issues", "permission-security-events",
}

// auditReadWhere says, for each credential and variable, where the workflow
// reads it: the one place a message names.
var auditReadWhere = map[string]string{ //nolint:gosec // the names of secrets and where they are read, no credential
	"secrets.CLAUDE_CODE_OAUTH_TOKEN":   "in the model step's step-level env: alone",
	"secrets.AUDIT_MODEL_PRIVATE_KEY":   "in the model job's decrypt step's step-level env: alone",
	"secrets.AUDIT_PUBLISH_PRIVATE_KEY": "in the publish job's decrypt step's step-level env: alone",
	"secrets.AUDIT_APP_PRIVATE_KEY":     "in the publish job's App token step's with: alone",
	"secrets.AUDIT_FINGERPRINT_KEY":     "in the publish step's step-level env: alone",
	"vars.AUDIT_MODEL_PUBLIC_KEY":       "in the checks job's encryption step's step-level env: alone",
	"vars.AUDIT_PUBLISH_PUBLIC_KEY":     "in the model job's encryption step's step-level env: alone",
	"vars.AUDIT_APP_CLIENT_ID":          "in the publish job's App token step's with: alone",
	auditGitHubToken:                    "as GH_TOKEN in the step-level env: of the steps that read GitHub's API with it",
	auditAppTokenOutput:                 "as GH_TOKEN in the publish step's step-level env: alone",
}

// auditCredentialRef matches a credential or variable an expression reads: a
// secret or variable by name, the job's token, a step's token output.
var auditCredentialRef = regexp.MustCompile(`(?i)\b(?:(secrets|vars)\s*\.\s*([A-Za-z0-9_-]+)|(github\s*\.\s*token)\b|steps\s*\.\s*([A-Za-z0-9_-]+)\s*\.\s*outputs\s*\.\s*token\b)`)

// auditInherit matches secrets: inherit, which hands a job every secret.
var auditInherit = regexp.MustCompile(`\bsecrets\s*:\s*inherit\b`)

// auditTrace matches a shell told to print its commands, and so the values
// they expand.
var auditTrace = regexp.MustCompile(`(?:^|[\s;&|])(?:set\s+(?:-[A-Za-z]*x[A-Za-z]*|-o\s+xtrace)|bash\s+-[A-Za-z]*x)`)

// auditModelTools are the programs the model job never runs: every check is
// the checks job's, and no container runs beside the model.
var auditModelTools = regexp.MustCompile("(?:^|[\\s;&|(`\"'/])(docker|podman|gitleaks|govulncheck|osv-scanner|zizmor|actionlint|cosign|trivy|scorecard)(?:$|[\\s;&|)`\"'])")

// auditGoCommand matches a Go command: the word go and an argument.
var auditGoCommand = regexp.MustCompile(`(?:^|[\s;&|(])go\s+\S`)

// auditTrailingComment matches a # at the start of a word, which the shell
// reads as a comment to the end of the line: on a code line it can hide a
// guard or cut a \ continuation (review round 3, H1). A word starts after
// whitespace and right after an operator character, ; & | ( ) < > (review
// round 4, M1: ;#, &&# and |# begin a comment too) and right after a backtick,
// which opens a command substitution whose first word a # comments out
// (review round 5). A # inside a word (${digest#sha256:},
// ${GITLEAKS_VERSION#v}) is code; one after whitespace or an operator
// character inside quotes is refused too, which fails closed.
var auditTrailingComment = regexp.MustCompile(`(?:^|[\s;&|()<>` + "`" + `])#`)

// auditTempPath matches a path under /tmp or /run, which the sandbox's Bash
// can read: the deny list does not name them.
var auditTempPath = regexp.MustCompile(`(?:^|[\s"'=<>:|(])(/tmp|/run)(?:/|$|[\s"';)])`)

// auditVersionLine matches a tool's version pin in an env: block.
var auditVersionLine = regexp.MustCompile(`^(\s+)([A-Z0-9_]+)_VERSION:\s*(\S.*)$`)

// auditRenovate matches the Renovate comment above a pin.
var auditRenovate = regexp.MustCompile(`^\s*# renovate: datasource=(\S+) depName=(\S+)(?: digestVersion=(\S+))?`)

// auditDigestLine matches a SHA-256 pin, and captures its tool's name.
var auditDigestLine = regexp.MustCompile(`^\s+([A-Z0-9_]+?)(?:_DEB)?_SHA256:\s*(.*)$`)

// auditDigestValue is a SHA-256 pin's value: 64 lower-case hex, quoted.
var auditDigestValue = regexp.MustCompile(`^"[0-9a-f]{64}"$`)

// auditPin matches an action pinned to a commit with a version comment.
var auditPin = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+)@([0-9a-f]{40})(?:\s+#\s*(v\S+))?$`)

// AuditWorkflow returns a message for every way src, the scheduled security
// audit's workflow, departs from the shape PRD #175 milestone 2 and ADR 0040
// give it (test/architecture/internal/check/auditworkflow_test.go writes it
// out): a workflow_dispatch with no inputs as its only trigger, the one guard
// condition in every job and in the concurrency group, read-only scopes, three
// jobs whose steps are a closed list in order, two ciphertexts and no other
// hand-off, every secret and variable read in exactly one place, and every
// container of the checks job read-only, networkless, capability-less and
// unprivileged, with no mount, no socket, no registry login and no tmpfs but
// row 14's (PR 5c). Since review round D of PR 5c it also holds that nothing
// the release holds ends a step (timeouts, exit codes and not-run records in
// rows 7 to 13, the checksums filter, the size cap, loop budgets and the
// checks job's steps within its timeout), that no container is loosened past
// its pinned flags, run, entered, or started unless created with the four
// flags, that row 15 keeps names alone, and that every tool download's
// checksum check is a guard that names its tool. Since round E it also holds
// that row 7 records a failed verification as the finding verification-failed,
// that every checks step's own timeouts fit its timeout-minutes, and that the
// model and publish jobs' failures name their kind and runbook step. Since round
// F it also holds every tool run of the checks job under a timeout of its own
// with its exit recorded, and the model job's install of the Claude Code CLI to
// a guard that names its runbook step. Since review round I it also holds
// cosign verify's refusal exits (1 and 10 to 13) as a finding, every loop that
// runs a timeout to a budget, every docker command of the checks job to a
// timeout of its own, row 7's file checks to a download that brought
// checksums.txt and its unresolved digest to a record, rows 6, 8 and 10's
// failed tools to not-run.txt, row 9's binaries to files that hold bytes,
// latest.txt's values to their shapes, and row 15's unexpected ruleset ID to a
// record that ends nothing. A document it cannot read, or an empty one, is
// refused.
func AuditWorkflow(src []byte) []string {
	if strings.TrimSpace(string(src)) == "" {
		return []string{"the audit workflow is empty, so this check cannot tell what it runs"}
	}

	r := &schemaReader{raw: rawLines(src)}
	a := &auditReader{r: r, allowed: map[int][]string{}}

	a.lineRules()
	a.containerStarts()

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		r.fail("read the audit workflow: %v", err)

		return r.messages
	}

	if len(lines) == 0 {
		r.fail("the audit workflow is empty, so this check cannot tell what it runs")

		return r.messages
	}

	a.schema(lines)
	a.reach()
	a.pins()

	return r.messages
}

// auditReader collects the audit workflow's messages and the lines on which a
// credential or variable is read where its step's spec allows it.
type auditReader struct {
	r *schemaReader
	// allowed are, by line number, the references read on that line where the
	// spec allows them.
	allowed map[int][]string
}

// lineRules checks every line on its own: the secrets context in a form no
// list of names can follow, secrets: inherit, a traced shell, in the model
// job's lines a check or a container, and in every other job's lines what no
// container is given (containerRules).
func (a *auditReader) lineRules() {
	model := a.jobLines("model")
	inDocker := false

	for i, line := range a.r.raw {
		num := i + 1

		a.r.messages = append(a.r.messages, secretsContextForms(num, line)...)

		if auditInherit.MatchString(line) {
			a.r.fail("line %d: %q hands a job every secret (secrets: inherit), want each secret named where it is read", num, strings.TrimSpace(line))
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if auditTrace.MatchString(line) {
			a.r.fail("line %d: %q traces its commands (set -x), which prints the values they expand to the public log", num, trimmed)
		}

		if model[num] {
			if m := auditModelTools.FindStringSubmatch(line); m != nil {
				a.r.fail("line %d: the model job runs %s (%q): it runs no check, no container and no release code, which are the checks job's (ADR 0040)", num, m[1], trimmed)
			}

			// The model job runs no container at all; its own rule refuses one.
			inDocker = false

			continue
		}

		inDocker = a.containerRules(num, line, inDocker)
	}
}

// auditDockerWord matches a docker command on a line.
var auditDockerWord = regexp.MustCompile("(?:^|[\\s;&|(`\"'])docker\\s")

// auditDockerRefused are the docker subcommands the workflow never runs, read
// after docker's global flags and its container or image management word
// (review round D of PR 5c, B3): a registry login (the image is pulled
// anonymously), run, which starts a container in one step with whatever flags
// it is given, and exec, which runs a command in a container that is already
// running.
var auditDockerRefused = []string{"login", "run", "exec"}

// auditDockerGlobalValue are docker's global flags that take a value, which
// a subcommand is read after.
var auditDockerGlobalValue = []string{"--config", "-c", "--context", "-H", "--host", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey"}

// The flags no container of the workflow is given with any value but its
// one (review round D of PR 5c, B2), and those it is given with none: an
// unconfined security profile, an environment file, a cgroup namespace,
// another network (container:<id> included), a sysctl or another user.
var (
	auditSecurityOpt = regexp.MustCompile(`(?:^|\s)--security-opt[ =]"?([^\s"]*)`)
	auditNetworkOpt  = regexp.MustCompile(`(?:^|\s)--(?:network|net)[ =]"?([^\s"]*)`)
	auditUserOpt     = regexp.MustCompile(`(?:^|\s)--user[ =]"?([^\s"]*)`)
	auditShortUser   = regexp.MustCompile(`(?:^|\s)-u[ ]+"?([^\s"]*)`)
	auditFlagRefused = regexp.MustCompile(`(?:^|\s)(--env-file|--cgroupns|--sysctl)(?:[ =]|$)`)
)

// auditContainerRisk matches what no container of the workflow is given, on
// any line: a volume or a mount, privileges, a host namespace, a capability or
// a device. auditShortVolume is -v, read on a docker command's lines alone,
// where it is a volume and not some other program's flag.
var (
	auditContainerRisk = regexp.MustCompile(`(?:^|[\s;&|("'])(--volume|--volumes-from|--mount|--privileged|--(?:network|net|pid|ipc|userns|uts)[ =]host|--cap-add|--device)(?:$|[\s=;&|)"'])`)
	auditShortVolume   = regexp.MustCompile(`(?:^|\s)(-v)(?:$|\s)`)
	auditTmpfsValue    = regexp.MustCompile(`(?:^|\s)--tmpfs[ =](\S+)`)
)

// containerRules refuses, on a line outside the model job, what no container
// of the workflow is given (PR 5c, ADR 0040): its inputs reach it by docker cp
// into a staging container, so it needs no mount, no volume and no socket, and
// it gets no privilege, no host network and no tmpfs but row 14's. inDocker
// says the line before was a docker command continued with \; it returns
// whether this line is one.
func (a *auditReader) containerRules(num int, line string, inDocker bool) bool {
	trimmed := strings.TrimSpace(line)
	docker := inDocker || auditDockerWord.MatchString(line)

	if strings.Contains(line, "docker.sock") {
		a.r.fail("line %d: %q names the Docker socket (docker.sock): no step hands a container, or anything else, the Docker daemon (ADR 0040)", num, trimmed)
	}

	for _, spelled := range dockerRefusedCommands(line) {
		if strings.HasSuffix(spelled, " login") {
			a.r.fail("line %d: %q runs %s: the image is pulled anonymously, with no registry credential (ADR 0040)", num, trimmed, spelled)
		} else {
			a.r.fail("line %d: %q runs %s, want docker create with --read-only --network none --cap-drop ALL --security-opt no-new-privileges and docker start: "+
				"no container is run or entered any other way (ADR 0040)", num, trimmed, spelled)
		}
	}

	if m := auditContainerRisk.FindStringSubmatch(line); m != nil {
		a.r.fail("line %d: %q gives a container %s: no container gets a mount, a volume, privileges, a host namespace, a capability or a device; its inputs reach it by docker cp (ADR 0040)", num, trimmed, m[1])
	}

	a.containerOptions(num, line, trimmed, docker)

	if docker {
		if auditShortVolume.MatchString(line) {
			a.r.fail("line %d: %q gives a container a volume (-v): no container gets a mount or a volume; its inputs reach it by docker cp (ADR 0040)", num, trimmed)
		}
	}

	for _, m := range auditTmpfsValue.FindAllStringSubmatch(line, -1) {
		if "--tmpfs "+m[1] != auditFuzzTmpfs {
			a.r.fail("line %d: %q mounts the tmpfs %s, want only row 14's %s", num, trimmed, m[1], auditFuzzTmpfs)
		}
	}

	return docker && strings.HasSuffix(line, `\`)
}

// containerOptions refuses, on a line outside the model job, a container
// option the workflow gives no container, or gives only one value (review
// round D of PR 5c, B2): a security option but no-new-privileges, a network
// but none, a user but 65534:65534, an environment file, a cgroup namespace
// or a sysctl. -u is read on a docker command's lines alone, where it is a
// user and not some other program's flag.
func (a *auditReader) containerOptions(num int, line, trimmed string, docker bool) {
	only := []struct {
		re          *regexp.Regexp
		flag, value string
	}{
		{auditSecurityOpt, "--security-opt", "no-new-privileges"},
		{auditNetworkOpt, "--network", "none"},
		{auditUserOpt, "--user", "65534:65534"},
	}

	if docker {
		only = append(only, struct {
			re          *regexp.Regexp
			flag, value string
		}{auditShortUser, "-u", "65534:65534"})
	}

	for _, o := range only {
		for _, m := range o.re.FindAllStringSubmatch(line, -1) {
			if m[1] != o.value {
				a.r.fail("line %d: %q gives a container %s %s: the one value of %s is %s (ADR 0040)", num, trimmed, o.flag, m[1], o.flag, o.value)
			}
		}
	}

	for _, m := range auditFlagRefused.FindAllStringSubmatch(line, -1) {
		a.r.fail("line %d: %q gives a container %s: no container gets an environment file, a cgroup namespace or a sysctl (ADR 0040)", num, trimmed, m[1])
	}
}

// dockerRefusedCommands returns, for every docker command on line whose
// subcommand is one of auditDockerRefused, the command as it is spelled from
// docker to the subcommand: docker's global flags and the container or image
// management word are read past, so that docker --context x run and docker
// container exec are what they are.
func dockerRefusedCommands(line string) []string {
	var out []string

	for _, loc := range auditDockerWord.FindAllStringIndex(line, -1) {
		words := strings.Fields(line[loc[1]:])
		spelled := []string{"docker"}

		i := 0
		for i < len(words) && strings.HasPrefix(words[i], "-") {
			spelled = append(spelled, words[i])

			if slices.Contains(auditDockerGlobalValue, words[i]) && i+1 < len(words) {
				i++
				spelled = append(spelled, words[i])
			}

			i++
		}

		if i < len(words) && (words[i] == "container" || words[i] == "image") {
			spelled = append(spelled, words[i])
			i++
		}

		if i < len(words) && slices.Contains(auditDockerRefused, words[i]) {
			out = append(out, strings.Join(append(spelled, words[i]), " "))
		}
	}

	return out
}

// auditCreateAssign matches a variable assigned the ID of a container a
// docker create makes, with or without a timeout, and what follows create.
var auditCreateAssign = regexp.MustCompile(`(?:^|[\s;&|])([A-Za-z_][A-Za-z0-9_]*)="\$\((?:timeout \S+ )?docker (?:container )?create( [^\n]*)?`)

// auditStart matches a docker start and its arguments.
var auditStart = regexp.MustCompile("(?:^|[\\s;&|(`\"'])docker (?:container )?start( [^\\n]*)?")

// auditStartVar matches a variable a docker start names.
var auditStartVar = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// containerStarts holds every docker start outside the model job to a
// container that a docker create made with the four flags (review round D of
// PR 5c, B4): a variable every create assigns with --read-only --network none
// --cap-drop ALL --security-opt no-new-privileges first. A staging container,
// created with no flag, only receives inputs by docker cp, and is never
// started.
func (a *auditReader) containerStarts() {
	model := a.jobLines("model")
	hardened := map[string]bool{}

	code := func(i int) bool {
		return !model[i+1] && !strings.HasPrefix(strings.TrimSpace(a.r.raw[i]), "#")
	}

	for i, line := range a.r.raw {
		if !code(i) {
			continue
		}

		for _, m := range auditCreateAssign.FindAllStringSubmatch(line, -1) {
			ok, seen := hardened[m[1]]
			flagged := strings.HasPrefix(m[2], strings.TrimPrefix(auditContainerFlags, "docker create")+" ")
			hardened[m[1]] = flagged && (ok || !seen)
		}
	}

	for i, line := range a.r.raw {
		if !code(i) {
			continue
		}

		for _, m := range auditStart.FindAllStringSubmatch(line, -1) {
			v := auditStartVar.FindStringSubmatch(m[1])

			switch {
			case v == nil:
				a.r.fail("line %d: %q runs docker start of no container a variable names, want ${cid}, the container a docker create made with %s (ADR 0040)",
					i+1, strings.TrimSpace(line), strings.TrimPrefix(auditContainerFlags, "docker create "))
			case !hardened[v[1]]:
				a.r.fail("line %d: %q runs docker start of ${%s}, which no docker create made with %s first: only such a container is started, and a staging container never is (ADR 0040)",
					i+1, strings.TrimSpace(line), v[1], strings.TrimPrefix(auditContainerFlags, "docker create "))
			}
		}
	}
}

// auditSection is one job of the workflow as its raw lines hold it: its key
// and the indexes of its lines in raw, from its key's line to the line before
// the next job's key.
type auditSection struct {
	key        string
	start, end int
}

// sections returns the jobs as the raw lines hold them, so that a line the
// YAML reader cannot place is still its job's, and a job key that appears twice
// is two sections.
func (a *auditReader) sections() []auditSection {
	var out []auditSection

	inJobs := false

	for i, line := range a.r.raw {
		if line == "jobs:" || strings.HasPrefix(line, "jobs: ") {
			inJobs = true

			continue
		}

		if !inJobs || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}

		if k, _, ok := yamlread.KeyValue(strings.TrimSpace(line)); ok {
			if len(out) > 0 {
				out[len(out)-1].end = i
			}

			out = append(out, auditSection{key: k, start: i, end: len(a.r.raw)})
		}
	}

	return out
}

// jobLines returns the line numbers of every section of the job key.
func (a *auditReader) jobLines(key string) map[int]bool {
	lines := map[int]bool{}

	for _, s := range a.sections() {
		if s.key != key {
			continue
		}

		for i := s.start; i < s.end; i++ {
			lines[i+1] = true
		}
	}

	return lines
}

// keys reads lines as a block mapping and reports every key outside want and
// every key of required it lacks. It returns the entries it read, nil when it
// cannot read them.
func (a *auditReader) keys(lines []yamlread.Line, path string, want, required []string) map[string]yamlread.Entry {
	if lines == nil {
		return nil
	}

	entries, err := yamlread.Mapping(lines)
	if err != nil {
		a.r.fail("%s: %v", path, err)

		return nil
	}

	got := make(map[string]yamlread.Entry, len(entries))

	for _, e := range entries {
		got[e.Key] = e

		if !slices.Contains(want, e.Key) {
			a.r.fail("line %d: %s holds the key %q, want only %s", e.Num, path, e.Key, strings.Join(want, ", "))
		}
	}

	for _, key := range required {
		if _, found := got[key]; !found {
			a.r.fail("%s holds no %q, want %s", path, key, strings.Join(required, ", "))
		}
	}

	return got
}

// exact checks that e's value is the one scalar want.
func (a *auditReader) exact(e yamlread.Entry, path, want, why string) {
	value, err := yamlread.Single(e)
	if err != nil {
		a.r.fail("%s: %v", path, err)

		return
	}

	if value != want {
		a.r.fail("line %d: %s is %q, want %s%s", e.Num, path, value, want, why)
	}
}

// schema checks the workflow's frame and its jobs.
func (a *auditReader) schema(lines []yamlread.Line) {
	top := a.keys(lines, "the audit workflow", []string{"name", "on", "permissions", "concurrency", "jobs"},
		[]string{"name", "on", "permissions", "concurrency", "jobs"})
	if top == nil {
		return
	}

	if e, ok := top["name"]; ok {
		a.exact(e, "the audit workflow's name", auditWorkflowName, "")
	}

	if e, ok := top["on"]; ok {
		a.triggers(e)
	}

	if e, ok := top["permissions"]; ok {
		a.r.rawValue(e, "the audit workflow's top-level permissions", "{}")
	}

	if e, ok := top["concurrency"]; ok {
		c := a.keys(a.r.block(e, "concurrency"), "concurrency", []string{"group", "cancel-in-progress"}, []string{"group", "cancel-in-progress"})
		if g, ok := c["group"]; ok {
			a.exact(g, "concurrency: group", auditGroup, ": the guard condition, and a group of its own (skipped-<run ID>) for a run the guard skips")
		}

		if p, ok := c["cancel-in-progress"]; ok {
			a.exact(p, "concurrency: cancel-in-progress", "false", ": a run is never cancelled")
		}
	}

	e, ok := top["jobs"]
	if !ok {
		return
	}

	keys := make([]string, 0, len(auditJobs))
	for _, j := range auditJobs {
		keys = append(keys, j.key)
	}

	jobs := a.keys(a.r.block(e, "jobs"), "jobs", keys, keys)

	for _, spec := range auditJobs {
		if job, ok := jobs[spec.key]; ok {
			a.job(job, spec)
		}
	}
}

// triggers checks on:: workflow_dispatch alone, with nothing below it.
func (a *auditReader) triggers(e yamlread.Entry) {
	on := a.keys(a.r.block(e, "on"), "on", []string{"workflow_dispatch"}, []string{"workflow_dispatch"})

	dispatch, ok := on["workflow_dispatch"]
	if !ok {
		return
	}

	if dispatch.Value != "" {
		a.r.fail("line %d: on: workflow_dispatch is %q, want nothing: the dispatch takes no inputs", dispatch.Num, dispatch.Value)
	}

	if len(dispatch.Block) == 0 {
		return
	}

	entries, err := yamlread.Mapping(dispatch.Block)
	if err != nil {
		a.r.fail("on: workflow_dispatch: %v", err)

		return
	}

	for _, d := range entries {
		a.r.fail("line %d: on: workflow_dispatch holds %q, want nothing: the dispatch takes no inputs, so no caller chooses a value of the run", d.Num, d.Key)
	}
}

// job checks one job against its spec.
func (a *auditReader) job(e yamlread.Entry, spec auditJob) {
	path := "jobs: " + spec.key

	want := []string{"name", "runs-on", "timeout-minutes", "if", "environment", "permissions", "steps"}
	if spec.needs != "" {
		want = append(want, "needs")
	}

	keys := a.keys(a.r.block(e, path), path, want, want)
	if keys == nil {
		return
	}

	if v, ok := keys["name"]; ok {
		a.exact(v, path+": name", spec.name, "")
	}

	if v, ok := keys["needs"]; ok {
		a.exact(v, path+": needs", spec.needs, ": the jobs run in order, each on the one before's ciphertext")
	}

	if v, ok := keys["runs-on"]; ok {
		a.exact(v, path+": runs-on", auditRunner, ", never a moving image: the wrapper's option analysis assumes its GNU coreutils (ADR 0040)")
	}

	if v, ok := keys["timeout-minutes"]; ok {
		a.timeout(v, path, auditJobLimit-1)
	}

	if v, ok := keys["if"]; ok {
		a.exact(v, path+": if", auditIf, ": the guard condition, verbatim")
	} else {
		a.r.fail("%s has no if:, want the guard condition %s", path, auditIf)
	}

	if v, ok := keys["environment"]; ok {
		a.exact(v, path+": environment", auditEnvironment, ": the environment limited to main")
	}

	if v, ok := keys["permissions"]; ok {
		a.permissions(v, path, spec.permissions)
	}

	if v, ok := keys["steps"]; ok {
		steps := a.steps(v, path, spec)

		if t, ok := keys["timeout-minutes"]; ok && spec.stepsWithinJob {
			a.stepsWithin(t, path, steps)
		}
	}
}

// auditJobOverhead is the time a job's timeout counts beyond its steps: the
// runner's job set-up and the actions' post steps.
const auditJobOverhead = 5

// stepsWithin holds a job's steps' timeout-minutes, and auditJobOverhead, to
// the job's own (review round D of PR 5c, A4): when they add up to more, the
// job's timeout can fire while a step is still within its own, and then no
// later step runs at all.
func (a *auditReader) stepsWithin(t yamlread.Entry, path string, steps []auditParsedStep) {
	value, err := yamlread.Single(t)
	if err != nil {
		return
	}

	job, err := strconv.Atoi(value)
	if err != nil {
		return
	}

	sum := 0

	for _, s := range steps {
		e, ok := s.keys["timeout-minutes"]
		if !ok {
			continue
		}

		v, err := yamlread.Single(e)
		if err != nil {
			continue
		}

		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}

		sum += n
		if sum+auditJobOverhead > job {
			a.r.fail("line %d: %s: its steps' timeout-minutes add up to %d at the step %q, which with the job's %d minutes of set-up and post steps is past the job's timeout-minutes of %d: "+
				"the steps stay within the job's timeout, so that a step's own timeout fires before the job's (ADR 0040)", e.Num, path, sum, s.name, auditJobOverhead, job)

			return
		}
	}
}

// timeout checks a timeout-minutes: a whole number of minutes, 1 to most.
func (a *auditReader) timeout(e yamlread.Entry, path string, most int) {
	value, err := yamlread.Single(e)
	if err != nil {
		a.r.fail("%s: timeout-minutes: %v", path, err)

		return
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > most {
		a.r.fail("line %d: %s: timeout-minutes is %q, want a whole number from 1 to %d, below GitHub's limit of %d", e.Num, path, value, most, auditJobLimit)
	}
}

// permissions checks a job's scopes: exactly want, each read.
func (a *auditReader) permissions(e yamlread.Entry, path string, want []string) {
	path += ": permissions"

	got := a.keys(a.r.block(e, path), path, want, want)

	for _, scope := range want {
		v, ok := got[scope]
		if !ok {
			continue
		}

		value, err := yamlread.Single(v)
		if err != nil {
			a.r.fail("%s: %s: %v", path, scope, err)

			continue
		}

		if value != "read" {
			a.r.fail("line %d: %s: %s is %s, want read: no job of the audit writes through GITHUB_TOKEN", v.Num, path, scope, value)
		}
	}
}

// auditParsedStep is one step as read.
type auditParsedStep struct {
	name string
	num  int
	keys map[string]yamlread.Entry
}

// steps checks a job's steps: the closed list, in order, and each step. It
// returns the steps it read.
func (a *auditReader) steps(e yamlread.Entry, path string, spec auditJob) []auditParsedStep {
	block := a.r.block(e, path+": steps")
	if block == nil {
		return nil
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		a.r.fail("%s: steps: %v", path, err)

		return nil
	}

	parsed := make([]auditParsedStep, 0, len(items))

	for i, it := range items {
		stepPath := fmt.Sprintf("%s: step %d (line %d)", path, i+1, it.Num)

		keys := a.keys(it.Lines, stepPath, []string{"name", "id", "uses", "with", "env", "run", "timeout-minutes"}, []string{"name"})
		if keys == nil {
			continue
		}

		name := ""
		if n, ok := keys["name"]; ok {
			value, err := yamlread.Single(n)
			if err != nil {
				a.r.fail("%s: name: %v", stepPath, err)
			}

			name = value
		}

		parsed = append(parsed, auditParsedStep{name: name, num: it.Num, keys: keys})
	}

	a.stepOrder(path, parsed, spec)

	downloaded := false

	for _, s := range parsed {
		var want *auditStep

		for i := range spec.steps {
			if spec.steps[i].name == s.name {
				want = &spec.steps[i]
			}
		}

		a.step(s, fmt.Sprintf("%s: step %q (line %d)", path, s.name, s.num), want, downloaded)

		// A script the reader cannot follow was reported by step already.
		if run, ok := s.keys["run"]; ok {
			text, err := yamlread.LiteralBlock(a.r.raw, run)
			if err == nil && modDownload.re.MatchString(text) {
				downloaded = true
			}

			if err == nil && spec.stepsWithinJob {
				a.fit(withoutComments(strings.Split(text, "\n")), fmt.Sprintf("%s: step %q (line %d)", path, s.name, s.num), stepMinutes(s))
			}

			// The download step installs the tools and runs none of them on the
			// repository; its one run, trivy's database download, has a timeout.
			if err == nil && spec.boundsTools && (want == nil || !want.guardsChecksums) {
				a.toolRuns(withoutComments(strings.Split(text, "\n")), fmt.Sprintf("%s: step %q (line %d)", path, s.name, s.num))
				a.dockerRuns(withoutComments(strings.Split(text, "\n")), fmt.Sprintf("%s: step %q (line %d)", path, s.name, s.num))
			}
		}
	}

	return parsed
}

// stepMinutes returns a step's timeout-minutes, 0 when it has none the reader
// can follow (step reports that).
func stepMinutes(s auditParsedStep) int {
	t, ok := s.keys["timeout-minutes"]
	if !ok {
		return 0
	}

	v, err := yamlread.Single(t)
	if err != nil {
		return 0
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}

	return n
}

// stepOrder holds the job's steps to the closed list: every name on it, in its
// order, none twice, none missing.
func (a *auditReader) stepOrder(path string, got []auditParsedStep, spec auditJob) {
	index := map[string]int{}
	for i, s := range spec.steps {
		index[s.name] = i
	}

	seen := map[string]bool{}
	last, lastName := -1, ""

	for _, s := range got {
		i, known := index[s.name]
		if !known {
			a.r.fail("line %d: %s holds the step %q, which is no step of the %s job: its steps are a closed list (ADR 0040)", s.num, path, s.name, spec.key)

			continue
		}

		if seen[s.name] {
			a.r.fail("line %d: %s holds the step %q twice", s.num, path, s.name)

			continue
		}

		seen[s.name] = true

		if i < last {
			a.r.fail("line %d: %s runs the step %q after %q, want it before", s.num, path, s.name, lastName)

			continue
		}

		last, lastName = i, s.name
	}

	for _, s := range spec.steps {
		if !seen[s.name] {
			a.r.fail("%s holds no step %q", path, s.name)
		}
	}

	if spec.downloadsModules != "" && !seen[spec.downloadsModules] {
		a.r.fail("%s has no step %q that runs go mod download, after which every Go command runs offline", path, spec.downloadsModules)
	}
}

// step checks one step: its timeout, its action or script, and what its spec,
// when it has one, wants of it.
func (a *auditReader) step(s auditParsedStep, path string, want *auditStep, downloaded bool) {
	minutes := 0

	if t, ok := s.keys["timeout-minutes"]; ok {
		a.timeout(t, path, auditJobLimit-1)

		if v, err := yamlread.Single(t); err == nil {
			minutes, _ = strconv.Atoi(v)
		}
	} else {
		a.r.fail("%s has no timeout-minutes, want one on every step", path)
	}

	uses, hasUses := s.keys["uses"]
	run, hasRun := s.keys["run"]

	if hasUses == hasRun {
		a.r.fail("%s holds %s, want exactly one of uses: and run:", path, map[bool]string{true: "both uses: and run:", false: "neither uses: nor run:"}[hasUses])
	}

	env := a.env(s, path)

	if want != nil {
		a.reads(path, want, env)
	}

	if hasUses {
		a.action(s, uses, path, want)
	}

	if hasRun {
		a.run(run, path, want, env, downloaded, minutes)
	}
}

// auditVar is one variable of a step's env:.
type auditVar struct {
	name, value string
	num         int
}

// env reads a step's env:, one NAME: value line each.
func (a *auditReader) env(s auditParsedStep, path string) []auditVar {
	e, ok := s.keys["env"]
	if !ok {
		return nil
	}

	lines := a.r.block(e, path+": env")

	var vars []auditVar

	for _, l := range lines {
		name, rest, found := strings.Cut(l.Text, ": ")
		if l.Indent != lines[0].Indent || !found || name == "" || strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			a.r.fail("line %d: %s: env: %q is not one variable this reader understands", l.Num, path, l.Text)

			continue
		}

		value, err := yamlread.Scalar(strings.TrimSpace(rest))
		if err != nil {
			a.r.fail("line %d: %s: env: %s: %v", l.Num, path, name, err)

			continue
		}

		if slices.ContainsFunc(vars, func(v auditVar) bool { return v.name == name }) {
			a.r.fail("line %d: %s: env: %s is repeated", l.Num, path, name)
		}

		vars = append(vars, auditVar{name: name, value: value, num: l.Num})
	}

	return vars
}

// reads checks the credentials and variables the step's spec reads, and
// allows each on its line.
func (a *auditReader) reads(path string, want *auditStep, env []auditVar) {
	for _, read := range want.env {
		found := false

		for _, v := range env {
			if v.name == read.key && v.value == read.value {
				a.allow(v.num, read.value)

				found = true
			}
		}

		if !found {
			a.r.fail("%s has no %s: %s in its step-level env:", path, read.key, read.value)
		}
	}

	if want.githubToken && !hasVar(env, "GH_TOKEN", "${{ "+auditGitHubToken+" }}") {
		a.r.fail("%s has no GH_TOKEN: ${{ %s }} in its step-level env:, want it: the step reads GitHub's API with the job's token", path, auditGitHubToken)
	}

	for _, m := range want.envMatch {
		i := slices.IndexFunc(env, func(v auditVar) bool { return v.name == m.key })
		if i < 0 {
			a.r.fail("%s has no %s in its step-level env:, want %s", path, m.key, m.want)

			continue
		}

		if !m.re.MatchString(env[i].value) {
			a.r.fail("line %d: %s: env: %s is %q, want %s", env[i].num, path, m.key, env[i].value, m.want)
		}
	}

	for _, v := range env {
		if want.githubToken && v.name == "GH_TOKEN" && v.value == "${{ "+auditGitHubToken+" }}" {
			a.allow(v.num, v.value)
		}

		if want.envNames != nil && !slices.Contains(want.envNames, v.name) {
			a.r.fail("line %d: %s sets %s in its env:, want only %s", v.num, path, v.name, strings.Join(want.envNames, ", "))
		}
	}
}

// allow records that the references in value are read on line num where the
// spec allows them.
func (a *auditReader) allow(num int, value string) {
	for _, m := range auditCredentialRef.FindAllStringSubmatch(value, -1) {
		a.allowed[num] = append(a.allowed[num], credentialName(m))
	}
}

// credentialName names the reference auditCredentialRef matched.
func credentialName(m []string) string {
	switch {
	case m[1] != "":
		return strings.ToLower(m[1]) + "." + m[2]
	case m[3] != "":
		return auditGitHubToken
	default:
		return "steps." + m[4] + ".outputs.token"
	}
}

// reach refuses every reference to a secret, a variable, the job's token or a
// step's token output on a line the spec does not allow it on, comments
// included.
func (a *auditReader) reach() {
	for i, line := range a.r.raw {
		num := i + 1

		for _, m := range auditCredentialRef.FindAllStringSubmatch(line, -1) {
			name := credentialName(m)
			if slices.Contains(a.allowed[num], name) {
				continue
			}

			where, known := auditReadWhere[name]
			if !known {
				where = "nowhere"
			}

			a.r.fail("line %d: %q reads %s, which the audit workflow reads %s (ADR 0040)", num, strings.TrimSpace(line), name, where)
		}
	}
}

// action checks a uses: step: its pin, its action, and its with:.
func (a *auditReader) action(s auditParsedStep, uses yamlread.Entry, path string, want *auditStep) {
	fields := strings.Fields(uses.Value)
	if len(fields) == 0 {
		a.r.fail("line %d: %s has an empty uses:", uses.Num, path)

		return
	}

	m := auditPin.FindStringSubmatch(uses.Value)
	action, _, _ := strings.Cut(fields[0], "@")

	switch {
	case m == nil:
		a.r.fail("line %d: %s uses %s, which is not pinned to a 40-hex commit with a version comment", uses.Num, path, fields[0])
	case m[3] == "":
		a.r.fail("line %d: %s uses %s@%s pinned without a version comment (# vX.Y.Z)", uses.Num, path, m[1], m[2])
	}

	if action == "actions/cache" || strings.HasPrefix(action, "actions/cache/") {
		a.r.fail("line %d: %s uses actions/cache: no job restores or saves an Actions cache, which another run could have written (ADR 0040)", uses.Num, path)
	}

	if !slices.Contains([]string{auditCheckout, auditUpload, auditAppToken}, action) {
		a.r.fail("line %d: %s uses %s, want only %s, %s and %s", uses.Num, path, action, auditCheckout, auditUpload, auditAppToken)

		return
	}

	if want != nil && want.uses != action {
		a.r.fail("line %d: %s uses %s, want %s", uses.Num, path, action, want.uses)

		return
	}

	with := s.keys["with"]

	switch action {
	case auditCheckout:
		a.checkout(with, path)
	case auditUpload:
		artifact := ""
		if want != nil {
			artifact = want.artifact
		}

		a.upload(with, path, artifact)
	case auditAppToken:
		a.appToken(with, path, want)
	}
}

// withKeys reads a step's with:, holding only want and every key of required.
func (a *auditReader) withKeys(with yamlread.Entry, path string, want, required []string) map[string]yamlread.Entry {
	if with.Num == 0 {
		a.r.fail("%s has no with:, want %s", path, strings.Join(required, ", "))

		return map[string]yamlread.Entry{}
	}

	keys := a.keys(a.r.block(with, path+": with"), path+": with", want, nil)

	for _, key := range required {
		if _, ok := keys[key]; !ok {
			a.r.fail("%s: with: holds no %s", path, key)
		}
	}

	return keys
}

// checkout checks a checkout's with:: the run's commit, no credential kept,
// no sparse checkout.
func (a *auditReader) checkout(with yamlread.Entry, path string) {
	keys := a.withKeys(with, path, []string{"ref", "persist-credentials", "fetch-depth"}, nil)

	if e, ok := keys["ref"]; ok {
		a.exact(e, path+": with: ref", "${{ github.sha }}", ": the commit the run started on")
	} else {
		a.r.fail("%s checks out without ref: ${{ github.sha }}, the commit the run started on", path)
	}

	if e, ok := keys["persist-credentials"]; ok {
		a.exact(e, path+": with: persist-credentials", "false", ": no credential is left in .git/config")
	} else {
		a.r.fail("%s has no persist-credentials: false, so the token stays in .git/config", path)
	}
}

// upload checks an upload's with:: the artifact's name, its ciphertext, a
// one-day retention.
func (a *auditReader) upload(with yamlread.Entry, path, artifact string) {
	keys := a.withKeys(with, path, []string{"name", "path", "retention-days", "if-no-files-found"},
		[]string{"name", "path", "retention-days", "if-no-files-found"})

	if artifact == "" {
		a.r.fail("%s uploads an artifact, want only %s and %s", path, auditChecksArtifact, auditModelArtifact)

		return
	}

	if e, ok := keys["name"]; ok {
		a.exact(e, path+": with: name", artifact, ": the artifact's one name")
	}

	if e, ok := keys["path"]; ok {
		a.exact(e, path+": with: path", "${{ runner.temp }}/"+artifact+".age", ": the ciphertext alone")
	}

	if e, ok := keys["retention-days"]; ok {
		a.exact(e, path+": with: retention-days", "1", ": the ciphertext is kept one day")
	}

	if e, ok := keys["if-no-files-found"]; ok {
		a.exact(e, path+": with: if-no-files-found", "error", "")
	}
}

// appToken checks the App token step's with:: the App's client ID and key, and
// exactly the four permission-* inputs of Decision 1, each write.
func (a *auditReader) appToken(with yamlread.Entry, path string, want *auditStep) {
	if want == nil {
		return
	}

	keys := a.withKeys(with, path, append([]string{"client-id", "private-key"}, auditAppPermissions...),
		append([]string{"client-id", "private-key"}, auditAppPermissions...))

	for _, read := range want.with {
		e, ok := keys[read.key]
		if !ok {
			continue
		}

		a.exact(e, path+": with: "+read.key, read.value, "")
		a.allow(e.Num, e.Value)
	}

	for _, p := range auditAppPermissions {
		if e, ok := keys[p]; ok {
			a.exact(e, path+": with: "+p, "write", "")
		}
	}
}

// run checks a run: step's script; minutes is the step's timeout-minutes, 0
// when it has none.
func (a *auditReader) run(e yamlread.Entry, path string, want *auditStep, env []auditVar, downloaded bool, minutes int) {
	text, err := yamlread.LiteralBlock(a.r.raw, e)
	if err != nil {
		a.r.fail("%s: run: %v", path, err)

		return
	}

	lines := strings.Split(text, "\n")

	if lines[0] != "set -euo pipefail" {
		a.r.fail("line %d: %s's script starts %q, want set -euo pipefail", e.Num+1, path, lines[0])
	}

	offline := hasVar(env, "GOFLAGS", "-mod=readonly") && hasVar(env, "GOPROXY", "off")

	for i, line := range lines {
		num := e.Num + 1 + i

		if strings.Contains(line, "${{") {
			a.r.fail("line %d: %s's script holds an expression (%q), ${{ in a run: body: a value reaches the shell through env: and files alone", num, path, strings.TrimSpace(line))
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			// A comment line a \ continues joins the command before it as a
			// trailing comment, which cuts the continuation; withoutComments drops
			// it, so the command would look contiguous to the patterns.
			if i > 0 && strings.HasSuffix(lines[i-1], `\`) {
				a.r.fail("line %d: %s's script continues a \\ line with a comment (%q): the comment cuts the command it continues, so the lines after it are no part of it; put a comment before the command", num, path, trimmed)
			}

			continue
		}

		if auditTrailingComment.MatchString(line) {
			a.r.fail("line %d: %s's script holds a trailing comment (%q): a # at the start of a word comments out the rest of the line, a trailing \\ included, so a guard after it is no command; put a comment on a line of its own", num, path, trimmed)
		}

		if m := auditTempPath.FindStringSubmatch(line); m != nil {
			a.r.fail("line %d: %s names a path under %s (%q): no file of the audit lives under /tmp or /run, which the sandbox's Bash can read (ADR 0040)", num, path, m[1], trimmed)
		}

		if downloaded && !offline && auditGoCommand.MatchString(line) {
			if !strings.Contains(line, "GOFLAGS=-mod=readonly") {
				a.r.fail("line %d: %s runs a Go command after the module download without GOFLAGS=-mod=readonly (%q)", num, path, trimmed)
			}

			if !strings.Contains(line, "GOPROXY=off") {
				a.r.fail("line %d: %s runs a Go command after the module download without GOPROXY=off (%q)", num, path, trimmed)
			}
		}
	}

	if want == nil {
		return
	}

	// A comment line is no command: a guard commented out is a guard
	// missing, and a commented copy stands in for no command in order.
	code := withoutComments(lines)

	for _, c := range want.runs {
		if !c.re.MatchString(code) {
			a.r.fail("%s runs no %s", path, c.want)
		}
	}

	for _, c := range want.refuses {
		if loc := c.re.FindStringIndex(code); loc != nil {
			a.r.fail("%s runs %s (%q)", path, c.want, lineAt(code, loc[0]))
		}
	}

	a.order(code, path, want.order)

	if want.check != "" {
		a.bounded(code, path, want.check)
	}

	if want.budget {
		a.budget(code, path, want.check, minutes)
	}

	if want.guardsChecksums {
		a.checksumGuards(code, path)
	}

	if want.verifies {
		a.verifications(code, path, want.check)
		a.releaseBlocks(code, path, want.check)
	}

	for _, o := range want.outcomes {
		a.toolOutcomes(code, path, o)
	}
}

// toolOutcome returns the pattern of a tool run's outcome, from the line after
// its command to its fi (review round I of PR 5c, L3): its exit recorded, then
// rc 124 as the check timed out, in not-run.txt and with its limits line, and
// any other non-zero exit as the check's tool-failed, each on a line of its own.
func toolOutcome(check string) *regexp.Regexp {
	q := regexp.QuoteMeta(check)

	return regexp.MustCompile(`\A[ \t]*echo "\$\{rc\}" > "\$\{out\}/[^"\n]+\.exit"\n` +
		`[ \t]*if \[ "\$\{rc\}" -eq 124 \]; then\n` +
		`[ \t]+echo "` + q + ` timed-out" >> "\$\{results\}/not-run\.txt"\n` +
		`[ \t]+echo "` + q + `[^"\n]*" >> "\$\{results\}/limits\.txt"\n` +
		`[ \t]*elif \[ "\$\{rc\}" -ne 0 \]; then\n` +
		`[ \t]+echo "` + q + ` tool-failed" >> "\$\{results\}/not-run\.txt"\n` +
		`[ \t]*fi(?:\n|\z)`)
}

// toolOutcomes holds every run of o.run to its outcome branch right after it
// (review round I of PR 5c, L3): scorecard, trivy and docker pull exit non-zero
// only on an error, so a run that failed is a check that did not run, recorded
// in not-run.txt, never left to read as clean.
func (a *auditReader) toolOutcomes(code, path string, o auditOutcome) {
	outcome := toolOutcome(o.check)
	lines := strings.Split(code, "\n")

	for i := 0; i < len(lines); i++ {
		if !o.run.re.MatchString(lines[i]) {
			continue
		}

		end := i
		for end < len(lines)-1 && strings.HasSuffix(lines[end], `\`) {
			end++
		}

		if !outcome.MatchString(strings.Join(lines[end+1:], "\n")) {
			a.r.fail(`%s runs %s (%q) without its outcome branch, want right after it, each on a line of its own: echo "${rc}" > "${out}/<name>.exit", `+
				`if [ "${rc}" -eq 124 ]; then, echo "%s timed-out" >> "${results}/not-run.txt", echo "%s...: ..." >> "${results}/limits.txt", `+
				`elif [ "${rc}" -ne 0 ]; then, echo "%s tool-failed" >> "${results}/not-run.txt" and fi: the tool exits non-zero only on an error, `+
				`so a run that timed out or failed is the check not run, never clean (PRD #175 milestone 2, ADR 0040)`,
				path, o.run.want, strings.TrimSpace(lines[i]), o.check, o.check, o.check)
		}

		i = end
	}
}

// releaseBlocks holds row 7's two branches that decide what it verifies
// (review round I of PR 5c): the release's file checks run only inside the
// rel_ok gate the download sets (L2), and the if that verifies the image when
// its digest resolved records, in its else, the verifications it skipped (S4).
func (a *auditReader) releaseBlocks(code, path, check string) {
	lines := strings.Split(code, "\n")

	if i := lineMatching(lines, releaseFileGate.re); i >= 0 {
		_, fi := ifBlock(lines, i)
		body := strings.Join(lines[i+1:max(fi, i+1)], "\n")

		for _, c := range releaseFileChecks {
			if c.re.MatchString(code) && !c.re.MatchString(body) {
				a.r.fail(`%s runs %s outside its if [ "${rel_ok}" -eq 1 ]; then gate: %s's file checks run only when the download brought the release's files and checksums.txt, `+
					`so that a failed download is a check that did not run, never a checksum finding (PRD #175 milestone 2, ADR 0040)`, path, c.want, check)
			}
		}
	} else {
		a.r.fail(`%s runs no if [ "${rel_ok}" -eq 1 ]; then gate, on a line of its own, around the release's file checks: %s's filter, checksum check, listing and attestations `+
			`run only when the download brought the files and checksums.txt, so that a failed download is a check that did not run, never a checksum finding (PRD #175 milestone 2, ADR 0040)`,
			path, check)
	}

	i := lineMatching(lines, tagDigestBranch.re)
	if i < 0 {
		return
	}

	els, fi := ifBlock(lines, i)

	notRun := `echo "` + check + ` tool-failed" >> "${results}/not-run.txt"`
	limits := regexp.MustCompile(`^[ \t]+echo "` + regexp.QuoteMeta(check) + `: [^"\n]+" >> "\$\{results\}/limits\.txt"$`)

	if els < 0 || fi != els+3 || strings.TrimSpace(lines[els+1]) != notRun || !limits.MatchString(lines[els+2]) {
		a.r.fail(`%s's if [ -n "${tag_digest}" ]; then has no else that records what it skipped, want, each on a line of its own: else, %s, `+
			`echo "%s: ..." >> "${results}/limits.txt" naming the signature and attestations not verified, and fi: an image digest that did not resolve is a check that did not run, never left silent `+
			`(PRD #175 milestone 2, ADR 0040)`, path, notRun, check)
	}
}

// lineMatching returns the index of the first of lines re matches, -1 for
// none.
func lineMatching(lines []string, re *regexp.Regexp) int {
	for i, l := range lines {
		if re.MatchString(l) {
			return i
		}
	}

	return -1
}

// ifBlock returns, for the if on lines[i], the index of its else and of its
// fi, each the first such line at the if's own indent; -1 for one it lacks.
// The scripts the check reads indent every block, so a nested if is never at
// the outer one's indent.
func ifBlock(lines []string, i int) (els, fi int) {
	indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
	els = -1

	for j := i + 1; j < len(lines); j++ {
		text := strings.TrimLeft(lines[j], " \t")
		if len(lines[j])-len(text) != indent {
			continue
		}

		switch {
		case text == "fi":
			return els, j
		case text == "else" && els < 0:
			els = j
		}
	}

	return els, -1
}

// auditVerification matches the first line of one of row 7's verifications,
// under its timeout: the checksum check, gh attestation verify, cosign verify
// and cosign verify-attestation.
var auditVerification = regexp.MustCompile(`^[ \t]*(?:\(cd "\$\{rel\}" && )?timeout \S+ (sha256sum --check|gh attestation verify|cosign verify-attestation|cosign verify)\s`)

// auditVerificationKinds are row 7's five verifications, each by its first
// line: every one of them must be there, each with its outcome branch.
var auditVerificationKinds = []auditCommand{
	command(`sha256sum --check --strict "\$\{out\}/checksums\.filtered"`, `the checksum check (sha256sum --check --strict "${out}/checksums.filtered")`),
	command(`gh attestation verify "\$\{file\}"`, `each release file's attestation (gh attestation verify "${file}")`),
	command(`cosign verify "\$\{ref\}"`, `the image's signature (cosign verify "${ref}")`),
	command(`cosign verify-attestation "\$\{ref\}"`, `the image's attestations' signatures (cosign verify-attestation "${ref}")`),
	command(`gh attestation verify "oci://\$\{ref\}"`, `the SBOM's attestation (gh attestation verify "oci://${ref}")`),
}

// The finding conditions of row 7's verifications (review round I of PR 5c),
// by the tool's own exits: each is the condition of the elif that records
// verification-failed.
const (
	// findingExit1 is the finding of gh attestation verify and cosign
	// verify-attestation, which exit 1 on a refusal.
	findingExit1 = `[ "${rc}" -eq 1 ]`
	// findingChecksum is sha256sum's mismatch (1), a finding only when the
	// filtered list held well-formed lines (L2): over an empty list it is no
	// verification at all.
	findingChecksum = `[ "${rc}" -eq 1 ] && [ -s "${out}/checksums.filtered" ]`
	// findingCosignVerify is cosign verify's refusal (B1): cosign v3.1.5 maps
	// ErrNoSignaturesFound, ErrImageTagNotFound, ErrNoMatchingSignatures and
	// ErrNoCertificateFoundOnSignature to 10 to 13
	// (cmd/cosign/errors/exit_code_lookup.go, exit_codes.go), and any other
	// refusal to 1.
	findingCosignVerify = `[ "${rc}" -eq 1 ] || { [ "${rc}" -ge 10 ] && [ "${rc}" -le 13 ]; }`
)

// findingCondition returns the finding condition of the verification tool
// auditVerification captured, and why it is that one.
func findingCondition(tool string) (cond, why string) {
	switch tool {
	case "sha256sum --check":
		return findingChecksum, "a mismatch over the filtered list's well-formed lines is a finding, and over an empty list no verification"
	case "cosign verify":
		return findingCosignVerify, "cosign v3.1.5 exits 1, or 10 to 13 (no signature, no such tag, no matching signature, no certificate on the signature), on a refused signature"
	default:
		return findingExit1, "the tool exits 1 on a refusal"
	}
}

// verificationOutcome returns the pattern of a verification's outcome, from
// the line after its command to its fi: its exit recorded, then rc 124 as the
// check timed out with its limits line, the finding condition cond as the
// finding verification-failed, any other failure as the check's tool-failed,
// each on a line of its own.
func verificationOutcome(check, cond string) *regexp.Regexp {
	q := regexp.QuoteMeta(check)

	return regexp.MustCompile(`\A[ \t]*echo "\$\{rc\}" > "\$\{out\}/[^"\n]+\.exit"\n` +
		`[ \t]*if \[ "\$\{rc\}" -eq 124 \]; then\n` +
		`[ \t]+echo "` + q + ` timed-out" >> "\$\{results\}/not-run\.txt"\n` +
		`[ \t]+echo "` + q + `: [^"\n]+" >> "\$\{results\}/limits\.txt"\n` +
		`[ \t]*` + regexp.QuoteMeta(`elif `+cond+`; then`) + `\n` +
		`[ \t]+echo "` + q + ` verification-failed" >> "\$\{results\}/verification-failed\.txt"\n` +
		`[ \t]*elif \[ "\$\{rc\}" -ne 0 \]; then\n` +
		`[ \t]+echo "` + q + ` tool-failed" >> "\$\{results\}/not-run\.txt"\n` +
		`[ \t]*fi(?:\n|\z)`)
}

// verifications holds row 7's verifications to one outcome each (round E of
// PR 5c): a checksum mismatch, a refused attestation or a refused signature
// (rc 1) is a finding about the release, recorded as verification-failed in
// the results, never as a check that did not run, and a timeout (rc 124) is
// the check timed out, never a tool that failed.
func (a *auditReader) verifications(code, path, check string) {
	lines := strings.Split(code, "\n")

	for _, k := range auditVerificationKinds {
		if !k.re.MatchString(code) {
			a.r.fail("%s runs no %s: each of the release's five verifications runs, and records its outcome", path, k.want)
		}
	}

	for i := 0; i < len(lines); i++ {
		m := auditVerification.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}

		first := strings.TrimSpace(lines[i])
		cond, why := findingCondition(m[1])

		end := i
		for end < len(lines)-1 && strings.HasSuffix(lines[end], `\`) {
			end++
		}

		if !verificationOutcome(check, cond).MatchString(strings.Join(lines[end+1:], "\n")) {
			a.r.fail(`%s runs %q without its outcome branch, want right after it, each on a line of its own: echo "${rc}" > "${out}/<name>.exit", `+
				`if [ "${rc}" -eq 124 ]; then, echo "%s timed-out" >> "${results}/not-run.txt", echo "%s: ..." >> "${results}/limits.txt", `+
				`elif %s; then, echo "%s verification-failed" >> "${results}/verification-failed.txt", `+
				`elif [ "${rc}" -ne 0 ]; then, echo "%s tool-failed" >> "${results}/not-run.txt" and fi: `+
				`a mismatch or a refusal is a finding about the release (verification-failed), a timeout is timed-out, and neither is a tool that failed; %s (PRD #175 milestone 2, ADR 0040)`,
				path, first, check, check, cond, check, check, why)
		}

		i = end
	}
}

// auditStepTimeout matches one timeout <duration> of a script: whole
// minutes (m), seconds (s) or a bare number of seconds.
var auditStepTimeout = regexp.MustCompile(`(?:^|[\s;&|(])timeout (?:-\S+ )*([0-9]+)([sm]?)(?:\s|$)`)

// auditLoopStart and auditLoopEnd are a while, until or for loop's first and
// last lines; a loop may be the last command of a pipeline.
var (
	auditLoopStart = regexp.MustCompile(`^[ \t]*(?:[^\n]*\|[ \t]*)?(?:while|until|for)\s.*(?:;|\s)do$`)
	auditLoopEnd   = regexp.MustCompile(`^[ \t]*done(?:\s|;|$)`)
	// auditLiteralLoop is a for loop over a list of literal words, which
	// expands to nothing the release or the API holds: its words are how many
	// times it runs.
	auditLiteralLoop = regexp.MustCompile("^[ \\t]*for [A-Za-z_][A-Za-z0-9_]* in ([^;$*?\\[\\](){}\"'`\\\\]+); do$")
)

// auditCaseStart and auditCaseEnd are a case statement's first and last
// lines, auditCaseArmEnd the end of one of its arms; auditFunctionStart is a
// shell function's first line, its name in the group (review round J of PR
// 5c).
var (
	auditCaseStart     = regexp.MustCompile(`^[ \t]*case\s.*\sin$`)
	auditCaseEnd       = regexp.MustCompile(`^[ \t]*esac(?:\s|;|$)`)
	auditCaseArmEnd    = regexp.MustCompile(`;;[ \t]*$`)
	auditFunctionStart = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*\(\)[ \t]*\{$`)
	auditLocalLine     = regexp.MustCompile(`^[ \t]*local\s`)
)

// scriptTimeouts returns the seconds the timeouts of lines add up to, and each
// as it is spelled. The arms of a case statement are alternatives: only one of
// them runs, so the case counts its longest arm (review round J of PR 5c).
func scriptTimeouts(lines []string) (int, []string) {
	seconds := 0

	var parts []string

	for i := 0; i < len(lines); i++ {
		if auditCaseStart.MatchString(lines[i]) {
			end := caseEnd(lines, i)
			s, p := caseTimeouts(lines[i+1 : end])
			seconds += s
			parts = append(parts, p...)
			i = end

			continue
		}

		s, p := lineTimeouts(lines[i])
		seconds += s
		parts = append(parts, p...)
	}

	return seconds, parts
}

// lineTimeouts returns the seconds the timeouts of one line add up to, and
// each as it is spelled.
func lineTimeouts(line string) (int, []string) {
	seconds := 0

	var parts []string

	for _, m := range auditStepTimeout.FindAllStringSubmatch(line, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		if m[2] == "m" {
			n *= 60
		}

		seconds += n
		parts = append(parts, "timeout "+m[1]+m[2])
	}

	return seconds, parts
}

// caseEnd returns the index of the esac that closes the case lines[i] starts,
// len(lines) when none does.
func caseEnd(lines []string, i int) int {
	depth := 0

	for j := i; j < len(lines); j++ {
		switch {
		case auditCaseStart.MatchString(lines[j]):
			depth++
		case auditCaseEnd.MatchString(lines[j]):
			depth--
			if depth == 0 {
				return j
			}
		}
	}

	return len(lines)
}

// caseTimeouts returns the timeouts of a case statement's longest arm, arms
// being the lines between its case and its esac.
func caseTimeouts(arms []string) (int, []string) {
	longest := 0

	var parts []string

	start := 0
	depth := 0

	for j, line := range arms {
		switch {
		case auditCaseStart.MatchString(line):
			depth++
		case auditCaseEnd.MatchString(line):
			depth--
		}

		if depth > 0 || (!auditCaseArmEnd.MatchString(line) && j < len(arms)-1) {
			continue
		}

		if s, p := scriptTimeouts(arms[start : j+1]); s > longest {
			longest, parts = s, p
		}

		start = j + 1
	}

	if len(parts) == 0 {
		return 0, nil
	}

	return longest, []string{"the longest arm of a case (" + strings.Join(parts, ", ") + ")"}
}

// functionEnd returns the index of the } that closes the shell function
// lines[i] starts, the first one indented as its first line, len(lines) when
// none does.
func functionEnd(lines []string, i int) int {
	indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]

	for j := i + 1; j < len(lines); j++ {
		if lines[j] == indent+"}" {
			return j
		}
	}

	return len(lines)
}

// headBudget returns the budget a shell function's body holds at its head:
// its first line past its local lines and blank lines, as auditBudget spells
// it.
func headBudget(body []string) (int, bool) {
	for _, line := range body {
		if strings.TrimSpace(line) == "" || auditLocalLine.MatchString(line) {
			continue
		}

		m := auditBudget.FindStringSubmatch(line)
		if m == nil {
			return 0, false
		}

		n, err := strconv.Atoi(m[1])

		return n, err == nil
	}

	return 0, false
}

// maxBudget returns the largest budget lines hold, 0 when they hold none.
func maxBudget(lines []string) int {
	budget := 0

	for _, m := range auditBudget.FindAllStringSubmatch(strings.Join(lines, "\n"), -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > budget {
			budget = n
		}
	}

	return budget
}

// stepBound is the running bound fit adds a step's timeouts to, in seconds,
// and the parts that make it up.
type stepBound struct {
	seconds int
	parts   []string
}

// add adds timeouts that run once.
func (b *stepBound) add(seconds int, parts []string) {
	b.seconds += seconds
	b.parts = append(b.parts, parts...)
}

// budgeted counts what a budget stops (a loop, or a shell function called any
// number of times): nothing of it starts past its budget of budget seconds,
// so it ends by that budget and the timeouts of the one round, an iteration
// or a call, that began just before it. That bounds it together with every
// timeout before it, which ran before that round began, so the bound becomes
// the larger of the two; what follows adds to it.
func (b *stepBound) budgeted(what string, budget, round int, roundParts []string) {
	desc := fmt.Sprintf(`%s's budget of %d seconds (if [ "${SECONDS}" -ge %d ]) and the timeouts of its last round (%s)`,
		what, budget, budget, strings.Join(roundParts, ", "))

	if budget+round < b.seconds {
		b.parts = append(b.parts, desc+", which end within the timeouts before it")

		return
	}

	b.seconds = budget + round
	b.parts = []string{desc}
}

// loopEnd returns the index of the done that closes the loop lines[i] starts,
// len(lines) when none does.
func loopEnd(lines []string, i int) int {
	depth := 0

	for j := i; j < len(lines); j++ {
		switch {
		case auditLoopStart.MatchString(lines[j]):
			depth++
		case auditLoopEnd.MatchString(lines[j]):
			depth--
			if depth == 0 {
				return j
			}
		}
	}

	return len(lines)
}

// fit holds a step of a job whose steps stay within its timeout to its own
// timeout-minutes (round E of PR 5c): its worst case adds up to less, because
// a step's timeout firing ends the job and every later step of it, the
// encrypt step included, is skipped. Since review round J the worst case is
// counted in the script's order, as one sum T of its timeouts: a timeout
// outside its loops and functions adds once, a case its longest arm; a loop
// with a budget B inside, whose body's timeouts are T_iter, sets T to
// max(T, B + T_iter), since no iteration begins past B but the last one begun
// runs to its timeouts; a shell function with a timeout holds a budget B at
// the head of its body and counts as max(T, B + one call's timeouts), never per
// call, or is refused; a loop over literal words with no budget inside counts
// its timeouts once per word. A loop that holds a timeout and is none of these
// is refused (review round I, M1): nothing bounds how often it runs, so
// nothing bounds the step.
func (a *auditReader) fit(code, path string, minutes int) {
	if minutes <= 0 {
		return
	}

	lines := logicalLines(code)
	stepBudget := maxBudget(lines)

	var bound stepBound

	for i := 0; i < len(lines); i++ {
		switch {
		case auditFunctionStart.MatchString(lines[i]):
			name := auditFunctionStart.FindStringSubmatch(lines[i])[1]
			end := functionEnd(lines, i)
			body := lines[i+1 : min(end, len(lines))]
			i = end

			s, p := scriptTimeouts(body)
			if len(p) == 0 {
				continue
			}

			budget, ok := headBudget(body)
			if !ok {
				a.r.fail(`%s runs the shell function %q, which holds %s with no function budget (if [ "${SECONDS}" -ge <seconds> ]; then, on a line of its own, at the head of its body): `+
					`nothing bounds how often the step calls it, so its timeouts can outrun the step's timeout-minutes, which ends the job and skips every later step, the encrypt step included; `+
					`a function with a timeout stops at a budget and records the reads it did not make (PRD #175 milestone 2, ADR 0040)`, path, name, strings.Join(p, ", "))
				bound.add(s, p)

				continue
			}

			bound.budgeted(fmt.Sprintf("the shell function %q", name), budget, s, p)
		case auditCaseStart.MatchString(lines[i]):
			end := caseEnd(lines, i)
			bound.add(scriptTimeouts(lines[i:min(end+1, len(lines))]))
			i = end
		case auditLoopStart.MatchString(lines[i]):
			end := loopEnd(lines, i)
			body := lines[i+1 : end]
			header := strings.TrimSpace(lines[i])
			i = end

			s, p := scriptTimeouts(body)
			if len(p) == 0 {
				continue
			}

			if budget := maxBudget(body); budget > 0 {
				bound.budgeted(fmt.Sprintf("the loop %q", header), budget, s, p)

				continue
			}

			words := auditLiteralLoop.FindStringSubmatch(header)
			if stepBudget > 0 && words != nil && !slices.ContainsFunc(body, auditLoopStart.MatchString) {
				n := len(strings.Fields(words[1]))
				bound.add(n*s, []string{fmt.Sprintf("%d runs of the loop %q (%s)", n, header, strings.Join(p, ", "))})

				continue
			}

			a.r.fail(`%s runs the loop %q, which holds %s with no loop budget (if [ "${SECONDS}" -ge <seconds> ]; then, on a line of its own, at the head of its body): `+
				`nothing bounds how often it runs, so its timeouts can outrun the step's timeout-minutes, which ends the job and skips every later step, the encrypt step included; `+
				`a loop with a timeout stops at a budget and records what it did not reach (PRD #175 milestone 2, ADR 0040)`, path, header, strings.Join(p, ", "))
		default:
			bound.add(lineTimeouts(lines[i]))
		}
	}

	if bound.seconds >= minutes*60 {
		a.r.fail("%s's own timeouts and its budgets, counted in the script's order, add up to %d seconds (%s), not below and so past its timeout-minutes of %d, %d seconds: "+
			"a step's timeout firing ends the job and skips every later step, the encrypt step included, so each of its own timeouts fires first (ADR 0040)",
			path, bound.seconds, strings.Join(bound.parts, ", "), minutes, minutes*60)
	}
}

// auditBounded matches the commands that read the release's files or its
// image (review round D of PR 5c, A1): each one runs under a timeout of its
// own and records its exit with || rc=$?.
var auditBounded = regexp.MustCompile(`(?:^|[\s;&|(])(sha256sum|tar|unzip|docker (?:export|create|cp|commit|image inspect|save)|cosign|gh attestation verify)(?:\s|$)`)

// bounded holds a step that reads the release or its image (want.check) to
// what nothing the release holds can make it skip: every command of
// auditBounded under a timeout, its exit recorded with || rc=$?, and both a
// failure and a timeout recorded as the check not run.
func (a *auditReader) bounded(code, path, check string) {
	for _, line := range logicalLines(code) {
		for _, m := range auditBounded.FindAllStringSubmatchIndex(line, -1) {
			name := line[m[2]:m[3]]
			quoted := strings.TrimSpace(line)

			if !strings.Contains(line[:m[2]], "timeout ") {
				a.r.fail("%s runs %s without its timeout (%q): a command that reads the release's files or its image runs under timeout <duration> of its own, "+
					"so that one the release makes wait is recorded as %s timed-out, not ended by the step's timeout", path, name, quoted, check)
			}

			if !strings.Contains(line[m[2]:], " || rc=$?") {
				a.r.fail("%s runs %s without || rc=$? (%q): its exit is recorded, so that a failure is %s tool-failed, not the end of the step under set -e",
					path, name, quoted, check)
			}
		}
	}

	for _, why := range []string{"tool-failed", "timed-out"} {
		record := `echo "` + check + ` ` + why + `" >> "${results}/not-run.txt"`
		if !strings.Contains(code, record) {
			a.r.fail("%s records no %s %s (%s): a command of the check that fails or times out names the check as not run, never as clean", path, check, why, record)
		}
	}
}

// auditToolRun matches a run of one of the checks job's tools, by its command
// word; auditGoToolRun matches one the go command runs (go run <path>/<tool>,
// go tool <tool>), by the tool's last path element (round F of PR 5c).
var (
	auditToolRun   = regexp.MustCompile(`(?:^|[\s;&|(])(govulncheck|osv-scanner|gitleaks|zizmor|actionlint|scorecard|trivy|cosign|gh attestation verify|go test)(?:\s|$)`)
	auditGoToolRun = regexp.MustCompile(`(?:^|[\s;&|(])go\s+(?:-C\s+\S+\s+)?(?:run|tool)\s+(?:-\S+\s+)*(?:\S*/)?(govulncheck|osv-scanner|gitleaks|zizmor|actionlint|scorecard|trivy|cosign)(?:\s|$)`)
	// auditToolTimeout is the timeout a tool run is under: whole minutes or
	// seconds.
	auditToolTimeout = regexp.MustCompile(`(?:^|[\s;&|(])timeout [0-9]+[ms] `)
	// auditMessageLine is a line that only echoes a quoted text, to the log or
	// to a quoted file: a tool it names is no run of it.
	auditMessageLine = regexp.MustCompile(`^[ \t]*echo "[^"\n]*"(?: >>? "[^"\n]*")?$`)
	// auditContainerTest is the go test a container's sh -c runs (row 14), and
	// auditBoundedStart the docker start that bounds it: timeout and rc=$? are
	// that line's, not the container's script's.
	auditContainerTest = regexp.MustCompile(`docker create [^\n]*\ssh -c '[^'\n]*$`)
	auditBoundedStart  = regexp.MustCompile(`(?m)^[ \t]*timeout [0-9]+[ms] docker start [^\n]* \|\| rc=\$\?$`)
)

// toolRuns holds every tool run of a checks step to a timeout of its own and
// its exit recorded (round F of PR 5c): timeout <N>m or <N>s before the tool,
// || rc=$? after it, on its line. A tool that hangs is then the check that timed
// out, recorded not run, never the end of the step and of every later one, the
// encrypt step included; a tool that fails is recorded, never the end of the
// step under set -e. The go test a container runs is bounded by the docker start
// that runs the container.
func (a *auditReader) toolRuns(code, path string) {
	for _, line := range logicalLines(code) {
		if auditMessageLine.MatchString(line) {
			continue
		}

		seen := map[int]bool{}

		for _, re := range []*regexp.Regexp{auditGoToolRun, auditToolRun} {
			for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
				if seen[m[2]] {
					continue
				}

				seen[m[2]] = true
				a.toolRun(code, path, line, line[m[2]:m[3]], m[2])
			}
		}
	}
}

// toolRun holds one tool run, tool at pos of line, to its timeout and its
// recorded exit.
func (a *auditReader) toolRun(code, path, line, tool string, pos int) {
	quoted := strings.TrimSpace(line)

	if tool == "go test" && auditContainerTest.MatchString(line[:pos]) {
		if !auditBoundedStart.MatchString(code) {
			a.r.fail("%s runs go test in a container (%q) with no timeout <N>m docker start ... || rc=$? on a line of its own: the container's run is under a timeout of its own, "+
				"so that a target that hangs is recorded, not the end of the step", path, quoted)
		}

		return
	}

	if !auditToolTimeout.MatchString(line[:pos]) {
		a.r.fail("%s runs %s without its timeout (%q): every tool run of the checks job is under timeout <N>m or <N>s of its own, "+
			"so that a tool that hangs is recorded as the check that timed out, not the end of the step and of every later one, the encrypt step included (ADR 0040)",
			path, tool, quoted)
	}

	if !strings.Contains(line[pos:], " || rc=$?") {
		a.r.fail("%s runs %s without || rc=$? (%q): its exit is recorded, so that a failure is the check's tool-failed or its finding, not the end of the step under set -e",
			path, tool, quoted)
	}
}

// auditDockerTimeout is the timeout right before a docker command: whole
// minutes, seconds or a bare number of seconds.
var auditDockerTimeout = regexp.MustCompile(`(?:^|[\s;&|(])timeout (?:-\S+ )*[0-9]+[sm]? $`)

// dockerRuns holds every docker command of a checks step to a timeout of its
// own and its exit handled (review round I of PR 5c, L1): timeout <N> right
// before docker, and || rc=$? or, for a command that only cleans up, || true
// after it. The daemon, the registry and the release's image are no less able
// to make an inspect, a kill or a rm wait than a run, and a step whose own
// timeout fires ends the job before the encrypt step.
func (a *auditReader) dockerRuns(code, path string) {
	for _, line := range logicalLines(code) {
		if auditMessageLine.MatchString(line) {
			continue
		}

		for _, loc := range auditDockerWord.FindAllStringIndex(line, -1) {
			pos := loc[0] + strings.Index(line[loc[0]:loc[1]], "docker")
			spelled := dockerSpelled(line[pos:])

			if !auditDockerTimeout.MatchString(line[:pos]) {
				a.r.fail("%s runs %s without its timeout (%q): every docker command of the checks job runs right after timeout <N>m or <N> of its own, "+
					"so that one the daemon, the registry or the image makes wait ends in the check's record, not in the step's timeout, which ends the job before the encrypt step (PRD #175 milestone 2, ADR 0040)",
					path, spelled, strings.TrimSpace(line))
			}

			if rest := line[pos:]; !strings.Contains(rest, " || rc=$?") && !strings.Contains(rest, " || true") {
				a.r.fail("%s runs %s without || rc=$? or || true (%q): its exit is recorded, or for a command that only cleans up ignored, never the end of the step under set -e (PRD #175 milestone 2, ADR 0040)",
					path, spelled, strings.TrimSpace(line))
			}
		}
	}
}

// dockerSpelled returns a docker command as a message names it: docker, its
// subcommand past any global flag, and the subcommand's own word after a
// management word (docker image rm, docker buildx imagetools inspect).
func dockerSpelled(cmd string) string {
	words := strings.Fields(cmd)
	spelled := []string{"docker"}

	i := 1
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		i++
	}

	for n := 0; i < len(words) && n < 3; n++ {
		spelled = append(spelled, words[i])

		if !slices.Contains([]string{"image", "container", "buildx", "imagetools"}, words[i]) {
			break
		}

		i++
	}

	return strings.Join(spelled, " ")
}

// logicalLines returns the script's lines with every \ continuation joined
// onto the line it continues.
func logicalLines(code string) []string {
	var out []string

	cur := ""

	for _, line := range strings.Split(code, "\n") {
		if body, ok := strings.CutSuffix(line, `\`); ok {
			cur += body + " "

			continue
		}

		out = append(out, cur+line)
		cur = ""
	}

	if cur != "" {
		out = append(out, cur)
	}

	return out
}

// auditBudget matches a loop's budget: the seconds since the step began,
// against a number.
var auditBudget = regexp.MustCompile(`(?m)^[ \t]*if \[ "\$\{SECONDS\}" -ge ([0-9]+) \]; then$`)

// budget holds a step's loop to a budget below the step's own timeout
// (review round D of PR 5c, A3): the loop, not the step's timeout, ends it,
// and what it did not reach is recorded.
func (a *auditReader) budget(code, path, check string, minutes int) {
	found := auditBudget.FindAllStringSubmatch(code, -1)
	if len(found) == 0 {
		a.r.fail(`%s has no loop budget (if [ "${SECONDS}" -ge <seconds> ]; then, on a line of its own): its loop stops below the step's timeout-minutes, so that what %s did not reach is recorded in the limits, `+
			`not lost to the step's timeout (PRD #175 milestone 2, ADR 0040)`, path, check)

		return
	}

	for _, m := range found {
		n, err := strconv.Atoi(m[1])
		if err != nil || (minutes > 0 && n >= minutes*60) {
			a.r.fail(`%s's loop budget of %s seconds (if [ "${SECONDS}" -ge %s ]) is not below its timeout-minutes of %d, %d seconds: the step's timeout would end the loop first`,
				path, m[1], m[1], minutes, minutes*60)
		}
	}
}

// auditChecksumGuard matches one download's checksum guard, whole: the pin
// compared by sha256sum under an if !, then its refusalBranch.
var auditChecksumGuard = regexp.MustCompile(`(?m)^[ \t]*if ! echo "\$\{[A-Z0-9_]+_SHA256\}  \$\{[A-Za-z_]+\}/[A-Za-z0-9._-]+" \| sha256sum --check --strict -` + refusalBranch)

// checksumGuards holds every checksum check of a download step to a guard
// (round C of PR 5c, PRD #175 "A failed run is visible"): bare, the step
// fails with sha256sum's own line alone, which names no cause.
func (a *auditReader) checksumGuards(code, path string) {
	rest := auditChecksumGuard.ReplaceAllString(code, "")

	for _, line := range strings.Split(rest, "\n") {
		if strings.Contains(line, "sha256sum") {
			a.r.fail(`%s runs %q, a checksum check that is no guard: want if ! echo "${<TOOL>_SHA256}  <file>" | sha256sum --check --strict -; then, `+
				`echo "::error ..." naming the tool and the runbook step, exit 1 and fi, each on a line of its own (PRD #175, a failed run is visible)`, path, strings.TrimSpace(line))
		}
	}
}

// lineAt returns the line of text that holds the byte at, trimmed: the text a
// refusal quotes.
func lineAt(text string, at int) string {
	start := strings.LastIndexByte(text[:at], '\n') + 1

	end := strings.IndexByte(text[at:], '\n')
	if end < 0 {
		return strings.TrimSpace(text[start:])
	}

	return strings.TrimSpace(text[start : at+end])
}

// withoutComments returns the script's lines that are no comment, joined. Only
// whole comment lines go: a # inside a code line (${digest#sha256:}) stays, and
// a trailing comment is refused by run on its own.
func withoutComments(lines []string) string {
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}

// order checks that the script holds the commands of order in that order. A
// command the script lacks is left to runs, which reports it.
// Every pair out of order is named, the command that must come first first, so
// that one command moved names each command it now follows.
func (a *auditReader) order(text, path string, order []auditCommand) {
	at := make([]int, len(order))

	for i, c := range order {
		at[i] = -1
		if loc := c.re.FindStringIndex(text); loc != nil {
			at[i] = loc[0]
		}
	}

	for i, first := range order {
		for j := i + 1; j < len(order); j++ {
			if at[i] >= 0 && at[j] >= 0 && at[i] > at[j] {
				a.r.fail("%s runs %s after %s, want it before", path, first.want, order[j].want)
			}
		}
	}
}

// hasVar reports whether env sets name to value.
func hasVar(env []auditVar, name, value string) bool {
	return slices.ContainsFunc(env, func(v auditVar) bool { return v.name == name && v.value == value })
}

// pins checks every tool pin of the workflow's env: blocks: a version under a
// Renovate comment, and, for a tool the workflow downloads, its SHA-256 beside
// it, 64 lower-case hex, under its own digestVersion when it has one.
func (a *auditReader) pins() {
	for i, line := range a.r.raw {
		m := auditVersionLine.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[3], "${{") {
			continue
		}

		num, tool, version := i+1, m[2], strings.Trim(m[3], `"`)

		var above []string
		if i > 0 {
			above = auditRenovate.FindStringSubmatch(a.r.raw[i-1])
		}

		if above == nil {
			a.r.fail("line %d: %s_VERSION has no # renovate: datasource=... depName=... comment on the line above, so nothing bumps it (CLAUDE.md rule 7)", num, tool)

			continue
		}

		if above[1] == "go" {
			// A Go module is verified by go.sum and the checksum database.
			continue
		}

		a.digest(i, len(m[1]), tool, version)
	}

	a.pinCopies()
}

// auditPinCopy matches a version or SHA-256 pin of any env: block.
var auditPinCopy = regexp.MustCompile(`^\s+([A-Z0-9_]+_(?:VERSION|SHA256)):\s*(\S.*)$`)

// pinCopies holds every copy of a version or SHA-256 pin to the first one's
// value: Renovate moves every copy together, so copies that differ are a hand
// edit that moved one alone (review round 1 of PR 5b, R8). A value read from
// an expression is no pin.
func (a *auditReader) pinCopies() {
	type first struct {
		value string
		num   int
	}

	seen := map[string]first{}

	for i, line := range a.r.raw {
		m := auditPinCopy.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[2], "${{") {
			continue
		}

		name, value := m[1], strings.TrimSpace(m[2])

		f, ok := seen[name]
		if !ok {
			seen[name] = first{value: value, num: i + 1}

			continue
		}

		if f.value != value {
			a.r.fail("line %d: %s is %s, but its copy on line %d is %s: every copy of a pin holds the same value", i+1, name, value, f.num, f.value)
		}
	}
}

// digest finds the SHA-256 pin of tool below its version, the i-th raw line
// indented by indent, before the next version or the block's end.
func (a *auditReader) digest(i, indent int, tool, version string) {
	for j := i + 1; j < len(a.r.raw); j++ {
		line := a.r.raw[j]
		text := strings.TrimLeft(line, " ")

		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		if len(line)-len(text) != indent || auditVersionLine.MatchString(line) {
			break
		}

		m := auditDigestLine.FindStringSubmatch(line)
		if m == nil || m[1] != tool {
			continue
		}

		if !auditDigestValue.MatchString(strings.TrimSpace(m[2])) {
			a.r.fail("line %d: %s's SHA-256 is %s, want 64 lower-case hex in quotes", j+1, tool, strings.TrimSpace(m[2]))
		}

		if c := auditRenovate.FindStringSubmatch(a.r.raw[j-1]); c != nil && c[3] != version {
			a.r.fail("line %d: %s's SHA-256 is for digestVersion=%s, want %s, the version it pins", j+1, tool, c[3], version)
		}

		return
	}

	a.r.fail("line %d: %s_VERSION %s is a downloaded tool with no SHA-256 pin (%s_SHA256) beside it", i+1, tool, version, tool)
}
