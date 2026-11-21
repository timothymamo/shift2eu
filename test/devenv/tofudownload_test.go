// Pins that the OpenTofu download dev/tofu.sh makes cannot hang a check (issue #434, finding 4 of the AI
// review of PR #433): a curl that waits for a stalled connection, or for a server that stops sending, has no
// deadline of its own, so `make check` (which runs make infra-check) would wait on it as long as the network
// keeps the socket open - and an agent's pane with it. Every curl invocation the script makes must therefore
// carry --connect-timeout and --max-time with a positive number of seconds.
//
// The test runs the script in a tree of its own: dev/tofu.sh keeps the tarball under bin/tofu, relative to the
// directory it runs in, and reads its pins from infra/tofu.yaml there, so a temporary tree holding a copy of
// the pins and an empty bin/ makes the tarball missing - which is the one path that downloads - without
// reading or writing the worktree's real cache. A fake curl first on PATH records its argv and refuses the
// download, so nothing reaches the network and the script stops before it would hash or extract anything.
package devenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// tofuDownloadScript is the wrapper dev/infra-check.sh, CI and the maintainer run every tofu command
	// through.
	tofuDownloadScript = "dev/tofu.sh"
	// tofuDownloadPins is the pin file dev/tofu.sh reads from its working directory.
	tofuDownloadPins = "infra/tofu.yaml"
	// tofuCurlLogEnv names the file the fake curl appends its argv to.
	tofuCurlLogEnv = "SHIFT2EU_TEST_CURL_LOG"
)

// tofuFakeCurl records one invocation of curl - a curl line, then its arguments, one arg= line each - and
// refuses the download with the status curl gives a failed transfer, so the script stops as it would on a
// real network failure.
const tofuFakeCurl = `#!/bin/sh
{
  printf 'curl\n'
  for arg in "$@"; do printf 'arg=%s\n' "${arg}"; done
} >> "${SHIFT2EU_TEST_CURL_LOG:?set SHIFT2EU_TEST_CURL_LOG}"
echo "fake curl: refusing the download" >&2
exit 22
`

// TestTofuDownloadHasTimeouts pins that every curl dev/tofu.sh makes on the download path carries both a
// connection timeout and a total one, each a positive number of seconds.
func TestTofuDownloadHasTimeouts(t *testing.T) {
	t.Parallel()

	// The script reaches curl only after sed, uname, find, mkdir and mktemp have run: where one of them is
	// missing the test would report a download that never happened, not a timeout.
	for _, tool := range []string{"bash", "sed", "uname", "find", "mkdir", "mktemp"} {
		realTool(t, tool)
	}

	script := scriptPath(t, tofuDownloadScript)

	tree := t.TempDir()
	writeFileMode(t, filepath.Join(tree, tofuDownloadPins), []byte(readRepoFile(t, tofuDownloadPins)), 0o600)

	binDir := filepath.Join(t.TempDir(), "bin")
	writeFileMode(t, filepath.Join(binDir, "curl"), []byte(tofuFakeCurl), 0o700)

	logPath := filepath.Join(t.TempDir(), "curl.log")

	cmd := exec.Command("bash", script, "version")
	cmd.Dir = tree
	cmd.Env = envList(map[string]string{
		"PATH":         binDir + ":" + os.Getenv("PATH"),
		"HOME":         t.TempDir(),
		"TMPDIR":       t.TempDir(),
		tofuCurlLogEnv: logPath,
	})

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		t.Errorf("%s exited 0 although the fake curl refused the download: stdout was:\n%s\nstderr was:\n%s",
			tofuDownloadScript, stdout.String(), stderr.String())
	}

	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("%s made no download, so its timeouts are unchecked (the fake curl recorded nothing in %s): stderr was:\n%s: %v",
			tofuDownloadScript, logPath, stderr.String(), err)
	}

	invocations := tofuCurlInvocations(string(body))
	if len(invocations) == 0 {
		t.Fatalf("%s made no curl call, so its timeouts are unchecked: stderr was:\n%s", tofuDownloadScript, stderr.String())
	}

	for i, args := range invocations {
		for _, flag := range []string{"--connect-timeout", "--max-time"} {
			value, ok := curlFlagValue(args, flag)
			if !ok {
				t.Errorf("curl invocation %d (%s) holds no %s: a download that stalls would hold make check, and an agent's pane, with no deadline",
					i+1, strings.Join(args, " "), flag)

				continue
			}

			seconds, err := strconv.Atoi(value)
			if err != nil || seconds <= 0 {
				t.Errorf("curl invocation %d (%s) sets %s %q, want a positive number of seconds",
					i+1, strings.Join(args, " "), flag, value)
			}
		}
	}
}

// tofuCurlInvocations splits the fake curl's log into invocations: one list of arguments per curl line.
func tofuCurlInvocations(log string) [][]string {
	var (
		invocations [][]string
		current     []string
	)

	for _, line := range strings.Split(log, "\n") {
		switch {
		case line == "curl":
			if current != nil {
				invocations = append(invocations, current)
			}

			current = []string{}
		case strings.HasPrefix(line, "arg="):
			current = append(current, strings.TrimPrefix(line, "arg="))
		}
	}

	if current != nil {
		invocations = append(invocations, current)
	}

	return invocations
}

// curlFlagValue returns the value an invocation gives flag, whether it is written as "--flag value" or as
// "--flag=value".
func curlFlagValue(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag {
			if i+1 < len(args) {
				return args[i+1], true
			}

			return "", false
		}

		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			return value, true
		}
	}

	return "", false
}
