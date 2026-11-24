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
}

// auditRead is one env: or with: key and the one expression it reads.
type auditRead struct {
	key, value string
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
	zipDigest      = command(`echo "\$\{digest#sha256:\}  \$\{RUNNER_TEMP\}/[a-z-]+\.zip" \| sha256sum --check --strict -`, `sha256sum --check --strict of the zip against the checked digest`)
	zipUnpack      = command(`unzip -q "\$\{RUNNER_TEMP\}/[a-z-]+\.zip" -d `, "unzip -q of the checked zip")
	zipOneFile     = command(`if \[ "\$\(ls -A "\$\{RUNNER_TEMP\}/[a-z-]+"\)" != [a-z-]+\.age \]; then`, "the single-file check (ls -A) of the unzipped ciphertext")
	// secondListing is a download step that lists the run's artifacts again,
	// so that what it downloads is no longer what the check accepted.
	secondListing = command(`actions/runs/`, "a second listing of the run's artifacts (actions/runs/...), want the ID and digest from ${RUNNER_TEMP}/artifact.json, the checked listing's")

	// The attestation step's order: the digest read as a string alone and
	// refused unless it is exactly "" or one sha256 digest (the step trusts no
	// earlier step's read, review round 2, G3), the tag's shape whatever the
	// digest, then an empty digest skipped with a fixed notice (the run pulled
	// no image), then the digest's shape, and only then the verification.
	digestRead = command(`if ! digest="\$\(jq -er '\.image_digest \| select\(type == "string"\) \| select\(\. == "" or test\("\^sha256:\[0-9a-f\]\{64\}\\\\z"\)\)' "\$\{RUNNER_TEMP\}/result\.json"\)"; then\s+echo "::error [^"\n]*"\s+exit 1\s+fi`,
		`string-and-shape read of the digest: if ! digest="$(jq -er '.image_digest | select(type == "string") | select(. == "" or test("^sha256:[0-9a-f]{64}\\z"))' ...)"; then ... exit 1`)
	tagShape    = command(`\[\[ ! "\$\{tag\}" =~ \^v\[0-9\]\+\\\.\[0-9\]\+\\\.\[0-9\]\+\$ \]\]`, `the tag's shape check [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]`)
	emptyDigest = command(`if \[ -z "\$\{digest\}" \]; then\s+echo "image not pulled[^"\n]*"\s+exit 0\s+fi`, `the empty digest's branch: echo "image not pulled: ..." and exit 0`)
	digestShape = command(`\[\[ ! "\$\{digest\}" =~ \^sha256:\[0-9a-f\]\{64\}\$ \]\]`, `the digest's shape check [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]`)
	verify      = command(`gh attestation verify "oci://`, `gh attestation verify "oci://<image>@<digest>"`)
)

// auditJobs are the workflow's three jobs, in order, with their steps in
// order: the closed list a step is recognised by, by name.
var auditJobs = []auditJob{
	{
		key:  "checks",
		name: "Checks",
		// No attestations scope: no row of the checks job PR 5b runs reads
		// one; PR 5c adds it with the release-integrity rows.
		permissions:      []string{"contents"},
		downloadsModules: "Download the Go modules",
		steps: []auditStep{
			{name: "Check out the audited commit", uses: auditCheckout},
			{
				name: "Record the audited commit and the latest release tag", githubToken: true,
				runs: []auditCommand{command(`releases/latest`, "the latest release's tag (releases/latest)")},
			},
			{name: "Download the pinned tools and verify their checksums", runs: []auditCommand{checksumCheck}},
			{
				name: "Download the Go modules",
				runs: []auditCommand{modDownload, command(`go -C tools mod download`, "go -C tools mod download")},
			},
			{name: "govulncheck", runs: []auditCommand{command(`govulncheck .*\./\.\.\.`, "govulncheck ./...")}},
			{name: "osv-scanner", runs: []auditCommand{command(`osv-scanner scan source `, "osv-scanner scan source")}},
			{
				name: "gitleaks on the history",
				runs: []auditCommand{
					command(`gitleaks git .*--redact`, "gitleaks git --redact"),
					command(`gitleaks git .*--ignore-gitleaks-allow`, "gitleaks git --ignore-gitleaks-allow (no allow comment in the history hides a secret)"),
				},
			},
			// Two steps: only zizmor's online audits read GitHub's API, so only
			// its step holds the job's token.
			{name: "zizmor", githubToken: true, runs: []auditCommand{command(`zizmor `, "zizmor")}},
			{name: "actionlint", runs: []auditCommand{command(`actionlint`, "actionlint")}},
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
				name: "Install the pinned tools",
				runs: []auditCommand{checksumCheck, command(`dpkg -i `, "dpkg -i"), command(`\.github/scripts/install-claude-code\.sh `, ".github/scripts/install-claude-code.sh")},
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
				runs:    []auditCommand{artifactsModel, artifactFile, artifactID, artifactDigest, artifactZip, zipDigest, zipUnpack, zipOneFile},
				refuses: []auditCommand{secondListing},
				order:   []auditCommand{artifactsModel, artifactFile, artifactZip, zipDigest, zipUnpack, zipOneFile},
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
				runs: []auditCommand{
					command(`systemctl stop docker`, "systemctl stop docker"),
					command(`rm -f /etc/sudoers\.d/runner`, "rm -f /etc/sudoers.d/runner"),
					command(`sudo -n true`, "a check that sudo -n true fails"),
				},
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
			{name: "Download the pinned tools and verify their checksums", runs: []auditCommand{checksumCheck}},
			{
				name: "List the run's artifacts and download the model's ciphertext", githubToken: true,
				runs:    []auditCommand{artifactsPublic, artifactFile, artifactID, artifactDigest, artifactZip, zipDigest, zipUnpack, zipOneFile},
				refuses: []auditCommand{secondListing},
				order:   []auditCommand{artifactsPublic, artifactFile, artifactZip, zipDigest, zipUnpack, zipOneFile},
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
					digestRead, tagShape, emptyDigest, digestShape, verify,
					command(`--repo "\$\{GITHUB_REPOSITORY\}"`, `--repo "${GITHUB_REPOSITORY}"`),
					command(`--signer-workflow "\$\{GITHUB_REPOSITORY\}/\.github/workflows/release\.yml"`, `--signer-workflow "${GITHUB_REPOSITORY}/.github/workflows/release.yml"`),
					command(`--source-ref "refs/tags/`, `--source-ref "refs/tags/<tag>"`),
					command(`--deny-self-hosted-runners`, "--deny-self-hosted-runners: no provenance a self-hosted runner signed"),
				},
				order: []auditCommand{digestRead, tagShape, emptyDigest, digestShape, verify},
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
				runs: []auditCommand{command(`auditpublish"? publish `, "auditpublish publish")},
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
// guard or cut a \ continuation (review round 3, H1). A # inside a word
// (${digest#sha256:}) is code; one after whitespace inside quotes is refused
// too, which fails closed.
var auditTrailingComment = regexp.MustCompile(`(?:^|\s)#`)

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
// hand-off, and every secret and variable read in exactly one place. A
// document it cannot read, or an empty one, is refused.
func AuditWorkflow(src []byte) []string {
	if strings.TrimSpace(string(src)) == "" {
		return []string{"the audit workflow is empty, so this check cannot tell what it runs"}
	}

	r := &schemaReader{raw: rawLines(src)}
	a := &auditReader{r: r, allowed: map[int][]string{}}

	a.lineRules()

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
// list of names can follow, secrets: inherit, a traced shell, and in the model
// job's lines a check or a container.
func (a *auditReader) lineRules() {
	model := a.jobLines("model")

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
		a.steps(v, path, spec)
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

// steps checks a job's steps: the closed list, in order, and each step.
func (a *auditReader) steps(e yamlread.Entry, path string, spec auditJob) {
	block := a.r.block(e, path+": steps")
	if block == nil {
		return
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		a.r.fail("%s: steps: %v", path, err)

		return
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
			if text, err := yamlread.LiteralBlock(a.r.raw, run); err == nil && modDownload.re.MatchString(text) {
				downloaded = true
			}
		}
	}
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
	if t, ok := s.keys["timeout-minutes"]; ok {
		a.timeout(t, path, auditJobLimit-1)
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
		a.run(run, path, want, env, downloaded)
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

// run checks a run: step's script.
func (a *auditReader) run(e yamlread.Entry, path string, want *auditStep, env []auditVar, downloaded bool) {
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
		if c.re.MatchString(code) {
			a.r.fail("%s runs %s", path, c.want)
		}
	}

	a.order(code, path, want.order)
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
