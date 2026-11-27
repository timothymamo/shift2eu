//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// This file runs a command docs/guide/inventory.md documents, not just reads
// its wording: the block that pages `opensearchserverless list-collections` by
// hand (review finding S-A). The AWS CLI has no paginator for that call, so the
// guide writes one JSON value per page into the collections file itself, and
// the program reads every value. A visitor who copies the block gets a
// truncated inventory if the loop stops after the first page, or something the
// visitor's shell cannot run at all, and nothing in the guide would say so.
//
// The block is extracted from the guide's markdown and run by a POSIX shell
// against a fake aws program that the test writes and puts first on PATH: no
// network, no real aws, no credentials (CLAUDE.md rules 1 and 3). It lives in
// the e2e tier, beside the tests that run the guide's jq programs, because it
// starts processes and needs the jq the guide's own block calls, which the
// default tests never require.

// inventoryGuidePath is the guide whose block this file runs. The test's
// working directory is test/e2e.
const inventoryGuidePath = "../../docs/guide/inventory.md"

// listCollectionsFile is the file the list-collections block writes: one page
// of the call's output per JSON value, and what the inventory program reads.
const listCollectionsFile = "opensearchserverless-collections.json"

// listCollectionsBlockStart is the guide's first line of the block: it names the
// file the pages are appended to, which the rest of the block writes through.
const listCollectionsBlockStart = `f="$out/` + listCollectionsFile + `"`

// fakeCallSeparator is the line a fake program writes before each call's
// arguments, so a call's words stay apart from the next call's.
const fakeCallSeparator = "--call--"

// fakeAWSResponse is the answer the fake aws program gives to one call: the
// JSON value it prints to standard output, or the failure it reports.
type fakeAWSResponse struct {
	// stdout is the JSON value the call prints, empty for a call that fails.
	stdout string
	// stderr is the message the call prints to standard error, empty for a
	// call that succeeds.
	stderr string
	// status is the exit status of the call, 0 for a call that succeeds.
	status int
}

// listCollectionsRun is what one run of the guide's block did: the calls the
// block made, everything it wrote to standard error, the status its shell
// exited with, and the JSON values the collections file holds afterwards.
type listCollectionsRun struct {
	// calls holds one entry per aws invocation, in order, each the call's
	// arguments as the separate words the shell passed.
	calls [][]string
	// stderr is everything the block wrote to standard error.
	stderr string
	// exitStatus is the status the shell exited with.
	exitStatus int
	// collections are the JSON values the collections file holds, one per
	// page the block wrote.
	collections []json.RawMessage
}

// twoPages are the two pages the fake aws answers a successful run with: the
// first names the continuation token of the second, and the second names none,
// as the last page of a real call does.
var twoPages = []fakeAWSResponse{
	{stdout: `{"collectionSummaries": [{"id": "0123456789abcdef0123456789abcdef", "name": "shop-vectors"}], "nextToken": "t2"}`},
	{stdout: `{"collectionSummaries": [{"id": "fedcba9876543210fedcba9876543210", "name": "shop-thumbnails"}], "nextToken": null}`},
}

// failingSecondPage is page one of twoPages followed by a page the call fails
// on: the truncation the guide has to report rather than write out as if the
// second page did not exist.
var failingSecondPage = []fakeAWSResponse{
	twoPages[0],
	{stderr: "the fake aws refused page 2", status: 254},
}

// failingFirstPage is a run whose very first page fails: no page was written, so
// the collections file has to stay empty and the block has to say why, rather
// than hand the program a file with nothing in it and no word about it.
var failingFirstPage = []fakeAWSResponse{
	{stderr: "the fake aws refused page 1", status: 254},
}

// repeatingToken is two pages that name the same continuation token, which a
// region whose collections are being reindexed can do: a loop that trusts the
// token asks for the same page for ever, and the guide's run never ends.
var repeatingToken = []fakeAWSResponse{
	{stdout: `{"collectionSummaries": [{"id": "0123456789abcdef0123456789abcdef", "name": "shop-vectors"}], "nextToken": "t2"}`},
	{stdout: `{"collectionSummaries": [{"id": "fedcba9876543210fedcba9876543210", "name": "shop-thumbnails"}], "nextToken": "t2"}`},
}

// guideShell is one shell the guide's blocks are run under here.
type guideShell struct {
	// name is how the subtests name the shell.
	name string
	// shell is the shell the block runs under, by name on PATH.
	shell string
	// preamble is written before the block, for a shell that has to run in a
	// stricter mode than its default.
	preamble string
}

// guidePOSIXShells are the shells the guide says its commands are for (bash,
// zsh), the two smaller POSIX ones a visitor may have instead (sh, dash), and a
// bash that stops on an unset variable. The guide promises all of them, so every
// block it documents has to run in every one, whatever words or files it uses
// (review finding N-a).
var guidePOSIXShells = []guideShell{
	{name: "bash", shell: "bash"},
	{name: "zsh", shell: "zsh"},
	{name: "sh", shell: "sh"},
	{name: "dash", shell: "dash"},
	{name: "bash with set -u", shell: "bash", preamble: "set -u\n"},
}

// TestInventoryGuideListCollectionsBlock runs the guide's own list-collections
// block, extracted from docs/guide/inventory.md, against a fake aws program
// (review findings S-A, S-B and N-a): it pages through every page, it says the
// collections file is incomplete when a page fails instead of writing a
// truncated file silently, and it runs under every POSIX shell the guide
// promises.
func TestInventoryGuideListCollectionsBlock(t *testing.T) {
	t.Parallel()

	block := inventoryGuideListCollectionsBlock(t)

	t.Run("pages through every page", func(t *testing.T) {
		t.Parallel()

		run := runInventoryGuideBlock(t, "bash", "", block, twoPages)

		if run.exitStatus != 0 {
			t.Errorf("the block exited %d, want 0, so a visitor's own run would stop there too; stderr = %q",
				run.exitStatus, run.stderr)
		}

		if len(run.calls) != 2 {
			t.Fatalf("the block called aws %d times, want 2: one call per page, and the first page names a token for the second\n%s",
				len(run.calls), formatFakeCalls(run.calls))
		}

		if slices.Contains(run.calls[0], "--next-token") {
			t.Errorf("the first call passes a continuation token, want none: the loop starts at the first page\n%s",
				formatFakeCalls(run.calls))
		}

		token, ok := passedNextToken(run.calls[1])
		switch {
		case !ok:
			t.Errorf("the second call passes no --next-token argument, want --next-token and then t2, as two words, or the call returns the first page again\n%s",
				formatFakeCalls(run.calls))
		case token != "t2":
			t.Errorf("the second call passes --next-token %q, want the token the first page named, t2\n%s",
				token, formatFakeCalls(run.calls))
		}

		if len(run.collections) != 2 {
			t.Errorf("%s holds %d JSON values after two pages, want 2: one value per page, of which the program reads every one",
				listCollectionsFile, len(run.collections))
		}
	})

	t.Run("says the file is incomplete when a page fails", func(t *testing.T) {
		t.Parallel()

		run := runInventoryGuideBlock(t, "bash", "", block, failingSecondPage)

		if len(run.calls) != 2 {
			t.Fatalf("the block called aws %d times, want 2: the second page is the one that fails, so the run has to reach it before it reports anything\n%s",
				len(run.calls), formatFakeCalls(run.calls))
		}

		if !strings.Contains(run.stderr, listCollectionsFile) {
			t.Errorf("the block wrote no message to stderr naming %s, so a run that stopped at the second page leaves a file that looks complete; stderr = %q",
				listCollectionsFile, run.stderr)
		}

		if !strings.Contains(strings.ToLower(run.stderr), "incomplete") {
			t.Errorf("the block's stderr does not say the collections file is incomplete, so the truncation is silent; stderr = %q",
				run.stderr)
		}
	})

	t.Run("says the file is empty when the first page fails", func(t *testing.T) {
		t.Parallel()

		run := runInventoryGuideBlock(t, "bash", "", block, failingFirstPage)

		if run.exitStatus != 0 {
			t.Errorf("the block exited %d, want 0, so the run stops before it reports the failure; stderr = %q",
				run.exitStatus, run.stderr)
		}

		if len(run.calls) != 1 {
			t.Errorf("the block called aws %d times, want 1: the first page is the one that fails, and there is no token to ask a second page with\n%s",
				len(run.calls), formatFakeCalls(run.calls))
		}

		if len(run.collections) != 0 {
			t.Errorf("%s holds %d JSON values after the first page failed, want 0: the block has no page to write, and an empty file it does not report reads as a region without collections",
				listCollectionsFile, len(run.collections))
		}

		if !strings.Contains(run.stderr, listCollectionsFile) {
			t.Errorf("the block wrote no message to stderr naming %s after the first page failed, so a run that collected nothing leaves a file that looks complete; stderr = %q",
				listCollectionsFile, run.stderr)
		}
	})

	t.Run("stops when a page repeats its token", func(t *testing.T) {
		t.Parallel()

		run := runInventoryGuideBlock(t, "bash", "", block, repeatingToken)

		if run.exitStatus != 0 {
			t.Errorf("the block exited %d, want 0, so the run stops before it reports the repeated token; stderr = %q",
				run.exitStatus, run.stderr)
		}

		if len(run.calls) != 2 {
			t.Errorf("the block called aws %d times, want 2: the second page names the token the first one named, so a loop that trusts the token asks the same page for ever\n%s",
				len(run.calls), formatFakeCalls(run.calls))
		}

		if !strings.Contains(run.stderr, listCollectionsFile) {
			t.Errorf("the block wrote no message to stderr naming %s after a page repeated its token, so a run that keeps asking for the same page says nothing about the file it leaves; stderr = %q",
				listCollectionsFile, run.stderr)
		}
	})

	// The guide says its commands are for a POSIX shell (bash, zsh). A bash
	// array, such as one holding the CLI arguments still to pass and expanded
	// as "${next[@]}", is not POSIX: under set -u a bash before 4.4 takes its
	// empty expansion for an unset variable and stops the whole command (review
	// finding N-a). So the block has to run in each of guidePOSIXShells,
	// whatever words it pages with.
	for _, sh := range guidePOSIXShells {
		t.Run("runs under "+sh.name, func(t *testing.T) {
			t.Parallel()

			if _, err := exec.LookPath(sh.shell); err != nil {
				t.Skipf("%s is not installed, so this shell cannot be checked here: %v", sh.shell, err)
			}

			run := runInventoryGuideBlock(t, sh.shell, sh.preamble, block, twoPages)

			if run.exitStatus != 0 {
				t.Errorf("the block exited %d under %s, want 0, so the guide's commands are not the POSIX shell's it says they are; stderr = %q",
					run.exitStatus, sh.name, run.stderr)
			}

			if len(run.calls) != 2 {
				t.Errorf("the block called aws %d times under %s, want 2\n%s",
					len(run.calls), sh.name, formatFakeCalls(run.calls))
			}

			if len(run.collections) != 2 {
				t.Errorf("%s holds %d JSON values under %s, want 2\n",
					listCollectionsFile, len(run.collections), sh.name)
			}
		})
	}
}

// inventoryGuideListCollectionsBlock returns the guide's block that pages
// opensearchserverless list-collections, read from the markdown itself: the
// test runs the command the guide gives, never a copy that could drift from it.
func inventoryGuideListCollectionsBlock(t *testing.T) string {
	t.Helper()

	document, err := os.ReadFile(inventoryGuidePath)
	if err != nil {
		t.Fatalf("read %s: %v", inventoryGuidePath, err)
	}

	lines := strings.Split(string(document), "\n")

	start := -1

	for i, line := range lines {
		if strings.HasPrefix(line, listCollectionsBlockStart) {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s no longer holds the block that pages opensearchserverless list-collections by hand: no line starts with %s",
			inventoryGuidePath, listCollectionsBlockStart)
	}

	end := -1

	for i := start; i < len(lines); i++ {
		if lines[i] == "done" {
			end = i

			break
		}
	}

	if end < 0 {
		t.Fatalf("%s: the list-collections block that starts at line %d has no line that is just `done`, so the loop it documents cannot be read",
			inventoryGuidePath, start+1)
	}

	block := strings.Join(lines[start:end+1], "\n") + "\n"

	// Guard the extraction itself: a block that lost the call, the token it
	// pages with or the loop that repeats it would make every check below pass
	// over something that is not the command the guide documents. Only what any
	// correct hand-paged loop keeps is named here, so the block stays free to
	// pass the token in the shape its shells can run.
	for _, want := range []string{
		"aws opensearchserverless list-collections",
		"--next-token",
		"nextToken",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("%s lines %d-%d hold no %q, so they are not the list-collections block this test runs:\n%s",
				inventoryGuidePath, start+1, end+1, want, block)
		}
	}

	return block
}

// blockRunTimeout bounds one run of the guide's block. A block that never reaches
// its last page, or that waits for something a visitor's shell never gives it,
// fails the check here instead of hanging the test.
const blockRunTimeout = 20 * time.Second

// runInventoryGuideBlock runs block under shellName with preamble prepended,
// against a fake aws program that answers with pages, one per call, and returns
// what the run did. out is a temporary directory, so the guide's own "$out"
// expansion writes under it.
func runInventoryGuideBlock(t *testing.T, shellName, preamble, block string, pages []fakeAWSResponse) listCollectionsRun {
	t.Helper()

	run := runGuideBlock(t, guideBlockOptions{
		shell:    shellName,
		preamble: preamble,
		block:    block,
		fakes: map[string]func(callLog string) []byte{
			"aws": func(callLog string) []byte { return fakeAWSProgram(callLog, callLog+".count", pages) },
		},
	})

	return listCollectionsRun{
		calls:       run.calls,
		stderr:      run.stderr,
		exitStatus:  run.exitStatus,
		collections: decodePages(t, filepath.Join(run.outDir, listCollectionsFile)),
	}
}

// guideBlockOptions describes one run of a block of the guide.
type guideBlockOptions struct {
	// shell is the shell the block runs under, by name on PATH.
	shell string
	// preamble is written before the block, for a shell that has to run in a
	// stricter mode; empty for a plain run.
	preamble string
	// block is the guide's command text itself, never a copy of it.
	block string
	// fakes are the programs the run puts first on PATH, by the name the block
	// calls: each is given the path of the log it records its calls in.
	fakes map[string]func(callLog string) []byte
	// files are the files the run writes into its $out directory before the
	// block starts, by name: what the guide's earlier commands would have left.
	files map[string]string
	// env are the shell variables the block reads, as VARIABLE=value, beside the
	// $out directory the run names itself.
	env []string
}

// guideBlockRun is what one run of a block of the guide did.
type guideBlockRun struct {
	// calls holds one entry per call of the run's fake programs, in order, each
	// the call's arguments as the separate words the shell passed.
	calls [][]string
	// stderr is everything the block wrote to standard error.
	stderr string
	// exitStatus is the status the shell exited with.
	exitStatus int
	// outDir is the block's $out directory, holding the files it wrote.
	outDir string
}

// runGuideBlock runs a block of the guide under a POSIX shell, with the fake
// programs the block calls standing where those programs do: no network, no real
// CLI and no credentials (CLAUDE.md rules 1 and 3). The PATH it runs with names
// the fakes' directory, jq's and the shell's and nothing else, so nothing of the
// test process's environment goes with the run. The deadline is the run's, not
// the test's: a block that never finishes is stopped here and reported.
func runGuideBlock(t *testing.T, options guideBlockOptions) guideBlockRun {
	t.Helper()

	shell, err := exec.LookPath(options.shell)
	if err != nil {
		t.Fatalf("%s is not installed, so the guide's block cannot be run with it: %v", options.shell, err)
	}

	jq := jqPath(t)

	dir := t.TempDir()

	fakeDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeDir, 0o700); err != nil {
		t.Fatalf("create %s: %v", fakeDir, err)
	}

	outDir := filepath.Join(dir, "inventories")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatalf("create %s: %v", outDir, err)
	}

	for name, content := range options.files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s into the run's $out: %v", name, err)
		}
	}

	callLog := filepath.Join(dir, "calls.log")

	for name, program := range options.fakes {
		writeScript(t, filepath.Join(fakeDir, name), program(callLog))
	}

	script := filepath.Join(dir, "block.sh")
	if err := os.WriteFile(script, []byte(options.preamble+options.block), 0o600); err != nil {
		t.Fatalf("write %s: %v", script, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), blockRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, script)
	cmd.Env = append([]string{
		"PATH=" + fakeDir + ":" + filepath.Dir(jq) + ":" + filepath.Dir(shell),
		"out=" + outDir,
	}, options.env...)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	exitStatus := 0

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("the block was still running after %s, so it never finishes: %v; stderr = %q",
				blockRunTimeout, ctx.Err(), stderr.String())
		}

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s %s: %v", shell, script, err)
		}

		exitStatus = exitErr.ExitCode()
	}

	return guideBlockRun{
		calls:      fakeCalls(t, callLog),
		stderr:     stderr.String(),
		exitStatus: exitStatus,
		outDir:     outDir,
	}
}

// fakeAWSProgram returns a shell program that stands where aws does: it records
// the arguments it is given, one call after another, and answers the nth call
// with the nth of pages. A call past the pages fails, so a block that asks for
// a page the test did not record is visible rather than silently answered.
func fakeAWSProgram(callLog, callCount string, pages []fakeAWSResponse) []byte {
	var program strings.Builder

	program.WriteString("#!/bin/sh\n")
	program.WriteString("# A fake aws program, written by the e2e test: it records each call's\n")
	program.WriteString("# arguments and answers with one canned page per call.\n")
	program.WriteString("{\n")
	program.WriteString("  printf '%s\\n' " + shellQuote(fakeCallSeparator) + "\n")
	program.WriteString("  for argument in \"$@\"; do printf '%s\\n' \"$argument\"; done\n")
	program.WriteString("} >> " + shellQuote(callLog) + "\n")
	program.WriteString("count=0\n")
	program.WriteString("if [ -f " + shellQuote(callCount) + " ]; then read -r count < " + shellQuote(callCount) + "; fi\n")
	program.WriteString("count=$((count + 1))\n")
	program.WriteString("printf '%s\\n' \"$count\" > " + shellQuote(callCount) + "\n")
	program.WriteString("case $count in\n")

	for i, page := range pages {
		fmt.Fprintf(&program, "%d)\n", i+1)

		if page.stdout != "" {
			program.WriteString("  printf '%s\\n' " + shellQuote(page.stdout) + "\n")
		}

		if page.stderr != "" {
			program.WriteString("  printf '%s\\n' " + shellQuote(page.stderr) + " >&2\n")
		}

		if page.status != 0 {
			fmt.Fprintf(&program, "  exit %d\n", page.status)
		}

		program.WriteString("  ;;\n")
	}

	program.WriteString("*)\n")
	fmt.Fprintf(&program, "  printf 'the fake aws was called %%s times, and the test recorded %d pages\\n' \"$count\" >&2\n",
		len(pages))
	program.WriteString("  exit 255\n")
	program.WriteString("  ;;\n")
	program.WriteString("esac\n")

	return []byte(program.String())
}

// fakeCalls returns the calls a run's fake programs recorded, one entry per call
// with the call's arguments as the separate words it was given.
func fakeCalls(t *testing.T, callLog string) [][]string {
	t.Helper()

	document, err := os.ReadFile(callLog)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		t.Fatalf("read the fake aws's call log %s: %v", callLog, err)
	}

	if len(document) == 0 {
		return nil
	}

	var calls [][]string

	for _, line := range strings.Split(strings.TrimSuffix(string(document), "\n"), "\n") {
		switch {
		case line == fakeCallSeparator:
			calls = append(calls, nil)
		case len(calls) == 0:
			t.Fatalf("%s starts with %q, not the separator %q the fake writes before a call",
				callLog, line, fakeCallSeparator)
		default:
			calls[len(calls)-1] = append(calls[len(calls)-1], line)
		}
	}

	return calls
}

// formatFakeCalls renders the calls for a failure message: one line per call,
// its arguments quoted one by one, so a check that compares argv words shows
// which word was where.
func formatFakeCalls(calls [][]string) string {
	if len(calls) == 0 {
		return "the fake program was never called"
	}

	var formatted strings.Builder

	for i, call := range calls {
		fmt.Fprintf(&formatted, "call %d: %q\n", i+1, call)
	}

	return formatted.String()
}

// passedNextToken returns the value a call passes after --next-token, and
// whether it passes one at all: the flag and its token are two separate
// arguments, which a shell that joined them would not be.
func passedNextToken(call []string) (string, bool) {
	at := slices.Index(call, "--next-token")
	if at < 0 || at+1 >= len(call) {
		return "", false
	}

	return call[at+1], true
}

// decodePages returns every JSON value path holds. It differs from
// decodeJSONValues, which the fixture checks use, in one way: an empty or
// missing file is a result here, not a failure, because a block that wrote no
// page at all is exactly what the checks above have to see.
func decodePages(t *testing.T, path string) []json.RawMessage {
	t.Helper()

	document, err := os.ReadFile(path)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		t.Fatalf("read %s: %v", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(document))

	var values []json.RawMessage

	for {
		var value json.RawMessage

		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("%s is not a file of JSON values: %v", path, err)
		}

		values = append(values, value)
	}

	return values
}

// writeScript writes a program the block can run.
func writeScript(t *testing.T, path string, program []byte) {
	t.Helper()

	if err := os.WriteFile(path, program, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // G302: a program the block runs must be executable
		t.Fatalf("make %s executable: %v", path, err)
	}
}

// shellQuote quotes value as one word of a POSIX shell, so a path or a page
// holding a space or a quote reaches a fake program as the single word it is.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// The files the guide's Cloud DNS commands write, the call the record-set loop
// runs, and the first line of the block that runs it: the test below extracts
// the block from the guide by these, never by a line number.
const (
	// cloudDNSManagedZonesFile is the file the managed-zones call writes: the
	// zone list the record-set loop reads, one public zone per line.
	cloudDNSManagedZonesFile = "dns-managed-zones.json"
	// cloudDNSRoutedFile is the file the record-set loop writes: one count per
	// public zone, and no record.
	cloudDNSRoutedFile = "dns-routed-record-sets.json"
	// cloudDNSRecordSetsCall is the call the loop runs once per public zone.
	cloudDNSRecordSetsCall = "gcloud dns record-sets list"
	// cloudDNSBlockStart is the first line of the block: the jq filter that
	// selects the public zones out of the managed-zones file.
	cloudDNSBlockStart = `jq -r '.[] | select(.visibility == "public")`
	// cloudDNSProject is the project the guide's calls run in, which the
	// synthetic zone list names as the guide's jq filter would have.
	cloudDNSProject = "example-project"
)

// The two zones the synthetic zone list holds, and the one count the fake
// gcloud answers the public one with: the types of two record sets that route by
// a policy, never a record's values.
const (
	// cloudDNSPublicZoneName is the public zone's name. It holds a `t`, the
	// character a line split on the wrong characters cuts a name at, and it is
	// what the record-set call has to be given.
	cloudDNSPublicZoneName = "test-zone"
	// cloudDNSPublicZoneID is the number Google gave that zone: the key the
	// guide writes the count under, so the program can join it to the asset row.
	cloudDNSPublicZoneID = "1234567890123456789"
	// cloudDNSPrivateZoneName is the private zone's name, which no call may
	// mention: only a public zone's record sets say whether it routes by a
	// policy.
	cloudDNSPrivateZoneName = "shop-internal"
	// cloudDNSRoutedTypes is what the fake gcloud prints for the public zone,
	// one record type per line.
	cloudDNSRoutedTypes = "A\nAAAA"
)

// cloudDNSManagedZones is the zone list the guide's managed-zones call would
// have left: one public zone and one private one, in the shape the guide's jq
// filter gives them, each with the project it adds.
const cloudDNSManagedZones = `[
  {"kind": "dns#managedZone", "project": "example-project", "id": "1234567890123456789", "name": "test-zone", "visibility": "public", "dnssecConfig": {"state": "off"}},
  {"kind": "dns#managedZone", "project": "example-project", "id": "9876543210987654321", "name": "shop-internal", "visibility": "private", "dnssecConfig": {"state": "off"}}
]`

// TestInventoryGuideCloudDNSRecordSetsBlock runs the guide's own Cloud DNS
// record-set block, read out of docs/guide/inventory.md, under the POSIX shells
// the guide promises: the block reads each public zone's line of the
// managed-zones file for the zone's number and its name, and asks gcloud how many
// of that zone's record sets route by a policy. Under dash, `IFS=$'\t'` is the
// three characters `$`, `\` and `t`: the line `1234567890123456789 test-zone`
// then reads as a number with the separator still in it and a name cut at its own
// `t`, so the call asks about a zone that does not exist, the call fails, and
// every public zone loses dns-routing with no word about it (review finding,
// round 2). The block runs against a fake gcloud the test writes into a temporary
// directory: no network, no real gcloud and no credentials (CLAUDE.md rules 1
// and 3).
func TestInventoryGuideCloudDNSRecordSetsBlock(t *testing.T) {
	t.Parallel()

	block := inventoryGuideCloudDNSBlock(t)

	// The same shells the list-collections block runs under: the guide promises
	// bash and zsh, and a visitor may have sh or dash instead (review finding,
	// round 3).
	for _, sh := range guidePOSIXShells {
		shellName := sh.name

		t.Run("runs under "+sh.name, func(t *testing.T) {
			t.Parallel()

			if _, err := exec.LookPath(sh.shell); err != nil {
				t.Skipf("%s is not installed, so this shell cannot be checked here: %v", sh.shell, err)
			}

			run := runGuideBlock(t, guideBlockOptions{
				shell:    sh.shell,
				preamble: sh.preamble,
				block:    block,
				fakes:    map[string]func(callLog string) []byte{"gcloud": fakeGCloudProgram},
				files:    map[string]string{cloudDNSManagedZonesFile: cloudDNSManagedZones},
				env:      []string{"project=" + cloudDNSProject},
			})

			if run.exitStatus != 0 {
				t.Errorf("the block exited %d under %s, want 0, so the guide's commands are not the POSIX shell's it says they are; stderr = %q",
					run.exitStatus, shellName, run.stderr)
			}

			if len(run.calls) != 1 {
				t.Fatalf("the block called gcloud %d times under %s, want 1: the zone list holds one public zone, which the call runs for, and one private zone, which it must not\n%s",
					len(run.calls), shellName, formatFakeCalls(run.calls))
			}

			call := run.calls[0]

			for _, want := range []struct {
				word string
				why  string
			}{
				{"--zone=" + cloudDNSPublicZoneName, "the public zone's name, read out of the zone list: a line split on characters it does not hold leaves the name empty or cut in two, and the call asks about a zone that does not exist"},
				{"--project=" + cloudDNSProject, "the project the guide runs its calls in"},
			} {
				if !slices.Contains(call, want.word) {
					t.Errorf("the call passes no %q under %s, want %s\n%s",
						want.word, shellName, want.why, formatFakeCalls(run.calls))
				}
			}

			for _, word := range call {
				if strings.Contains(word, cloudDNSPrivateZoneName) {
					t.Errorf("the call names the private zone %q under %s, want no private zone called: only a public zone's record sets say whether the zone routes by a policy, and the private zone's are no part of the inventory (PRD #581)\n%s",
						word, shellName, formatFakeCalls(run.calls))
				}
			}

			want := []routedRecordSet{{
				Project:          cloudDNSProject,
				ID:               cloudDNSPublicZoneID,
				RoutedRecordSets: 2,
			}}

			if diff := cmp.Diff(want, routedRecordSets(t, filepath.Join(run.outDir, cloudDNSRoutedFile))); diff != "" {
				t.Errorf("%s holds the wrong entries under %s (-want +got):\n%s",
					cloudDNSRoutedFile, shellName, diff)
			}
		})
	}
}

// inventoryGuideCloudDNSBlock returns the guide's block that counts each public
// zone's routed record sets, read from the markdown itself: the test runs the
// command the guide gives, never a copy that could drift from it.
func inventoryGuideCloudDNSBlock(t *testing.T) string {
	t.Helper()

	document, err := os.ReadFile(inventoryGuidePath)
	if err != nil {
		t.Fatalf("read %s: %v", inventoryGuidePath, err)
	}

	lines := strings.Split(string(document), "\n")

	start := -1

	for i, line := range lines {
		if strings.HasPrefix(line, cloudDNSBlockStart) {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s no longer holds the block that counts each public zone's routed record sets: no line starts with %s",
			inventoryGuidePath, cloudDNSBlockStart)
	}

	end := -1

	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		if strings.HasPrefix(trimmed, "done") && strings.Contains(trimmed, cloudDNSRoutedFile) {
			end = i

			break
		}
	}

	if end < 0 {
		t.Fatalf("%s: the Cloud DNS block that starts at line %d has no line that ends its loop and writes %s, so the loop it documents cannot be read",
			inventoryGuidePath, start+1, cloudDNSRoutedFile)
	}

	block := strings.Join(lines[start:end+1], "\n") + "\n"

	// Guard the extraction itself: a block that lost the call, the zone list or
	// the file it writes would make every check below pass over something that is
	// not the command the guide documents.
	for _, want := range []string{
		cloudDNSRecordSetsCall,
		cloudDNSManagedZonesFile,
		cloudDNSRoutedFile,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("%s lines %d-%d hold no %q, so they are not the Cloud DNS block this test runs:\n%s",
				inventoryGuidePath, start+1, end+1, want, block)
		}
	}

	return block
}

// fakeGCloudProgram returns a shell program that stands where gcloud does: it
// records the arguments of every call, one call after another, and answers the
// one zone the test's zone list holds with cloudDNSRoutedTypes. Any other zone
// fails as a real call for a zone that does not exist does, so a loop that reads
// a zone's name wrong is visible rather than silently answered.
func fakeGCloudProgram(callLog string) []byte {
	var program strings.Builder

	program.WriteString("#!/bin/sh\n")
	program.WriteString("# A fake gcloud program, written by the e2e test: it records each call's\n")
	program.WriteString("# arguments and answers the one zone the test's zone list holds.\n")
	program.WriteString("{\n")
	program.WriteString("  printf '%s\\n' " + shellQuote(fakeCallSeparator) + "\n")
	program.WriteString("  for argument in \"$@\"; do printf '%s\\n' \"$argument\"; done\n")
	program.WriteString("} >> " + shellQuote(callLog) + "\n")
	program.WriteString("zone=\n")
	program.WriteString("for argument in \"$@\"; do\n")
	program.WriteString("  case $argument in --zone=*) zone=${argument#--zone=} ;; esac\n")
	program.WriteString("done\n")
	program.WriteString("case $zone in\n")
	program.WriteString(shellQuote(cloudDNSPublicZoneName) + ")\n")
	program.WriteString("  printf '%s\\n' " + shellQuote(cloudDNSRoutedTypes) + "\n")
	program.WriteString("  ;;\n")
	program.WriteString("*)\n")
	fmt.Fprintf(&program, "  printf 'the fake gcloud knows no zone %%s: the test records only %s\\n' \"$zone\" >&2\n",
		cloudDNSPublicZoneName)
	program.WriteString("  exit 1\n")
	program.WriteString("  ;;\n")
	program.WriteString("esac\n")

	return []byte(program.String())
}

// routedRecordSet is one entry of the file the record-set loop writes: the
// project the zone is in, the zone's number the program joins the count to the
// asset row on, and how many of the zone's record sets route by a policy. A
// record's own values are no part of it.
type routedRecordSet struct {
	Project          string `json:"project"`
	ID               string `json:"id"`
	RoutedRecordSets int    `json:"routedRecordSets"`
}

// routedRecordSets returns the entries of the file the record-set loop wrote,
// failing on any entry that carries more than the three fields the guide's loop
// writes: a record's values in that file are exactly what the milestone keeps
// out of it. A missing file is a failure too, because the loop writes it with
// `jq -s .` whatever the zones answered.
func routedRecordSets(t *testing.T, path string) []routedRecordSet {
	t.Helper()

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file the record-set loop wrote, %s: %v", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()

	var entries []routedRecordSet

	if err := decoder.Decode(&entries); err != nil {
		t.Fatalf("%s is not the list of {project, id, routedRecordSets} the guide's loop writes: %v; document = %q",
			path, err, document)
	}

	return entries
}

// The file the guide's Azure DNSSEC loop writes, the call it runs once per
// public zone, and the resource group the synthetic Resource Graph page records:
// the test below extracts the loop from the guide by the file it writes, never by
// a line number.
const (
	// azureDNSSECFile is the file the DNSSEC loop writes: one configuration per
	// public zone that has one, and nothing for a zone Azure does not sign.
	azureDNSSECFile = "dns-dnssec.json"
	// azureDNSSECCall is the call the loop runs once per public zone.
	azureDNSSECCall = "az network dns dnssec-config show"
	// azureDNSSECResourceGroup is the resource group the page records for every
	// zone: the DNSSEC call takes the zone by its name and its group.
	azureDNSSECResourceGroup = "rg-shop"
	// azureDNSSECSignedZone is the public zone Azure signs. Its DNSSEC
	// configuration's provisioningState is Succeeded.
	azureDNSSECSignedZone = "shop.example.com"
	// azureDNSSECUnsignedZone is a public zone with no DNSSEC configuration: the
	// call answers (NotFound) for it, which the guide says is expected.
	azureDNSSECUnsignedZone = "blog.example.com"
	// azureDNSSECLegacyZone is a public zone whose zoneType the page does not
	// record: Public is the API's default, so the loop asks about it too.
	azureDNSSECLegacyZone = "legacy.example.com"
	// azureDNSSECPrivateLegacyZone is a legacy private zone of the dnszones type:
	// only public zones are signed, so no call may name it.
	azureDNSSECPrivateLegacyZone = "old-internal.example.com"
	// azureDNSSECPrivateZone is a privateDnsZones zone: a zone of another type,
	// which serves nothing on the internet and is never signed.
	azureDNSSECPrivateZone = "internal.example.com"
	// azureDNSSECSignedConfigID is the DNSSEC configuration the fake az prints for
	// the signed zone: the configuration's own ID, which the program joins to the
	// zone by its case-insensitive resource ID, not by its casing.
	azureDNSSECSignedConfigID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnszones/shop.example.com/dnssecConfigs/default"
	// azureDNSSECDashResourceGroup is a resource group whose name starts with a
	// `-`, which Azure allows: a call that passes the name as a word of its own
	// has the CLI read `-rg-legacy` as an option instead of the flag's value, fail,
	// and the loop's `|| :` hides the failure (audit finding on PRD #581's
	// milestone 4).
	azureDNSSECDashResourceGroup = "-rg-legacy"
	// azureDNSSECDashZone is the public zone Azure signs in that group.
	azureDNSSECDashZone = "dash.example.com"
)

// azureDNSSECSignedConfig is the configuration the fake az prints for the signed
// zone of the page the guide's other Azure blocks use.
var azureDNSSECSignedConfig = dnssecConfiguration{
	ID:                azureDNSSECSignedConfigID,
	Type:              "Microsoft.Network/dnszones/dnssecConfigs",
	ProvisioningState: "Succeeded",
}

// azureDNSSECDashConfig is the configuration the fake az prints for the signed
// zone of the page whose group name starts with a `-`: an ID the fake builds out
// of the group and the zone it received, so a call that did not deliver the group
// as the flag's value writes no such configuration at all.
var azureDNSSECDashConfig = dnssecConfiguration{
	ID:                "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/" + azureDNSSECDashResourceGroup + "/providers/Microsoft.Network/dnszones/" + azureDNSSECDashZone + "/dnssecConfigs/default",
	Type:              "Microsoft.Network/dnszones/dnssecConfigs",
	ProvisioningState: "Succeeded",
}

// azureDNSSECGraphPage is the Resource Graph page the loop reads: the public
// zones, one of them signed, and the private zones no call may ask about. The
// unsigned public zone comes first and the signed one last, so a loop that stops
// its pipeline at the first (NotFound) answer is caught by the calls it did not
// make rather than by the shell's exit status.
const azureDNSSECGraphPage = `{
  "count": 5,
  "data": [
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/blog.example.com", "name": "blog.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Public"}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/old-internal.example.com", "name": "old-internal.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Private"}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/privateDnsZones/internal.example.com", "name": "internal.example.com", "type": "microsoft.network/privatednszones", "kind": "", "location": "global", "zones": null, "properties": null},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/legacy.example.com", "name": "legacy.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/shop.example.com", "name": "shop.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Public"}}
  ],
  "skip_token": null,
  "total_records": 5
}`

// azureDNSSECGraphPageUnsignedLast is the same set of zones with the signed one
// first and an unsigned one last. A shell exits with the status of its loop's
// last command, so this page is the one that catches a loop whose call is not
// ended with `|| :`: the last zone's (NotFound) answer would become the shell's
// exit status, and the guide's own run would stop there even though no zone is
// wrong.
const azureDNSSECGraphPageUnsignedLast = `{
  "count": 5,
  "data": [
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/shop.example.com", "name": "shop.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Public"}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/blog.example.com", "name": "blog.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Public"}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/old-internal.example.com", "name": "old-internal.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Private"}},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/privateDnsZones/internal.example.com", "name": "internal.example.com", "type": "microsoft.network/privatednszones", "kind": "", "location": "global", "zones": null, "properties": null},
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-shop/providers/Microsoft.Network/dnsZones/legacy.example.com", "name": "legacy.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {}}
  ],
  "skip_token": null,
  "total_records": 5
}`

// azureDNSSECGraphPageDashGroup is a page of its own, local to this test, whose
// one public signed zone sits in a resource group whose name starts with a `-`:
// a group name may start with a dash, and a call that passes it as a separate
// word has the CLI read it as an option, fail, and the loop's `|| :` hide the
// failure, so the signed zone loses dnssec (audit finding on PRD #581's milestone
// 4). The page the other Azure blocks use stays unchanged, so no fixture, golden
// or other block sees this group.
const azureDNSSECGraphPageDashGroup = `{
  "count": 1,
  "data": [
    {"id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/-rg-legacy/providers/Microsoft.Network/dnsZones/dash.example.com", "name": "dash.example.com", "type": "microsoft.network/dnszones", "kind": "", "location": "global", "zones": null, "properties": {"zoneType": "Public"}}
  ],
  "skip_token": null,
  "total_records": 1
}`

// azureDNSSECZone is one public zone the loop has to ask az about: its name and
// the resource group the page records it in, both as the page holds them.
type azureDNSSECZone struct {
	zone  string
	group string
}

// azureDNSSECScenario is one Resource Graph page the guide's DNSSEC loop reads,
// the public zones it has to ask az about, in the page's order, and the
// configurations its file has to end up holding.
type azureDNSSECScenario struct {
	// name is how the subtest names the case.
	name string
	// page is the Resource Graph page the run writes into its $out.
	page string
	// wantZones are the page's public zones with their groups, in its order, and
	// nothing else.
	wantZones []azureDNSSECZone
	// wantConfigs are the configurations the loop's file has to hold: one per
	// signed zone, and none for an unsigned one.
	wantConfigs []dnssecConfiguration
}

// azureDNSSECScenarios are the pages the DNSSEC loop is run against: the one the
// test's other blocks use, whose signed zone comes last, one whose last public
// zone is unsigned, and one whose signed zone sits in a resource group whose name
// starts with a `-`. The order is what a shell exits with, so the second case is
// the one that catches a loop whose call is not ended with `|| :` (PRD #581,
// milestone 4).
var azureDNSSECScenarios = []azureDNSSECScenario{
	{
		name: "the last public zone is signed",
		page: azureDNSSECGraphPage,
		wantZones: []azureDNSSECZone{
			{zone: azureDNSSECUnsignedZone, group: azureDNSSECResourceGroup},
			{zone: azureDNSSECLegacyZone, group: azureDNSSECResourceGroup},
			{zone: azureDNSSECSignedZone, group: azureDNSSECResourceGroup},
		},
		wantConfigs: []dnssecConfiguration{azureDNSSECSignedConfig},
	},
	{
		name: "the last public zone is unsigned",
		page: azureDNSSECGraphPageUnsignedLast,
		wantZones: []azureDNSSECZone{
			{zone: azureDNSSECSignedZone, group: azureDNSSECResourceGroup},
			{zone: azureDNSSECUnsignedZone, group: azureDNSSECResourceGroup},
			{zone: azureDNSSECLegacyZone, group: azureDNSSECResourceGroup},
		},
		wantConfigs: []dnssecConfiguration{azureDNSSECSignedConfig},
	},
	{
		name: "the signed zone's resource group starts with a dash",
		page: azureDNSSECGraphPageDashGroup,
		wantZones: []azureDNSSECZone{
			{zone: azureDNSSECDashZone, group: azureDNSSECDashResourceGroup},
		},
		wantConfigs: []dnssecConfiguration{azureDNSSECDashConfig},
	},
}

// azureDNSSECDocument is what the fake az prints for the signed zone: the
// `--query` projection the guide asks for, its signed state alone.
const azureDNSSECDocument = `{"id": "` + azureDNSSECSignedConfigID + `", "type": "Microsoft.Network/dnszones/dnssecConfigs", "provisioningState": "Succeeded"}`

// TestInventoryGuideAzureDNSSECBlock runs the guide's own Azure DNSSEC loop, read
// out of docs/guide/inventory.md, under the POSIX shells the guide promises: the
// loop reads each public zone's resource group and name out of the Resource
// Graph pages the query wrote and asks `az network dns dnssec-config show` for
// that zone's configuration, keeping its provisioning state alone. An unsigned
// zone answers (NotFound) on stderr and fails, which the loop has to survive: a
// public zone without a configuration is not an error (PRD #581, milestone 4).
// The loop runs against a fake az the test writes into a temporary directory: no
// network, no real az and no credentials (CLAUDE.md rules 1 and 3).
func TestInventoryGuideAzureDNSSECBlock(t *testing.T) {
	t.Parallel()

	block := inventoryGuideAzureDNSSECBlock(t)

	// The loop reads only public zones: a private zone is never signed, so a command
	// that asked about one would fail on every run.
	for _, private := range []string{azureDNSSECPrivateLegacyZone, azureDNSSECPrivateZone} {
		if strings.Contains(commandText(block), private) {
			t.Errorf("%s names the private zone %q, want the loop to select public zones by their type and their zone type: only a public zone is signed, so a call for a private one fails on every run (PRD #581, milestone 4)\n%s",
				inventoryGuidePath, private, block)
		}
	}

	// The same shells the other blocks of the guide run under (review finding N-a).
	for _, sh := range guidePOSIXShells {
		shellName := sh.name

		t.Run("runs under "+sh.name, func(t *testing.T) {
			t.Parallel()

			if _, err := exec.LookPath(sh.shell); err != nil {
				t.Skipf("%s is not installed, so this shell cannot be checked here: %v", sh.shell, err)
			}

			// One page per run, because the order of the page's zones is what the
			// shell exits with: see azureDNSSECScenarios.
			for _, scenario := range azureDNSSECScenarios {
				t.Run(scenario.name, func(t *testing.T) {
					t.Parallel()

					run := runGuideBlock(t, guideBlockOptions{
						shell:    sh.shell,
						preamble: sh.preamble,
						block:    block,
						fakes:    map[string]func(callLog string) []byte{"az": fakeAZDNSSECProgram},
						files:    map[string]string{"graph-1.json": scenario.page},
					})

					assertAzureDNSSECRun(t, shellName, run, scenario)
				})
			}
		})
	}
}

// assertAzureDNSSECRun checks one run of the guide's DNSSEC loop: it exited 0
// whatever its last zone answered, it asked az about each of the page's public
// zones in the page's order and about no private one, each call kept only the
// three fields the program reads, and the file holds the signed zones'
// configurations alone. The group each call passes is the one the page records
// for the zone: a group whose name starts with a `-` reaches the fake az only as
// the flag's value, and the configuration the fake builds out of it is then in
// the file, so a call that passed it as a word of its own leaves the signed zone
// without dnssec.
func assertAzureDNSSECRun(t *testing.T, shellName string, run guideBlockRun, scenario azureDNSSECScenario) {
	t.Helper()

	wantZones := scenario.wantZones

	if run.exitStatus != 0 {
		t.Errorf("the block exited %d under %s, want 0: an unsigned public zone answers (NotFound), which the guide says is expected, so the loop goes on to the zones after it, and the shell exits with the status of the loop's last command, whatever that zone answered; stderr = %q",
			run.exitStatus, shellName, run.stderr)
	}

	// One call per public zone, in the page's order, and none for a private one:
	// the public zones of the page, which each get one call.
	if len(run.calls) != len(wantZones) {
		t.Fatalf("the block called az %d times under %s, want %d: the loop asks about each public zone of the page once, and about no private one\n%s",
			len(run.calls), shellName, len(wantZones), formatFakeCalls(run.calls))
	}

	for i, wantZone := range wantZones {
		call := run.calls[i]

		for _, want := range []struct {
			name  string
			value string
			why   string
		}{
			{"--zone-name", wantZone.zone, "the public zone of the page, read out of it: a loop that read the line wrong asks about a zone that does not exist, and the zone loses its signed state"},
			{"--resource-group", wantZone.group, "the zone's resource group, which the DNSSEC call needs beside the zone's name: a group name may start with a `-`, which the CLI reads as an option when the flag and its value are two words, so the call fails and the loop's `|| :` hides it (audit finding, PRD #581, milestone 4)"},
			{"--output", "json", "the JSON output the program reads the provisioning state from"},
		} {
			if got, ok := passedFlag(call, want.name); !ok || got != want.value {
				t.Errorf("call %d passes no %s %q under %s (got %q, passed = %t), want %s\n%s",
					i+1, want.name, want.value, shellName, got, ok, want.why, formatFakeCalls(run.calls))
			}
		}

		for _, word := range call {
			for _, private := range []string{azureDNSSECPrivateLegacyZone, azureDNSSECPrivateZone} {
				if strings.Contains(word, private) {
					t.Errorf("call %d names the private zone %q under %s, want no private zone called: only a public zone is signed (PRD #581, milestone 4)\n%s",
						i+1, private, shellName, formatFakeCalls(run.calls))
				}
			}
		}

		// The projection is what keeps a zone's keys out of the file: az
		// filters on the client, after it read the whole configuration.
		query, ok := passedFlag(call, "--query")
		if !ok {
			t.Fatalf("call %d passes no --query under %s, want one keeping the configuration's ID, its type and its provisioning state: without it the call writes the zone's signing keys to the file (PRD #581, milestone 4)\n%s",
				i+1, shellName, formatFakeCalls(run.calls))
		}

		for _, field := range []string{"id", "type", "provisioningState"} {
			if !strings.Contains(query, field) {
				t.Errorf("call %d's --query keeps no %q under %s, want it: the program joins the configuration to its zone by the ID, and reads the provisioning state to decide dnssec\n%s",
					i+1, field, shellName, formatFakeCalls(run.calls))
			}
		}

		for _, field := range []string{"signingKeys", "publicKey", "delegationSignerInfo", "nameServers"} {
			if strings.Contains(query, field) {
				t.Errorf("call %d's --query keeps %q under %s, want no field of a zone's keys or its name servers: the configuration's signed state is all the program reads, and the keys are the zone's signing material (PRD #581)\n%s",
					i+1, field, shellName, formatFakeCalls(run.calls))
			}
		}
	}

	want := scenario.wantConfigs

	if diff := cmp.Diff(want, dnssecConfigurations(t, filepath.Join(run.outDir, azureDNSSECFile))); diff != "" {
		t.Errorf("%s holds the wrong configurations under %s (-want +got):\n%s\nthe file holds one document per signed zone and nothing for an unsigned one, and no field beyond the three the --query keeps (PRD #581, milestone 4); a signed zone missing from it means its call failed, which `|| :` then hid: stderr = %q",
			azureDNSSECFile, shellName, diff, run.stderr)
	}
}

// inventoryGuideAzureDNSSECBlock returns the guide's block that reads each public
// Azure DNS zone's DNSSEC configuration, read from the markdown itself: the test
// runs the command the guide gives, never a copy that could drift from it. The
// block starts at the jq filter that selects the public zones out of the Resource
// Graph pages and ends at the `done` line that writes the file.
func inventoryGuideAzureDNSSECBlock(t *testing.T) string {
	t.Helper()

	document, err := os.ReadFile(inventoryGuidePath)
	if err != nil {
		t.Fatalf("read %s: %v", inventoryGuidePath, err)
	}

	lines := strings.Split(string(document), "\n")

	end := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "done") && strings.Contains(trimmed, azureDNSSECFile) {
			end = i

			break
		}
	}

	if end < 0 {
		t.Fatalf("%s holds no line that ends the DNSSEC loop and writes %s, so the loop it documents cannot be read: PRD #581's milestone 4 asks for a per-zone loop over the public zones the Resource Graph query wrote",
			inventoryGuidePath, azureDNSSECFile)
	}

	start := -1

	for i := end; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "jq ") {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s lines 1-%d hold no jq filter that opens the block ending with %s, so the loop reads no zone list\n%s",
			inventoryGuidePath, end+1, azureDNSSECFile, strings.Join(lines[:end+1], "\n"))
	}

	block := strings.Join(lines[start:end+1], "\n") + "\n"

	// Guard the extraction itself: a block that lost the call, the zone list or the
	// file it writes would make every check below pass over something that is not
	// the command the guide documents.
	for _, want := range []string{
		azureDNSSECCall,
		azureDNSSECFile,
		"graph-",
		"zoneType",
		"while",
		"read -r",
		`"$rg"`,
		`"$zone"`,
		"--resource-group",
		"--zone-name",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("%s lines %d-%d hold no %q, so they are not the Azure DNSSEC block this test runs: the loop reads the public zones of the Resource Graph pages, takes each zone's group and name quoted, and writes %s\n%s",
				inventoryGuidePath, start+1, end+1, want, azureDNSSECFile, block)
		}
	}

	return block
}

// commandText returns a command without its comment lines: a comment may name a
// private zone in prose without a call asking about it.
func commandText(command string) string {
	var code []string

	for _, line := range strings.Split(command, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		code = append(code, line)
	}

	return strings.Join(code, "\n")
}

// passedFlag returns the value a call passes for a flag, written either as the
// flag and its value in two words or as one `--flag=value` word, and whether it
// passes the flag at all.
func passedFlag(call []string, name string) (string, bool) {
	for i, word := range call {
		if word == name && i+1 < len(call) {
			return call[i+1], true
		}

		if value, ok := strings.CutPrefix(word, name+"="); ok {
			return value, true
		}
	}

	return "", false
}

// fakeAZDNSSECProgram returns a shell program that stands where az does for the
// DNSSEC call: it records the arguments of every call, parses them the way the
// real CLI does, and answers the public zones of the test's Resource Graph pages.
// A value that starts with `-` is an option to the real az even when the word
// before it is a flag that takes one, so this fake reads it as an option too, and
// the call fails: only the attached form `--resource-group="$rg"` delivers a
// group whose name starts with a dash (audit finding, PRD #581, milestone 4).
// The signed zone gets its configuration, built out of the group and the zone it
// received; the unsigned ones get the (NotFound) error a real call gives for a
// zone without one, on stderr and with a non-zero status. Any other zone fails
// with the test's own message, so a loop that reads a zone wrong is visible rather
// than silently answered.
func fakeAZDNSSECProgram(callLog string) []byte {
	var program strings.Builder

	program.WriteString("#!/bin/sh\n")
	program.WriteString("# A fake az program, written by the e2e test: it records each call's\n")
	program.WriteString("# arguments and answers the public zones the test's page holds.\n")
	program.WriteString("{\n")
	program.WriteString("  printf '%s\\n' " + shellQuote(fakeCallSeparator) + "\n")
	program.WriteString("  for argument in \"$@\"; do printf '%s\\n' \"$argument\"; done\n")
	program.WriteString("} >> " + shellQuote(callLog) + "\n")
	program.WriteString("rg=\n")
	program.WriteString("zone=\n")
	program.WriteString("previous=\n")
	program.WriteString("for argument in \"$@\"; do\n")
	// The real az reads a word that starts with a `-` as an option, whatever the
	// flag before it: a resource group whose name starts with a dash only reaches
	// it attached to `--resource-group=`, so a separate-word call fails the way az
	// does and the loop's `|| :` hides it (audit finding, PRD #581, milestone 4).
	program.WriteString("  case $previous in\n")
	program.WriteString("    --resource-group|--zone-name)\n")
	program.WriteString("      case $argument in\n")
	program.WriteString("        -*)\n")
	program.WriteString("          printf 'the real az reads %s as an option, not as the value of %s: the value has to be attached to the flag\\n' \"$argument\" \"$previous\" >&2\n")
	program.WriteString("          exit 2\n")
	program.WriteString("          ;;\n")
	program.WriteString("      esac\n")
	program.WriteString("      ;;\n")
	program.WriteString("  esac\n")
	program.WriteString("  case $previous in\n")
	program.WriteString("    --resource-group) rg=$argument ;;\n")
	program.WriteString("    --zone-name) zone=$argument ;;\n")
	program.WriteString("  esac\n")
	program.WriteString("  case $argument in\n")
	program.WriteString("    --resource-group=*) rg=${argument#--resource-group=} ;;\n")
	program.WriteString("    --zone-name=*) zone=${argument#--zone-name=} ;;\n")
	program.WriteString("  esac\n")
	program.WriteString("  previous=$argument\n")
	program.WriteString("done\n")
	program.WriteString("case $zone in\n")
	program.WriteString(shellQuote(azureDNSSECSignedZone) + ")\n")
	program.WriteString("  printf '%s\\n' " + shellQuote(azureDNSSECDocument) + "\n")
	program.WriteString("  ;;\n")
	// The signed zone in the group whose name starts with a dash answers with an
	// ID built from the group and the zone it received: a call that never
	// delivered the group writes no configuration for the zone at all.
	program.WriteString(shellQuote(azureDNSSECDashZone) + ")\n")
	program.WriteString("  printf '{\"id\": \"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/%s/providers/Microsoft.Network/dnszones/%s/dnssecConfigs/default\", \"type\": \"Microsoft.Network/dnszones/dnssecConfigs\", \"provisioningState\": \"Succeeded\"}\\n' \"$rg\" \"$zone\"\n")
	program.WriteString("  ;;\n")
	program.WriteString(shellQuote(azureDNSSECUnsignedZone) + "|" + shellQuote(azureDNSSECLegacyZone) + ")\n")
	program.WriteString("  printf \"(NotFound) DNSSEC is not enabled for DNS zone '%s'\\n\" \"$zone\" >&2\n")
	program.WriteString("  exit 1\n")
	program.WriteString("  ;;\n")
	program.WriteString("*)\n")
	fmt.Fprintf(&program, "  printf 'the fake az knows no zone %%s in group %%s: the test records only the public zones of %s\\n' \"$zone\" \"$rg\" >&2\n",
		azureDNSSECResourceGroup)
	program.WriteString("  exit 1\n")
	program.WriteString("  ;;\n")
	program.WriteString("esac\n")

	return []byte(program.String())
}

// dnssecConfiguration is one document the DNSSEC loop wrote: the `--query`
// projection the guide asks for, and nothing else. A field beyond the three is
// exactly what the milestone keeps out of the file.
type dnssecConfiguration struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	ProvisioningState string `json:"provisioningState"`
}

// dnssecConfigurations returns the documents the DNSSEC loop wrote, failing on any
// that carries a field beyond the three the guide's `--query` keeps: a zone's
// signing keys in that file are what the milestone keeps out of it. An empty file
// is a result, as a run with no signed zone writes one.
func dnssecConfigurations(t *testing.T, path string) []dnssecConfiguration {
	t.Helper()

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file the DNSSEC loop wrote, %s: %v", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()

	var entries []dnssecConfiguration

	for {
		var entry dnssecConfiguration

		err := decoder.Decode(&entry)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("%s is not the list of {id, type, provisioningState} the guide's DNSSEC loop writes: %v; document = %q",
				path, err, document)
		}

		entries = append(entries, entry)
	}

	return entries
}
