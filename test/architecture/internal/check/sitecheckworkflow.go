package check

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// SiteCheckWindow is the version window's bound the scheduled live check
// passes to ci/sitecheck live-version (PRD #297 milestone 4, ADR 0043): how
// long after the highest release tag's commit the live site may still serve
// the release before it. It covers MinSiteCheckWindow, the slowest release
// run the workflows allow, with a margin.
const SiteCheckWindow = 180 * time.Minute

// siteCheckHTMLMaxAge is the HTML's max-age, ci/sitecheck/internal/livecheck's
// MaxHTMLAge: a page deployed at the end of the release run may be served
// from a cache that long.
const siteCheckHTMLMaxAge = 5 * time.Minute

// siteCheckCron matches a cron entry that runs once a day: a fixed minute and
// hour, every day of every month.
var siteCheckCron = regexp.MustCompile(`^[1-5]?[0-9] (?:1?[0-9]|2[0-3]) \* \* \*$`)

// The run: commands of the scheduled live check's steps, exactly.
var (
	siteCheckExpectedRun = strings.Join([]string{
		"set -euo pipefail",
		"git fetch --quiet --force origin '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'",
		`mapfile -t tags < <(git tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V)`,
		`expected="${tags[-1]}"`,
		`printf '%s\n' "${tags[@]}" > "${RUNNER_TEMP}/tags.txt"`,
		`anchor="$(git log -1 --format=%cI "${expected}")"`,
		`echo "tag=${expected}" >> "${GITHUB_OUTPUT}"`,
		`echo "anchor=${anchor}" >> "${GITHUB_OUTPUT}"`,
	}, "\n")

	siteCheckLiveRun = strings.Join([]string{
		"set -euo pipefail",
		fmt.Sprintf(`live="$(go run ./ci/sitecheck live-version -tags "${RUNNER_TEMP}/tags.txt" -anchor "${ANCHOR}" -window %dm)"`,
			int(SiteCheckWindow/time.Minute)),
		`grep -qxF "${live}" "${RUNNER_TEMP}/tags.txt"`,
		`echo "tag=${live}" >> "${GITHUB_OUTPUT}"`,
	}, "\n")

	siteCheckAttestationRun = strings.Join([]string{
		"set -euo pipefail",
		`mkdir -p "${RUNNER_TEMP}/bundle"`,
		`gh release download "${TAG}" --repo timothymamo/shift2eu --pattern "shift2eu-site_${TAG}.tar.gz" --dir "${RUNNER_TEMP}/bundle"`,
		`gh attestation verify "${RUNNER_TEMP}/bundle/shift2eu-site_${TAG}.tar.gz" --repo timothymamo/shift2eu --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml --source-ref "refs/tags/${TAG}" --deny-self-hosted-runners`,
		`tar -xzf "${RUNNER_TEMP}/bundle/shift2eu-site_${TAG}.tar.gz" -C "${RUNNER_TEMP}/bundle"`,
	}, "\n")

	siteCheckFullRun = strings.Join([]string{
		"set -euo pipefail",
		`go run ./ci/sitecheck -manifest "${RUNNER_TEMP}/bundle/manifest.json" -tags "${RUNNER_TEMP}/tags.txt"`,
	}, "\n")

	siteCheckAlertRun = strings.Join([]string{
		"set -euo pipefail",
		`body="$(printf 'The scheduled live site check failed: %s\n' "${RUN_URL}")"`,
		`gh label create "${LABEL}" --repo "${GH_REPO}" --force --color ededed --description "The scheduled live site check"`,
		`existing="$(gh issue list --repo "${GH_REPO}" --label "${LABEL}" --state open --json number --jq '.[0].number // empty')"`,
		`if [ -n "${existing}" ]; then`,
		`  gh issue comment "${existing}" --repo "${GH_REPO}" --body "${body}"`,
		"else",
		`  gh issue create --repo "${GH_REPO}" --title "${TITLE}" --body "${body}" --label "${LABEL}" --assignee timothymamo`,
		"fi",
	}, "\n")
)

// siteCheckJobs are the scheduled live check's two jobs.
var siteCheckJobs = []releaseJob{
	{
		key: "check", name: "Check the live site", timeout: "30",
		permissions: map[string]string{"contents": "read", "attestations": "read"},
		steps: []releaseStep{
			fullCheckoutStep,
			setupGoStep,
			{what: "the expected release", name: "The expected release", id: "expected", run: siteCheckExpectedRun},
			{
				what: "the live version", name: "The live version", id: "live",
				env: []string{"ANCHOR: ${{ steps.expected.outputs.anchor }}"},
				run: siteCheckLiveRun,
			},
			{
				what: "the attestation's check", name: "Verify the release's attestation",
				env: []string{
					"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
					"TAG: ${{ steps.live.outputs.tag }}",
				},
				run: siteCheckAttestationRun,
			},
			{what: "the live check", name: "Check the live site", run: siteCheckFullRun},
		},
	},
	{
		key: "alert", name: "Alert", timeout: "10", needs: []string{"check"}, ifCond: "failure()",
		permissions: map[string]string{"issues": "write"},
		steps: []releaseStep{{
			what: "the alert issue", name: "Open or comment on the alert issue",
			env: []string{
				"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
				"GH_REPO: ${{ github.repository }}",
				"RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}",
				"LABEL: live site check",
				"TITLE: The live site check failed",
			},
			run: siteCheckAlertRun,
		}},
	},
}

// SiteCheckWorkflow returns nil when data, .github/workflows/site-check.yml,
// is the scheduled live check (PRD #297 milestone 4, ADR 0043), read as a
// closed schema, or an error listing every way it departs from it:
//
//   - the top level holds exactly name (Live site check), on, permissions
//     ({}) and jobs;
//   - on holds exactly one schedule of exactly one cron entry that runs once
//     a day, and no other trigger;
//   - jobs holds exactly check and alert, each with its name, ubuntu-latest,
//     its timeout-minutes and its permissions and nothing else (no
//     environment, no outputs);
//   - check holds contents: read and attestations: read alone, and runs
//     exactly: the whole history checked out without keeping the credential;
//     setup-go from go.mod with its cache off, every action pinned by commit;
//     main's release tags, fetched and listed exactly as release.yml's
//     site-check job lists them (--merged refs/remotes/origin/main, vX.Y.Z
//     alone), and the committer date (%cI) of the highest tag's commit;
//     ci/sitecheck live-version with that anchor, passed through env:, and the bound
//     SiteCheckWindow, its output re-checked against the tag list; the
//     release download and gh attestation verify with the signer workflow,
//     the tag's ref and no self-hosted runner, through GITHUB_TOKEN alone;
//     and the full check against the bundle's manifest and the tag list;
//   - alert needs check, runs if: failure(), holds issues: write alone, checks
//     nothing out, creates the label "live site check" with --force, so a
//     repository that lacks it still gets the issue, and then opens or
//     comments on the one issue so labelled, assigned to timothymamo, with
//     fixed text and the run's URL: no expression but GITHUB_TOKEN, the
//     repository, the server's URL and the run's id, and no output of the
//     check job.
//
// Every run: is compared exactly and holds no ${{ }} expression: a value a
// step needs reaches it through env:. Like ReleaseWorkflow, every
// line is also read for a secrets context in a form other than secrets.NAME.
func SiteCheckWorkflow(data []byte) error {
	raw := strings.Split(string(data), "\n")

	var messages []string

	for i := range raw {
		raw[i] = strings.TrimSuffix(raw[i], "\r")
		messages = append(messages, secretsContextForms(i+1, raw[i])...)
	}

	lines, err := yamlread.SplitLines(data)
	if err != nil {
		messages = append(messages, fmt.Sprintf("read the scheduled live check's workflow: %v", err))
	} else {
		messages = append(messages, siteCheckSchema(raw, lines)...)
	}

	if len(messages) == 0 {
		return nil
	}

	return errors.New(strings.Join(messages, "\n"))
}

// siteCheckSchema returns a message for every way lines, the content lines of
// the scheduled live check whose every line is raw, departs from the schema
// SiteCheckWorkflow describes.
func siteCheckSchema(raw []string, lines []yamlread.Line) []string {
	if len(lines) == 0 {
		return []string{"the scheduled live check's workflow is empty"}
	}

	r := &schemaReader{raw: raw}

	top := r.exactKeys(lines, "the scheduled live check", "name", "on", "permissions", "jobs")
	if top == nil {
		return r.messages
	}

	r.scalar(top["name"], "the scheduled live check's name", exactly("Live site check"))
	r.emptyMapping(top["permissions"], "the scheduled live check's permissions")

	if on := r.exactKeys(r.block(top["on"], "on"), "on", "schedule"); on != nil {
		r.schedule(on["schedule"])
	}

	keys := make([]string, 0, len(siteCheckJobs))
	for _, j := range siteCheckJobs {
		keys = append(keys, j.key)
	}

	jobs := r.exactKeys(r.block(top["jobs"], "jobs"), "jobs", keys...)
	if jobs == nil {
		return r.messages
	}

	for _, j := range siteCheckJobs {
		r.job(jobs[j.key], "jobs: "+j.key, j)
	}

	return r.messages
}

// schedule checks on: schedule: exactly one cron entry that runs once a day.
func (r *schemaReader) schedule(e yamlread.Entry) {
	const path = "on: schedule"

	block := r.block(e, path)
	if block == nil {
		return
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if len(items) != 1 {
		r.fail("line %d: %s holds %d entries, want exactly one daily cron", e.Num, path, len(items))

		return
	}

	if entry := r.exactKeys(items[0].Lines, path+": entry", "cron"); entry != nil {
		r.scalar(entry["cron"], path+": cron", siteCheckCron)
	}
}

// MinSiteCheckWindow returns the shortest version window that covers a
// release: from the release tag's commit, release-please.yml's release-please
// and tag jobs, then the longest chain of release.yml's jobs, along needs:,
// that ends at site-check, site-check included, each job at its
// timeout-minutes, and the HTML's max-age after the deploy. releasePlease and
// release are the two workflows; a job of the chain without needs: starts it.
// A job either workflow lacks, a timeout-minutes that is missing or not a
// positive whole number, a needs: naming an unknown job and a cycle are
// errors.
func MinSiteCheckWindow(releasePlease, release []byte) (time.Duration, error) {
	pleaseJobs, err := timedJobs(releasePlease)
	if err != nil {
		return 0, fmt.Errorf("release-please.yml: %w", err)
	}

	var total time.Duration

	for _, key := range []string{"release-please", "tag"} {
		job, ok := pleaseJobs[key]
		if !ok {
			return 0, fmt.Errorf("release-please.yml has no job %s", key)
		}

		total += job.timeout
	}

	releaseJobs, err := timedJobs(release)
	if err != nil {
		return 0, fmt.Errorf("release.yml: %w", err)
	}

	chain, err := longestChain(releaseJobs, "site-check", map[string]bool{})
	if err != nil {
		return 0, fmt.Errorf("release.yml: %w", err)
	}

	return total + chain + siteCheckHTMLMaxAge, nil
}

// timedJob is a job's timeout and the jobs it needs.
type timedJob struct {
	timeout time.Duration
	needs   []string
}

// timedJobs reads every job of a workflow: its timeout-minutes and needs.
func timedJobs(data []byte) (map[string]timedJob, error) {
	lines, err := yamlread.SplitLines(data)
	if err != nil {
		return nil, err
	}

	block, err := yamlread.Walk(lines, "jobs")
	if err != nil {
		return nil, err
	}

	entries, err := yamlread.Mapping(block)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}

	jobs := make(map[string]timedJob, len(entries))

	for _, e := range entries {
		job, err := readTimedJob(e)
		if err != nil {
			return nil, fmt.Errorf("jobs: %s: %w", e.Key, err)
		}

		jobs[e.Key] = job
	}

	return jobs, nil
}

// readTimedJob reads one job's timeout-minutes and needs.
func readTimedJob(e yamlread.Entry) (timedJob, error) {
	block, err := yamlread.BlockValue(e, e.Key)
	if err != nil {
		return timedJob{}, err
	}

	keys, err := yamlread.Mapping(block)
	if err != nil {
		return timedJob{}, err
	}

	var (
		job        timedJob
		hasTimeout bool
	)

	for _, k := range keys {
		switch k.Key {
		case "timeout-minutes":
			value, err := yamlread.Single(k)
			if err != nil {
				return timedJob{}, err
			}

			minutes, err := strconv.Atoi(value)
			if err != nil || minutes <= 0 {
				return timedJob{}, fmt.Errorf("line %d: timeout-minutes %q is not a positive whole number", k.Num, value)
			}

			job.timeout = time.Duration(minutes) * time.Minute
			hasTimeout = true
		case "needs":
			if k.Value == "" || strings.HasPrefix(k.Value, "[") {
				job.needs, err = yamlread.List(k)
			} else {
				var one string

				one, err = yamlread.Single(k)
				job.needs = []string{one}
			}

			if err != nil {
				return timedJob{}, err
			}
		}
	}

	if !hasTimeout {
		return timedJob{}, errors.New("no timeout-minutes")
	}

	return job, nil
}

// longestChain returns the longest sum of timeouts along needs: that ends at
// key, key's own included. visiting holds the jobs on the current path, so a
// cycle is an error.
func longestChain(jobs map[string]timedJob, key string, visiting map[string]bool) (time.Duration, error) {
	job, ok := jobs[key]
	if !ok {
		return 0, fmt.Errorf("no job %s", key)
	}

	if visiting[key] {
		return 0, fmt.Errorf("the needs of %s form a cycle", key)
	}

	visiting[key] = true
	defer delete(visiting, key)

	var longest time.Duration

	for _, need := range job.needs {
		d, err := longestChain(jobs, need, visiting)
		if err != nil {
			return 0, err
		}

		longest = max(longest, d)
	}

	return job.timeout + longest, nil
}
