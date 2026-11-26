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

// fakeAWSCallSeparator is the line the fake aws program writes before each
// call's arguments, so a call's words stay apart from the next call's.
const fakeAWSCallSeparator = "--call--"

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
				len(run.calls), formatFakeAWSCalls(run.calls))
		}

		if slices.Contains(run.calls[0], "--next-token") {
			t.Errorf("the first call passes a continuation token, want none: the loop starts at the first page\n%s",
				formatFakeAWSCalls(run.calls))
		}

		token, ok := passedNextToken(run.calls[1])
		switch {
		case !ok:
			t.Errorf("the second call passes no --next-token argument, want --next-token and then t2, as two words, or the call returns the first page again\n%s",
				formatFakeAWSCalls(run.calls))
		case token != "t2":
			t.Errorf("the second call passes --next-token %q, want the token the first page named, t2\n%s",
				token, formatFakeAWSCalls(run.calls))
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
				len(run.calls), formatFakeAWSCalls(run.calls))
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
				len(run.calls), formatFakeAWSCalls(run.calls))
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
				len(run.calls), formatFakeAWSCalls(run.calls))
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
	// finding N-a). So the block has to run in each of these, whatever words it
	// pages with.
	shells := []struct {
		name     string
		shell    string
		preamble string
	}{
		{name: "bash", shell: "bash"},
		{name: "zsh", shell: "zsh"},
		{name: "sh", shell: "sh"},
		{name: "dash", shell: "dash"},
		{name: "bash with set -u", shell: "bash", preamble: "set -u\n"},
	}

	for _, sh := range shells {
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
					len(run.calls), sh.name, formatFakeAWSCalls(run.calls))
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

	shell, err := exec.LookPath(shellName)
	if err != nil {
		t.Fatalf("%s is not installed, so the guide's block cannot be run with it: %v", shellName, err)
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

	callLog := filepath.Join(dir, "calls.log")
	callCount := filepath.Join(dir, "calls.count")

	writeScript(t, filepath.Join(fakeDir, "aws"), fakeAWSProgram(callLog, callCount, pages))

	script := filepath.Join(dir, "list-collections.sh")
	if err := os.WriteFile(script, []byte(preamble+block), 0o600); err != nil {
		t.Fatalf("write %s: %v", script, err)
	}

	// The block calls jq and aws by name, so the PATH it runs with names their
	// directories and nothing of the test process's environment goes with it:
	// there are no credentials in it to hand a visitor's command (CLAUDE.md
	// rule 3), and the fake aws stands where a real one would. The deadline is
	// the run's, not the test's: a block that asks the same page for ever is
	// stopped here and reported below.
	ctx, cancel := context.WithTimeout(t.Context(), blockRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, script)
	cmd.Env = []string{
		"PATH=" + fakeDir + ":" + filepath.Dir(jq) + ":" + filepath.Dir(shell),
		"out=" + outDir,
	}

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	exitStatus := 0

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("the block was still running after %s, so it never reaches its last page: %v; stderr = %q",
				blockRunTimeout, ctx.Err(), stderr.String())
		}

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s %s: %v", shell, script, err)
		}

		exitStatus = exitErr.ExitCode()
	}

	return listCollectionsRun{
		calls:       fakeAWSCalls(t, callLog),
		stderr:      stderr.String(),
		exitStatus:  exitStatus,
		collections: decodePages(t, filepath.Join(outDir, listCollectionsFile)),
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
	program.WriteString("  printf '%s\\n' " + shellQuote(fakeAWSCallSeparator) + "\n")
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

// fakeAWSCalls returns the calls the fake aws recorded, one entry per call with
// the call's arguments as the separate words it was given.
func fakeAWSCalls(t *testing.T, callLog string) [][]string {
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
		case line == fakeAWSCallSeparator:
			calls = append(calls, nil)
		case len(calls) == 0:
			t.Fatalf("%s starts with %q, not the separator %q the fake aws writes before a call",
				callLog, line, fakeAWSCallSeparator)
		default:
			calls[len(calls)-1] = append(calls[len(calls)-1], line)
		}
	}

	return calls
}

// formatFakeAWSCalls renders the calls for a failure message: one line per
// call, its arguments quoted one by one, so a check that compares argv words
// shows which word was where.
func formatFakeAWSCalls(calls [][]string) string {
	if len(calls) == 0 {
		return "the fake aws was never called"
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
// holding a space or a quote reaches the fake aws as the single word it is.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
