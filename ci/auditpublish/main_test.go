package main

// The fake gh group N drives ci/auditpublish's run against (PRD #175 milestone
// 2, ADR 0040, "The publish program's GitHub side"). It is this test binary,
// started through a symbolic link named gh, as the GitHub side's fake is: the
// library gives gh an environment of its own, so no variable could mark it, and
// the name it was started under is what TestMain reads. The same binary stands
// in for gitleaks, through a link of that name: a scanner that finds nothing.
//
// It records every call's arguments and answers it from its answers, the shape
// `gh api --include` writes: the status line, the headers, a blank line and the
// body, exit 0 for a 2xx status and 1 otherwise, with a marker on stderr that
// the program must never print. An answer names the method and endpoint it
// answers, or answers every call when they are empty. It records no environment:
// the one gh is given belongs to the GitHub side's tests, and this package must
// not read the process's environment at all (forbidigo group
// audit-publish-main-env).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// fakeGHName and fakeGitleaksName are the links' names, which TestMain
	// reads.
	fakeGHName       = "gh"
	fakeGitleaksName = "gitleaks"

	// The fake's files, next to its link.
	ghAnswersFile = "gh.answers.json"
	ghCallsFile   = "gh.calls.jsonl"

	// fakeOutputMarker is what the fake writes to its stderr beside gh's own
	// error line, so a test can show that none of it reaches the program's
	// output.
	fakeOutputMarker = "FAKE-TOOL-OUTPUT"
)

// TestMain makes the test binary stand in for gh and gitleaks: both fakes are
// symbolic links to this binary (newFakeGH, newFakeGitleaks), so the name it
// was started under says which one to be.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case fakeGHName:
		os.Exit(fakeGHMain())
	case fakeGitleaksName:
		// The fake gitleaks finds nothing in any text: it exits 0.
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// fakeAnswer is one answer the fake gh gives: the calls its method and
// endpoint name, or every call when they are empty.
type fakeAnswer struct {
	// Status is the HTTP status of the answer's status line.
	Status int `json:"status"`
	// Body is the answer's body, marshalled to JSON.
	Body any `json:"body"`
	// Method and Endpoint answer only the calls they match; empty answers
	// every call.
	Method   string `json:"method,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// matches reports whether the answer answers a call.
func (a fakeAnswer) matches(method, endpoint string) bool {
	return (a.Method == "" || a.Method == method) && (a.Endpoint == "" || a.Endpoint == endpoint)
}

// fakeCall is one invocation of the fake gh as it recorded it.
type fakeCall struct {
	// Args are gh's arguments, without argv[0].
	Args []string `json:"args"`
}

// fakeGH is a fake gh in a directory of the test's own.
type fakeGH struct {
	// Path is the fake's absolute path, handed to run as -gh.
	Path string

	dir string
}

// newFakeGH makes a fake gh that answers every call with answer.
func newFakeGH(t *testing.T, answer fakeAnswer) *fakeGH {
	t.Helper()

	return newRoutedFakeGH(t, answer)
}

// newRoutedFakeGH makes a fake gh that answers each call from the first of
// answers that matches it, and 404 when none does.
func newRoutedFakeGH(t *testing.T, answers ...fakeAnswer) *fakeGH {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	if err := os.Symlink(exe, filepath.Join(dir, fakeGHName)); err != nil {
		t.Fatalf("link the fake gh: %v", err)
	}

	body, err := json.Marshal(answers)
	if err != nil {
		t.Fatalf("marshal the fake gh's answers: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ghAnswersFile), body, 0o600); err != nil {
		t.Fatalf("write the fake gh's answers: %v", err)
	}

	return &fakeGH{Path: filepath.Join(dir, fakeGHName), dir: dir}
}

// newFakeGitleaks makes a fake gitleaks, a link to this binary that finds
// nothing in any text, and returns its path.
func newFakeGitleaks(t *testing.T) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	path := filepath.Join(dir, fakeGitleaksName)
	if err := os.Symlink(exe, path); err != nil {
		t.Fatalf("link the fake gitleaks: %v", err)
	}

	return path
}

// Calls returns every call the fake recorded, in order.
func (f *fakeGH) Calls(t *testing.T) []fakeCall {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(f.dir, ghCallsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		t.Fatalf("read the fake gh's record: %v", err)
	}

	var out []fakeCall

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)

	for scanner.Scan() {
		var c fakeCall
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			t.Fatalf("the fake gh's record holds a line that is no call: %v", err)
		}

		out = append(out, c)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("read the fake gh's record: %v", err)
	}

	return out
}

// fakeGHMain is the fake gh: it records its arguments and answers them from
// its answers, the way gh api --include does.
func fakeGHMain() int {
	dir := filepath.Dir(os.Args[0])

	record := fakeCall{Args: append([]string{}, os.Args[1:]...)}

	if err := appendFakeCall(filepath.Join(dir, ghCallsFile), record); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake gh:", err)

		return 1
	}

	answer, ok, err := nextAnswer(dir, record.Args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake gh:", err)

		return 1
	}

	if !ok {
		answer = fakeAnswer{Status: 404, Body: map[string]string{"message": "no route"}}
	}

	body, err := json.Marshal(answer.Body)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake gh:", err)

		return 1
	}

	var out strings.Builder

	fmt.Fprintf(&out, "HTTP/2.0 %d %s\r\n", answer.Status, fakeReason(answer.Status))
	out.WriteString("Content-Type: application/json\r\n\r\n")
	out.Write(body)

	_, _ = io.WriteString(os.Stdout, out.String())

	if answer.Status/100 == 2 {
		return 0
	}

	_, _ = fmt.Fprintf(os.Stderr, "gh: %s (HTTP %d) %s\n", fakeReason(answer.Status), answer.Status, fakeOutputMarker)

	return 1
}

// nextAnswer returns the first answer that matches the call of args.
func nextAnswer(dir string, args []string) (fakeAnswer, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ghAnswersFile)) //nolint:gosec // G304: the fake's own file, next to its link in the test's temporary directory
	if err != nil {
		return fakeAnswer{}, false, err
	}

	var answers []fakeAnswer
	if err := json.Unmarshal(data, &answers); err != nil {
		return fakeAnswer{}, false, err
	}

	// gh api --method <M> --include -H ... -H ... [--input -] <endpoint>.
	method, endpoint := "", ""
	if len(args) >= 9 {
		method, endpoint = args[2], args[len(args)-1]
	}

	for _, answer := range answers {
		if answer.matches(method, endpoint) {
			return answer, true, nil
		}
	}

	return fakeAnswer{}, false, nil
}

// fakeReason is the status line's reason phrase.
func fakeReason(status int) string {
	switch status {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 500:
		return "Internal Server Error"
	default:
		return "Status"
	}
}

// appendFakeCall appends one recorded call to the fake's record.
func appendFakeCall(path string, c fakeCall) error {
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: the fake's own record, next to its link in the test's temporary directory
	if err != nil {
		return err
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}
