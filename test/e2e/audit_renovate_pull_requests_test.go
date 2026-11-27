//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file runs row 15 of .github/workflows/scheduled-security-audit.yml, the
// read of Renovate's open pull requests, against answers in the shape GitHub
// gives them (issue #660). The read used the search endpoint, and in the first
// audit run it answered with the Dependency Dashboard issue and no pull request
// at all: the search's answer depends on the token, while the pulls endpoint
// lists what the repository holds. So the read calls the pulls endpoint, one
// page of 100 at a time, and keeps the pull requests Renovate's own through a jq
// filter that keeps the named fields alone.
//
// The test takes the read out of the workflow the release runs, checks that it
// calls the pulls endpoint with that filter, and runs the filter against answers
// in the shape GitHub gives, synthesized and sanitised here: a pulls page
// holding a pull request by renovate[bot] and one by another author, and a
// search answer holding the Dependency Dashboard issue beside a pull request.
// It lives in the e2e tier, beside the tests that run the guide's jq programs,
// because it starts a process and needs the jq the workflow's own step calls,
// which the default tests never require.

const (
	// auditRenovateWorkflow is the workflow the audit runs, read from the test's
	// own working directory (test/e2e): the document a released audit is.
	auditRenovateWorkflow = "../../.github/workflows/scheduled-security-audit.yml"
	// auditRenovatePullsAnswer is an answer in the shape the pulls endpoint gives,
	// synthesized here: the repository's open pull requests, Renovate's first and
	// another author's second, every field the API answers with.
	auditRenovatePullsAnswer = "testdata/audit-renovate/pulls-page-1.json"
	// auditRenovateSearchAnswer is an answer in the shape the search endpoint
	// gives, synthesized here: the Dependency Dashboard issue by renovate[bot]
	// and one of its pull requests, beside each other in one list of items. It is
	// the answer the read used to be run against.
	auditRenovateSearchAnswer = "testdata/audit-renovate/search-issues.json"
	// auditRenovatePullsEndpoint is the endpoint the read must call: the open pull
	// requests of the repository, 100 to a page, the page number a variable the
	// workflow's own loop advances.
	auditRenovatePullsEndpoint = "repos/${GH_REPO}/pulls?state=open&per_page=100&page=${page}"
	// auditRenovateFilter is the jq filter the read must pass: count, the page's
	// unfiltered length, so that a full page means another may follow, beside the
	// pull requests renovate[bot] opened, each of the named fields and no body, a
	// text the model would read as instructions (its title's included).
	auditRenovateFilter = `{count: length, renovate: [.[] | select(.user.login == "renovate[bot]") | {number, title, draft, created_at, updated_at, head: .head.ref, labels: [.labels[].name]}]}`
)

// auditRenovateReadLine matches one read_api call of the audit's supply-chain
// step: the name the answer is written under, quoted or bare, the endpoint in
// double quotes, and the jq filter in single quotes when the call passes one.
var auditRenovateReadLine = regexp.MustCompile(`(?m)^[ \t]*read_api[ \t]+(?:"([^"\n]*)"|(\S+))[ \t]+"([^"\n]*)"(?:[ \t]+'([^'\n]*)')?[ \t]*$`)

// TestScheduledSecurityAuditReadsRenovatesPullRequests holds the audit's read of
// Renovate's open pull requests to the pulls endpoint and its filter, and runs
// that filter against answers in the shape GitHub gives (issue #660).
func TestScheduledSecurityAuditReadsRenovatesPullRequests(t *testing.T) {
	t.Parallel()

	jq := jqPath(t)
	workflow := readAuditRenovateAnswer(t, auditRenovateWorkflow)
	name, endpoint, filter := auditRenovateRead(t, workflow)

	t.Run("the pulls endpoint", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff(auditRenovatePullsEndpoint, endpoint); diff != "" {
			t.Errorf("%s's read of Renovate's pull requests (%s) calls the endpoint %q, want the pulls endpoint (-want +got):\n%s\n"+
				"the search endpoint the read used before answered the first audit run with the Dependency Dashboard issue and no pull request, so the read lists the repository's open pull requests instead",
				auditRenovateWorkflow, name, endpoint, diff)
		}
	})

	t.Run("the filter that keeps Renovate's pull requests", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff(auditRenovateFilter, filter); diff != "" {
			t.Errorf("%s's read of Renovate's pull requests passes the jq filter %q, want the one that keeps count and renovate[bot]'s pull requests alone (-want +got):\n%s\n"+
				"read_api falls back to . for a call that names no filter, so an answer used unfiltered hands the model every author's pull request, its body included",
				auditRenovateWorkflow, filter, diff)
		}
	})

	t.Run("the pulls page", func(t *testing.T) {
		t.Parallel()

		answer := readAuditRenovateAnswer(t, auditRenovatePullsAnswer)

		got := decodeAuditRenovateAnswer(t, auditJQ(t, jq, auditRenovateFilterOrNothing(filter), answer))

		want := map[string]any{
			"count": float64(2),
			"renovate": []any{
				map[string]any{
					"number":     float64(1),
					"title":      "Update the example action to v2",
					"draft":      false,
					"created_at": "2024-01-01T00:00:00Z",
					"updated_at": "2024-01-02T00:00:00Z",
					"head":       "renovate/example-action-2.x",
					"labels":     []any{"dependencies"},
				},
			},
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the read's filter on %s gives (-want +got):\n%s\n"+
				"want count 2, the page's two pull requests, beside Renovate's one alone, its named fields and its head ref, and no body, and never the other author's pull request",
				auditRenovatePullsAnswer, diff)
		}
	})

	// The search answer illustrates why the old search read was wrong: it holds
	// an issue beside a pull request, and neither has a head ref. The read the
	// workflow makes is pinned above; this subtest pins no behaviour of the
	// workflow and only shows the answer the search endpoint gave.
	t.Run("the search answer", func(t *testing.T) {
		t.Parallel()

		answer := readAuditRenovateAnswer(t, auditRenovateSearchAnswer)

		// The search endpoint answers with an object whose items: holds the
		// issues and the pull requests alike, so the same filter reads .items.
		got := decodeAuditRenovateAnswer(t, auditJQ(t, jq, ".items | "+auditRenovateFilterOrNothing(filter), answer))

		want := map[string]any{
			"count": float64(2),
			"renovate": []any{
				map[string]any{
					"number":     float64(1),
					"title":      "Dependency Dashboard",
					"draft":      nil,
					"created_at": "2024-01-01T00:00:00Z",
					"updated_at": "2024-01-02T00:00:00Z",
					"head":       nil,
					"labels":     []any{"dependencies"},
				},
				map[string]any{
					"number":     float64(2),
					"title":      "Update example dependency to 2.0.0",
					"draft":      nil,
					"created_at": "2024-01-03T00:00:00Z",
					"updated_at": "2024-01-04T00:00:00Z",
					"head":       nil,
					"labels":     []any{"dependencies"},
				},
			},
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the read's filter on %s gives (-want +got):\n%s\n"+
				"the search answer keeps the Dependency Dashboard issue beside a pull request and gives neither a head ref, which is why the read asks the pulls endpoint, whose answer holds pull requests alone",
				auditRenovateSearchAnswer, diff)
		}
	})
}

// auditRenovateFilterOrNothing returns filter, or read_api's own default . for a
// read that names none: the answer the workflow as it stands hands the model.
func auditRenovateFilterOrNothing(filter string) string {
	if filter == "" {
		return "."
	}

	return filter
}

// auditRenovateRead returns the name, endpoint and jq filter of the workflow's
// one read_api call of Renovate's pull requests: the call whose name begins
// renovate-pull-requests. A workflow with no such call, or with two, fails the
// test: the pin is one read, named so.
func auditRenovateRead(t *testing.T, workflow []byte) (name, endpoint, filter string) {
	t.Helper()

	var names []string

	for _, m := range auditRenovateReadLine.FindAllSubmatch(workflow, -1) {
		call := string(m[1])
		if call == "" {
			call = string(m[2])
		}

		if !strings.HasPrefix(call, "renovate-pull-requests") {
			continue
		}

		names = append(names, call)
		name, endpoint, filter = call, string(m[3]), string(m[4])
	}

	if len(names) != 1 {
		t.Fatalf("%s holds %d read_api calls of Renovate's pull requests (%q), want the one whose name begins renovate-pull-requests: the read lists the repository's open pull requests page by page",
			auditRenovateWorkflow, len(names), names)
	}

	return name, endpoint, filter
}

// readAuditRenovateAnswer returns the answer, in the shape GitHub gives, or the
// workflow at path.
func readAuditRenovateAnswer(t *testing.T, path string) []byte {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v; the file is an answer in the shape GitHub gives the read", path, err)
	}

	return src
}

// auditJQ runs program with jq on input and returns what jq printed. A jq that
// cannot run or that refuses the program fails the test: the filter is the one
// the released audit runs, so it has to run on the answer the API gives.
func auditJQ(t *testing.T, jq, program string, input []byte) []byte {
	t.Helper()

	cmd := exec.Command(jq, program)
	cmd.Stdin = bytes.NewReader(input)
	// The program reads its input from standard input and names no file: it
	// needs no environment.
	cmd.Env = []string{}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		t.Fatalf("jq %s exited %d on the answer, want 0; stderr = %q", program, exitErr.ExitCode(), stderr.String())
	default:
		t.Fatalf("run jq %s: %v", program, err)
	}

	if stderr.Len() != 0 {
		t.Errorf("jq wrote to stderr, want nothing: %q", stderr.String())
	}

	return stdout.Bytes()
}

// decodeAuditRenovateAnswer decodes what jq printed into the document's own
// shape, so that a field the filter keeps that the API gives another type fails
// here: the filter's named fields are what the model reads.
func decodeAuditRenovateAnswer(t *testing.T, document []byte) map[string]any {
	t.Helper()

	var doc map[string]any

	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("decode the filter's answer %q: %v; want the object {count, renovate} the filter writes", string(document), err)
	}

	return doc
}
