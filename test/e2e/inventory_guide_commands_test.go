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
