package lint_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// aiReviewWorkflowPath is the AI reviewer's workflow (PRD #161 milestone 4,
// ADR 0033).
const aiReviewWorkflowPath = ".github/workflows/ai-review.yml"

// aiReviewPinnedAction matches a uses: that names an action pinned to a commit:
// the 40-hex-digit SHA every action of this repository's workflows is pinned with
// (CLAUDE.md rule 7). It reads the workflow with test/internal/yamlread, the
// strict line reader the other tests of this package read YAML with; values that
// hold a ${{ }} expression are read as written, since that reader takes no
// expression.
var aiReviewPinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*@[0-9a-f]{40}$`)

// aiReviewClaudeVersion matches the install of the Claude Code CLI at an exact
// version: the npm package, its optional platform suffix and @X.Y.Z (CLAUDE.md
// rule 7).
var aiReviewClaudeVersion = regexp.MustCompile(`@anthropic-ai/claude-code(?:-[a-z0-9-]+)?@(\d+\.\d+\.\d+)`)

// aiReviewChecksum matches a pinned digest the install step checks the download
// against: a SHA-256 in hex, or npm's sha512-<base64> integrity form.
var aiReviewChecksum = regexp.MustCompile(`[0-9a-f]{64}|sha512-[A-Za-z0-9+/=]{20,}`)

// aiReviewWorkflow is the parsed workflow the subtests of
// TestAIReviewWorkflowHoldsTheReviewersSecurityProperties read: its source, its
// content lines, its one job and that job's steps.
type aiReviewWorkflow struct {
	src   []byte
	lines []yamlread.Line
	job   yamlread.Entry
	steps []yamlread.Item
}

// readAIReviewWorkflow reads and parses the AI reviewer's workflow.
func readAIReviewWorkflow(t *testing.T) aiReviewWorkflow {
	t.Helper()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, aiReviewWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; ADR 0033 pins the workflow's trigger, permissions, environment, secrets and pins", aiReviewWorkflowPath, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", aiReviewWorkflowPath, err)
	}

	jobs, ok := aiReviewEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", aiReviewWorkflowPath)
	}

	jobsFields, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		t.Fatalf("%s: read jobs:: %v", aiReviewWorkflowPath, err)
	}

	if len(jobsFields) != 1 {
		t.Fatalf("%s: jobs: holds %d jobs, want the one that reviews the pull request", aiReviewWorkflowPath, len(jobsFields))
	}

	job := jobsFields[0]

	steps, ok := aiReviewEntry(t, job.Block, "steps")
	if !ok {
		t.Fatalf("%s: job %s has no steps:", aiReviewWorkflowPath, job.Key)
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		t.Fatalf("%s: job %s: read steps:: %v", aiReviewWorkflowPath, job.Key, err)
	}

	if len(items) == 0 {
		t.Fatalf("%s: job %s: steps: holds no step", aiReviewWorkflowPath, job.Key)
	}

	return aiReviewWorkflow{src: src, lines: lines, job: job, steps: items}
}

// TestAIReviewWorkflowHoldsTheReviewersSecurityProperties pins the shape of
// .github/workflows/ai-review.yml, which holds two secrets and reads a pull
// request anyone with push access wrote (ADR 0033): main's copy of the workflow
// runs on pull_request_target for the pull request's events only, with a token
// that reads and an App token that writes, in an environment limited to main,
// skipping drafts, cancelling the run in progress, never checking out the pull
// request's code, with every action pinned, and with no pull request data or
// secret interpolated into a script.
func TestAIReviewWorkflowHoldsTheReviewersSecurityProperties(t *testing.T) {
	t.Parallel()

	workflow := readAIReviewWorkflow(t)

	t.Run("pull_request_target on main is the only trigger", func(t *testing.T) {
		t.Parallel()

		on, ok := aiReviewEntry(t, workflow.lines, "on")
		if !ok {
			t.Fatalf("%s: the workflow has no on: trigger", aiReviewWorkflowPath)
		}

		if on.Value != "" {
			t.Fatalf("%s: line %d: on: %s, want a block of triggers: a workflow that runs the pull request's copy runs the pull request's code", aiReviewWorkflowPath, on.Num, on.Value)
		}

		triggers, err := yamlread.Mapping(on.Block)
		if err != nil {
			t.Fatalf("%s: read on:: %v", aiReviewWorkflowPath, err)
		}

		if len(triggers) != 1 || triggers[0].Key != "pull_request_target" {
			keys := make([]string, 0, len(triggers))
			for _, trigger := range triggers {
				keys = append(keys, trigger.Key)
			}

			t.Fatalf("%s: on: holds %v, want exactly pull_request_target: only it runs main's copy of the workflow and can read the ai-review environment's secrets (ADR 0033)", aiReviewWorkflowPath, keys)
		}

		fields, err := yamlread.Mapping(triggers[0].Block)
		if err != nil {
			t.Fatalf("%s: read on: pull_request_target:: %v", aiReviewWorkflowPath, err)
		}

		var types, branches []string

		var sawTypes, sawBranches bool

		for _, field := range fields {
			switch field.Key {
			case "types":
				sawTypes = true

				types, err = yamlread.List(field)
				if err != nil {
					t.Fatalf("%s: read the trigger's types:: %v", aiReviewWorkflowPath, err)
				}
			case "branches":
				sawBranches = true

				branches, err = yamlread.List(field)
				if err != nil {
					t.Fatalf("%s: read the trigger's branches:: %v", aiReviewWorkflowPath, err)
				}
			default:
				t.Errorf("%s: line %d: pull_request_target: has %s:, want types: and branches: only: every pull request into main is reviewed", aiReviewWorkflowPath, field.Num, field.Key)
			}
		}

		if !sawTypes {
			t.Errorf("%s: pull_request_target: has no types:, want opened, reopened, ready_for_review and synchronize", aiReviewWorkflowPath)
		} else {
			want := []string{"opened", "ready_for_review", "reopened", "synchronize"}

			got := slices.Clone(types)
			slices.Sort(got)

			if !slices.Equal(want, got) {
				t.Errorf("%s: pull_request_target: types are %v, want %v: the reviewer reviews a pull request when it is opened, reopened, marked ready and pushed to (ADR 0033)", aiReviewWorkflowPath, types, want)
			}
		}

		if !sawBranches {
			t.Errorf("%s: pull_request_target: has no branches:, want branches: [main]", aiReviewWorkflowPath)
		} else if !slices.Equal([]string{"main"}, branches) {
			t.Errorf("%s: pull_request_target: branches are %v, want [main]", aiReviewWorkflowPath, branches)
		}
	})

	t.Run("GITHUB_TOKEN only reads", func(t *testing.T) {
		t.Parallel()

		// The workflow grants nothing at all: the job's own permissions: are the
		// only ones any step has.
		if entry, ok := aiReviewEntry(t, workflow.lines, "permissions"); !ok {
			t.Errorf("%s: the workflow declares no permissions:, so it takes the repository's default token permissions; it grants nothing (ADR 0033)", aiReviewWorkflowPath)
		} else if entry.Value != "{}" {
			t.Errorf("%s: line %d: the workflow's permissions: is %q, want exactly {}: GITHUB_TOKEN's scopes are the job's alone (ADR 0033)", aiReviewWorkflowPath, entry.Num, entry.Value)
		}

		// Both calls fail on a scope but contents and pull-requests and on a
		// value but read; the job alone needs both, the workflow level may
		// grant nothing.
		aiReviewPermissions(t, workflow.lines, "the workflow")

		jobScopes := aiReviewPermissions(t, workflow.job.Block, "job "+workflow.job.Key)

		for _, scope := range []string{"contents", "pull-requests"} {
			if jobScopes[scope] != "read" {
				t.Errorf("%s: job %s: permissions: does not set %s: read; the job token reads the repository and the pull request, and the App's token writes the review (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, scope)
			}
		}
	})

	t.Run("the job runs in the ai-review environment", func(t *testing.T) {
		t.Parallel()

		environment, ok := aiReviewEntry(t, workflow.job.Block, "environment")
		if !ok {
			t.Fatalf("%s: job %s has no environment:, so it holds no secret of the ai-review environment (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key)
		}

		name := aiReviewScalar(t, environment)
		if name == "" {
			named, ok := aiReviewEntry(t, environment.Block, "name")
			if !ok {
				t.Fatalf("%s: job %s: environment: names no environment", aiReviewWorkflowPath, workflow.job.Key)
			}

			name = aiReviewScalar(t, named)
		}

		if name != "ai-review" {
			t.Errorf("%s: job %s runs in the environment %q, want ai-review: that environment is limited to main and holds the reviewer App's client ID and key, the model key and the switch (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, name)
		}
	})

	t.Run("a draft pull request or one from a fork starts no review", func(t *testing.T) {
		t.Parallel()

		guard, ok := aiReviewEntry(t, workflow.job.Block, "if")
		if !ok {
			t.Fatalf("%s: job %s has no if:, so a draft pull request starts a review (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key)
		}

		condition := aiReviewScalar(t, guard)

		if !strings.Contains(condition, "draft") {
			t.Errorf("%s: job %s: if: %q does not skip drafts, so a draft pull request starts a review; marking it ready starts one (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, condition)
		}

		// A fork must not spend the model budget, and its pull request's code is
		// never what this job checks out: the head repository is this one.
		for _, want := range []string{"head.repo.full_name", "github.repository"} {
			if !strings.Contains(condition, want) {
				t.Errorf("%s: job %s: if: %q does not compare %s with the repository, so a pull request from a fork starts a review and spends the model budget (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, condition, want)
			}
		}
	})

	t.Run("a new push cancels the review in progress", func(t *testing.T) {
		t.Parallel()

		concurrency, ok := aiReviewEntry(t, workflow.lines, "concurrency")
		if !ok {
			t.Fatalf("%s: the workflow has no concurrency:, so a new push reviews beside the run in progress and both spend the model budget (ADR 0033)", aiReviewWorkflowPath)
		}

		if concurrency.Value != "" {
			t.Fatalf("%s: line %d: concurrency: %s, want a block with group: and cancel-in-progress:", aiReviewWorkflowPath, concurrency.Num, concurrency.Value)
		}

		fields, err := yamlread.Mapping(concurrency.Block)
		if err != nil {
			t.Fatalf("%s: read concurrency:: %v", aiReviewWorkflowPath, err)
		}

		var group, cancel string

		for _, field := range fields {
			switch field.Key {
			case "group":
				group = aiReviewScalar(t, field)
			case "cancel-in-progress":
				cancel = aiReviewScalar(t, field)
			default:
				t.Errorf("%s: line %d: concurrency: has %s:, want group: and cancel-in-progress: only", aiReviewWorkflowPath, field.Num, field.Key)
			}
		}

		if !strings.Contains(group, "number") {
			t.Errorf("%s: concurrency: group: %q is not keyed on the pull request's number, so two pull requests cancel each other (ADR 0033)", aiReviewWorkflowPath, group)
		}

		if cancel != "true" {
			t.Errorf("%s: concurrency: cancel-in-progress: %q, want true: a new push cancels the review in progress (ADR 0033)", aiReviewWorkflowPath, cancel)
		}
	})

	t.Run("no step checks out the pull request", func(t *testing.T) {
		t.Parallel()

		checkouts := 0

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			uses, ok := aiReviewUses(t, fields)
			if !ok || !strings.HasPrefix(uses, "actions/checkout@") {
				continue
			}

			checkouts++

			with, ok := aiReviewFind(fields, "with")
			if !ok {
				continue
			}

			withFields, err := yamlread.Mapping(with.Block)
			if err != nil {
				t.Fatalf("%s: line %d: read the checkout's with:: %v", aiReviewWorkflowPath, with.Num, err)
			}

			for _, field := range withFields {
				if field.Key == "ref" || field.Key == "repository" {
					t.Errorf("%s: line %d: the checkout sets %s: %s; the workflow checks out main only, for the program and its instructions, never the pull request's code (ADR 0033)", aiReviewWorkflowPath, field.Num, field.Key, aiReviewScalar(t, field))
				}
			}
		}

		if checkouts == 0 {
			t.Errorf("%s: no step checks out main with actions/checkout, so the reviewer's program and its instructions are not there", aiReviewWorkflowPath)
		}
	})

	t.Run("the checkout leaves no credential in the git config", func(t *testing.T) {
		t.Parallel()

		checkouts := 0

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			uses, ok := aiReviewUses(t, fields)
			if !ok || !strings.HasPrefix(uses, "actions/checkout@") {
				continue
			}

			checkouts++

			persist, ok := aiReviewWithScalar(t, fields, "persist-credentials")
			if !ok {
				t.Errorf("%s: line %d: the checkout sets no persist-credentials:, so the job token stays in .git/config and every later step can read it (ADR 0033)", aiReviewWorkflowPath, step.Num)

				continue
			}

			if persist != "false" {
				t.Errorf("%s: line %d: the checkout sets persist-credentials: %s, want false: the job token stays out of .git/config (ADR 0033)", aiReviewWorkflowPath, step.Num, persist)
			}
		}

		if checkouts == 0 {
			t.Errorf("%s: no step checks out main with actions/checkout, so the reviewer's program and its instructions are not there", aiReviewWorkflowPath)
		}
	})

	t.Run("the go build cache is off", func(t *testing.T) {
		t.Parallel()

		setupSteps := 0

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			uses, ok := aiReviewUses(t, fields)
			if !ok || !strings.HasPrefix(uses, "actions/setup-go@") {
				continue
			}

			setupSteps++

			cache, ok := aiReviewWithScalar(t, fields, "cache")
			if !ok {
				t.Errorf("%s: line %d: setup-go sets no cache:, so it caches something by default; a job that holds two secrets restores nothing another run saved (ADR 0033)", aiReviewWorkflowPath, step.Num)

				continue
			}

			if cache != "false" {
				t.Errorf("%s: line %d: setup-go sets cache: %s, want false: a job that holds two secrets restores nothing another run saved (ADR 0033)", aiReviewWorkflowPath, step.Num, cache)
			}
		}

		if setupSteps == 0 {
			t.Errorf("%s: no step sets up Go with actions/setup-go, so the reviewer cannot be built", aiReviewWorkflowPath)
		}
	})

	t.Run("the Claude Code CLI is installed at a pinned version and verified", func(t *testing.T) {
		t.Parallel()

		index, run, ok := aiReviewClaudeInstall(t, workflow)
		if !ok {
			t.Fatalf("%s: no step installs the Claude Code CLI; the reviewer asks its model through `claude -p` (ADR 0033)", aiReviewWorkflowPath)
		}

		script := aiReviewText(run)
		lines := aiReviewRunLines(run)

		install := -1
		version := ""

		for i, line := range lines {
			match := aiReviewClaudeVersion.FindStringSubmatch(line.text)
			if match == nil {
				continue
			}

			install, version = i, match[1]

			break
		}

		if install < 0 {
			t.Fatalf("%s: line %d: the install step names no exact version of the CLI, want @anthropic-ai/claude-code@X.Y.Z: a range or latest would change the program the reviewer runs without a commit (CLAUDE.md rule 7)", aiReviewWorkflowPath, run.Num)
		}

		for _, loose := range []string{"latest", "^", "~", "*"} {
			if strings.Contains(lines[install].text, loose) {
				t.Errorf("%s: line %d: the CLI's install line %q holds %q; it names the exact version %s and no range (CLAUDE.md rule 7)", aiReviewWorkflowPath, lines[install].num, lines[install].text, loose, version)
			}
		}

		above := aiReviewSourceLine(t, workflow, lines[install].num-1)
		if !strings.HasPrefix(strings.TrimSpace(above), "# renovate:") ||
			!strings.Contains(above, "datasource=npm") ||
			!strings.Contains(above, "depName=@anthropic-ai/claude-code") {
			t.Errorf("%s: line %d: the line above the CLI's version pin is %q, want a # renovate: datasource=npm depName=@anthropic-ai/claude-code comment, so Renovate finds the pin and no one bumps it by hand (CLAUDE.md rule 7)", aiReviewWorkflowPath, lines[install].num-1, above)
		}

		// The reviewer runs a downloaded program that holds a subscription
		// token: the step compares the download against a digest pinned here,
		// so a changed tarball is refused rather than run (ADR 0033).
		if !strings.Contains(script, "sha512") && !strings.Contains(script, "sha256sum") {
			t.Errorf("%s: line %d: the install step verifies no checksum; it compares the download against a pinned digest (ADR 0033)", aiReviewWorkflowPath, run.Num)
		}

		if !aiReviewChecksum.MatchString(script) {
			t.Errorf("%s: line %d: the install step holds no pinned digest, want a SHA-256 in hex or npm's sha512-<base64> integrity to verify the download against", aiReviewWorkflowPath, run.Num)
		}

		// The CLI is installed before any secret is in a step's environment, so
		// nothing it writes while installing can read one.
		for i := 0; i <= index; i++ {
			fields := aiReviewStepFields(t, workflow.steps[i])

			for _, key := range []string{"env", "run"} {
				entry, ok := aiReviewFind(fields, key)
				if !ok || !strings.Contains(aiReviewText(entry), "secrets.") {
					continue
				}

				t.Errorf("%s: line %d: the step that installs the CLI, or one before it, reads a secret in its %s:; the CLI is installed before any secret is in a step's environment (ADR 0033)", aiReviewWorkflowPath, entry.Num, key)
			}
		}
	})

	t.Run("the review step holds the subscription token and no API key", func(t *testing.T) {
		t.Parallel()

		steps := 0

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			env, ok := aiReviewFind(fields, "env")
			if !ok {
				continue
			}

			if _, ok := aiReviewEnvValue(env, "AI_REVIEW_PR_NUMBER"); !ok {
				continue
			}

			steps++

			token, ok := aiReviewEnvValue(env, "CLAUDE_CODE_OAUTH_TOKEN")
			if !ok {
				t.Errorf("%s: line %d: the step that runs the reviewer sets no CLAUDE_CODE_OAUTH_TOKEN; `claude` authenticates with the maintainer's subscription through it (ADR 0033)", aiReviewWorkflowPath, env.Num)
			} else if want := "${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}"; token != want {
				t.Errorf("%s: line %d: CLAUDE_CODE_OAUTH_TOKEN is %q, want %q: the token of the ai-review environment's secret, and no other value (ADR 0033)", aiReviewWorkflowPath, env.Num, token, want)
			}

			if _, ok := aiReviewEnvValue(env, "ANTHROPIC_API_KEY"); ok {
				t.Errorf("%s: line %d: the step that runs the reviewer sets ANTHROPIC_API_KEY; Claude Code bills the API instead of the maintainer's subscription whenever one is set (ADR 0033)", aiReviewWorkflowPath, env.Num)
			}

			run, ok := aiReviewFind(fields, "run")
			if !ok {
				continue
			}

			if strings.Contains(aiReviewText(run), "${{ secrets.") {
				t.Errorf("%s: line %d: the reviewer's run: script interpolates a secret; the token reaches the program through env: only (ADR 0033)", aiReviewWorkflowPath, run.Num)
			}
		}

		if steps != 1 {
			t.Errorf("%s: %d steps set AI_REVIEW_PR_NUMBER, want the one that runs the reviewer", aiReviewWorkflowPath, steps)
		}
	})

	t.Run("no step sets an Anthropic API key", func(t *testing.T) {
		t.Parallel()

		for i, line := range strings.Split(string(workflow.src), "\n") {
			if !strings.Contains(line, "ANTHROPIC_API_KEY") {
				continue
			}

			// A comment that says why it must not be set is what the workflow
			// should hold; an env key or a secret expression is not.
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}

			t.Errorf("%s: line %d: %q names ANTHROPIC_API_KEY; Claude Code bills the API instead of the maintainer's subscription whenever one is set, so no step sets or reads one (ADR 0033)", aiReviewWorkflowPath, i+1, strings.TrimSpace(line))
		}
	})

	t.Run("the App token gets exactly read contents and write pull requests", func(t *testing.T) {
		t.Parallel()

		tokenSteps := 0

		want := map[string]string{
			"permission-contents":      "read",
			"permission-pull-requests": "write",
		}

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			uses, ok := aiReviewUses(t, fields)
			if !ok || !strings.HasPrefix(uses, "actions/create-github-app-token@") {
				continue
			}

			tokenSteps++

			withFields, ok := aiReviewWithFields(t, fields)
			if !ok {
				t.Fatalf("%s: line %d: the App token step has no with:, so it mints no token", aiReviewWorkflowPath, step.Num)
			}

			got := map[string]string{}

			for _, field := range withFields {
				if strings.HasPrefix(field.Key, "permission-") {
					got[field.Key] = aiReviewScalar(t, field)
				}
			}

			if !maps.Equal(want, got) {
				t.Errorf("%s: line %d: the App token asks for %v, want exactly %v: Pull requests write posts and dismisses its reviews, Contents read reads the pull request, and no other permission widens the token (ADR 0033)", aiReviewWorkflowPath, step.Num, got, want)
			}
		}

		if tokenSteps == 0 {
			t.Errorf("%s: no step mints the reviewer App's token with actions/create-github-app-token, so nothing can post the review", aiReviewWorkflowPath)
		}
	})

	t.Run("the program is built before a step holds a secret", func(t *testing.T) {
		t.Parallel()

		buildAt := -1
		secretSteps := 0

		for i, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			if run, ok := aiReviewFind(fields, "run"); ok && buildAt < 0 {
				script := aiReviewText(run)
				if strings.Contains(script, "go build") && strings.Contains(script, "ci/aireview") {
					buildAt = i
				}
			}

			readsSecret := false

			for _, key := range []string{"env", "with"} {
				if entry, ok := aiReviewFind(fields, key); ok && strings.Contains(aiReviewText(entry), "secrets.") {
					readsSecret = true
				}
			}

			if !readsSecret {
				continue
			}

			secretSteps++

			if buildAt < 0 || i == buildAt {
				t.Errorf("%s: line %d: a step reads a secret at or before the step that builds the reviewer; main's program is built first, so no secret is in a step's environment while it runs (ADR 0033)", aiReviewWorkflowPath, step.Num)
			}
		}

		if buildAt < 0 {
			t.Fatalf("%s: no step runs \"go build\" of ./ci/aireview, so the workflow never builds the reviewer from main", aiReviewWorkflowPath)
		}

		if secretSteps == 0 {
			t.Errorf("%s: no step reads a secret, so nothing mints the reviewer App's token or holds the model key", aiReviewWorkflowPath)
		}
	})

	t.Run("every action is pinned to a commit", func(t *testing.T) {
		t.Parallel()

		pins := 0

		for _, step := range workflow.steps {
			action, ok := aiReviewUses(t, aiReviewStepFields(t, step))
			if !ok {
				continue
			}

			pins++

			if !aiReviewPinnedAction.MatchString(action) {
				t.Errorf("%s: line %d: uses: %s is not pinned to a 40-hex-digit commit; every action is pinned by SHA (CLAUDE.md rule 7, ADR 0033)", aiReviewWorkflowPath, step.Num, action)
			}
		}

		if pins == 0 {
			t.Errorf("%s: no step uses an action, so no step mints the reviewer App's token", aiReviewWorkflowPath)
		}
	})

	t.Run("the secrets and the switch are read, and no script holds pull request data", func(t *testing.T) {
		t.Parallel()

		doc := string(workflow.src)

		for _, want := range []string{
			"secrets.AI_REVIEWER_APP_CLIENT_ID",
			"secrets.AI_REVIEWER_APP_PRIVATE_KEY",
			"secrets.CLAUDE_CODE_OAUTH_TOKEN",
			"vars.AI_REVIEW_REQUEST_CHANGES",
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s: nothing reads %s; the ai-review environment holds the reviewer App's client ID and key, the maintainer's Claude subscription token and the switch, and no repository-level secret holds them (ADR 0033)", aiReviewWorkflowPath, want)
			}
		}

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			run, ok := aiReviewFind(fields, "run")
			if !ok {
				continue
			}

			script := run.Value
			for _, line := range run.Block {
				script += "\n" + line.Text
			}

			for _, refused := range []string{
				"github.event.pull_request.title",
				"github.event.pull_request.body",
			} {
				if strings.Contains(script, refused) {
					t.Errorf("%s: line %d: the run: script holds %s; pull request data reaches a script through env: only, never through an expression (ADR 0033)", aiReviewWorkflowPath, run.Num, refused)
				}
			}

			if strings.Contains(script, "${{") {
				t.Errorf("%s: line %d: the run: script holds a ${{ }} expression; every value reaches a script through env: only, never through an expression (ADR 0033)", aiReviewWorkflowPath, run.Num)
			}
		}
	})
}

// aiReviewEntry returns the entry of the mapping lines for key, and false when
// the mapping holds none.
func aiReviewEntry(t *testing.T, lines []yamlread.Line, key string) (yamlread.Entry, bool) {
	t.Helper()

	return aiReviewFind(aiReviewFields(t, lines, key), key)
}

// numberedLine is one line of a step's run: script beside the number of the
// workflow's source line it is.
type numberedLine struct {
	num  int
	text string
}

// aiReviewRunLines returns the lines of a step's run: script with their source
// line numbers: the inline value alone, or the block's lines.
func aiReviewRunLines(run yamlread.Entry) []numberedLine {
	if run.Value != "" {
		return []numberedLine{{num: run.Num, text: run.Value}}
	}

	lines := make([]numberedLine, 0, len(run.Block))
	for _, line := range run.Block {
		lines = append(lines, numberedLine{num: line.Num, text: line.Text})
	}

	return lines
}

// aiReviewSourceLine returns the workflow's source line num, counted from 1.
func aiReviewSourceLine(t *testing.T, workflow aiReviewWorkflow, num int) string {
	t.Helper()

	lines := strings.Split(string(workflow.src), "\n")
	if num < 1 || num > len(lines) {
		t.Fatalf("%s: the workflow has no line %d", aiReviewWorkflowPath, num)
	}

	return lines[num-1]
}

// aiReviewClaudeInstall returns the index of the step that installs the Claude
// Code CLI, its run: entry, and false when no step installs it. The step is the
// one whose script names the CLI's npm package.
func aiReviewClaudeInstall(t *testing.T, workflow aiReviewWorkflow) (int, yamlread.Entry, bool) {
	t.Helper()

	for i, step := range workflow.steps {
		run, ok := aiReviewFind(aiReviewStepFields(t, step), "run")
		if !ok || !strings.Contains(aiReviewText(run), "@anthropic-ai/claude-code") {
			continue
		}

		return i, run, true
	}

	return -1, yamlread.Entry{}, false
}

// aiReviewFields returns the entries of the block mapping lines.
func aiReviewFields(t *testing.T, lines []yamlread.Line, where string) []yamlread.Entry {
	t.Helper()

	fields, err := yamlread.Mapping(lines)
	if err != nil {
		t.Fatalf("%s: read the mapping that holds %s: %v", aiReviewWorkflowPath, where, err)
	}

	return fields
}

// aiReviewFind returns the entry of fields for key, and false when fields holds
// none.
func aiReviewFind(fields []yamlread.Entry, key string) (yamlread.Entry, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
	}

	return yamlread.Entry{}, false
}

// aiReviewStepFields returns the keys of one step.
func aiReviewStepFields(t *testing.T, step yamlread.Item) []yamlread.Entry {
	t.Helper()

	return aiReviewFields(t, step.Lines, "a step's keys")
}

// aiReviewUses returns the action one step uses, as written, and false when the
// step uses none. The value is read as a scalar, so a version comment after the
// pin (" # v7.0.1") is no part of it.
func aiReviewUses(t *testing.T, fields []yamlread.Entry) (string, bool) {
	t.Helper()

	uses, ok := aiReviewFind(fields, "uses")
	if !ok {
		return "", false
	}

	return aiReviewScalar(t, uses), true
}

// aiReviewScalar reads the value of e as one scalar, with a trailing comment
// removed.
func aiReviewScalar(t *testing.T, e yamlread.Entry) string {
	t.Helper()

	value, err := yamlread.Single(e)
	if err != nil {
		t.Fatalf("%s: line %d: read %s:: %v", aiReviewWorkflowPath, e.Num, e.Key, err)
	}

	return value
}

// aiReviewText returns every text of e: its value and the lines of its block,
// joined by newlines.
func aiReviewText(e yamlread.Entry) string {
	texts := []string{e.Value}
	for _, line := range e.Block {
		texts = append(texts, line.Text)
	}

	return strings.Join(texts, "\n")
}

// aiReviewWithFields returns the entries of one step's with: block, and false
// when the step has none.
func aiReviewWithFields(t *testing.T, fields []yamlread.Entry) ([]yamlread.Entry, bool) {
	t.Helper()

	with, ok := aiReviewFind(fields, "with")
	if !ok {
		return nil, false
	}

	return aiReviewFields(t, with.Block, "a step's with:"), true
}

// aiReviewWithScalar returns the value of key in one step's with: block, and
// false when the step has no with: block or it does not set key.
func aiReviewWithScalar(t *testing.T, fields []yamlread.Entry, key string) (string, bool) {
	t.Helper()

	withFields, ok := aiReviewWithFields(t, fields)
	if !ok {
		return "", false
	}

	for _, field := range withFields {
		if field.Key == key {
			return aiReviewScalar(t, field), true
		}
	}

	return "", false
}

// aiReviewEnvValue returns the value the env: entry sets for name, as written on
// its line and without a trailing comment, and false when it sets none. A step's
// env: names its variables in upper case, which yamlread.Mapping refuses as keys,
// so this reads the block's lines as written.
func aiReviewEnvValue(env yamlread.Entry, name string) (string, bool) {
	for _, line := range env.Block {
		key, value, ok := strings.Cut(line.Text, ":")
		if !ok || key != name {
			continue
		}

		value = strings.TrimSpace(value)
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}

		return value, true
	}

	return "", false
}

// aiReviewPermissions returns the permissions the mapping lines declare at
// where, and fails on a scope but contents and pull-requests and on a value but
// read, so no step of the workflow can write with GITHUB_TOKEN.
func aiReviewPermissions(t *testing.T, lines []yamlread.Line, where string) map[string]string {
	t.Helper()

	entry, ok := aiReviewEntry(t, lines, "permissions")
	if !ok {
		t.Errorf("%s: %s declares no permissions:, so it takes the repository's default token permissions", aiReviewWorkflowPath, where)

		return nil
	}

	scopes := map[string]string{}

	switch {
	case entry.Value == "{}":
		// Declared and empty: the workflow grants nothing.
		return scopes
	case entry.Value != "":
		t.Errorf("%s: line %d: %s: permissions: %q is not a block of scope: value pairs", aiReviewWorkflowPath, entry.Num, where, entry.Value)

		return scopes
	}

	for _, scope := range aiReviewFields(t, entry.Block, where+": permissions:") {
		if scope.Key != "contents" && scope.Key != "pull-requests" {
			t.Errorf("%s: line %d: %s: permissions: grants %s: %s; the job token reads the repository and the pull request, and nothing else (ADR 0033)", aiReviewWorkflowPath, scope.Num, where, scope.Key, scope.Value)

			continue
		}

		if scope.Value != "read" {
			t.Errorf("%s: line %d: %s: permissions: sets %s: %s, want read: the review is written with the App's token, never with GITHUB_TOKEN (ADR 0033)", aiReviewWorkflowPath, scope.Num, where, scope.Key, aiReviewScalar(t, scope))
		}

		scopes[scope.Key] = aiReviewScalar(t, scope)
	}

	return scopes
}
