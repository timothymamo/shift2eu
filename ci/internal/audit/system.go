package audit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// The real run's files in the state area, which no tool can read: the
// transcripts, and the launcher's error of a failed CLI run.
const (
	preflightTranscript = stateOutDir + "/preflight.jsonl"
	preflightErrorFile  = stateOutDir + "/preflight.stderr"
	auditTranscript     = stateOutDir + "/audit.jsonl"
	auditErrorFile      = stateOutDir + "/audit.stderr"
	probeOneFile        = stateDir + "/probe-1"
	probeOneTmpFile     = "tmp/shift2eu-audit-probe-1"
	sudoPath            = "/usr/bin/sudo"
)

// The real run's limits.
const (
	// preflightTimeout is the preflight's own deadline: a list of calls, no
	// audit.
	preflightTimeout = 10 * time.Minute
	// maxTranscript caps the preflight's transcript the judgement reads.
	maxTranscript = 16 << 20
	// dialTimeout bounds the launcher's own connect to Docker's socket.
	dialTimeout = 2 * time.Second
	// observeTimeout bounds the launcher's own observations after the
	// preflight's run, under a context detached from the run's.
	observeTimeout = 10 * time.Second
	// outputMode is the mode of the files the launcher writes.
	outputMode fs.FileMode = 0o600
)

// MaxResult caps the result file the audit reads back, in bytes: a file over
// it is refused with a failure of its own.
const MaxResult int64 = 16 << 20

// The real steps' failures. Run prints none of them: each step's line is its
// constant class.
var (
	errNoInputs       = errors.New("the step was handed no inputs")
	errPreflightRun   = errors.New("the preflight's CLI run failed")
	errAuditRun       = errors.New("the audit's CLI run failed")
	errNoResultFile   = errors.New("the audit wrote no result file, or none that is a regular file")
	errResultOverCap  = fmt.Errorf("the audit's result file is over its cap of %d bytes", MaxResult)
	errResultNotJSON  = errors.New("the audit's result file is no single JSON value")
	errResultSecret   = errors.New("the audit's result file holds a secret of the step's")
	errWorkArea       = errors.New("the work area is not as laid out before the audit")
	errPreflightSetup = errors.New("the preflight could not be set up")
)

// Prober runs the CLI once with the preflight's prompt and returns its
// stream-json transcript.
type Prober interface {
	Probe(ctx context.Context, p claudecode.Prompt) ([]byte, error)
}

// Observer makes the launcher's own observations after the preflight's run:
// probe 1's files under root, Docker's socket, sudo, and the $GITHUB_ENV
// file's digest against before; accepted is whether probe 2's listener took a
// connection. Preflight calls it once, after the listener is closed, under a
// context detached from the run's cancellation with a deadline of its own.
type Observer func(ctx context.Context, root, gitHubEnv string, before [32]byte, accepted bool) Effects

// Launch runs the audit's one CLI run with cfg and prompt under root.
type Launch func(ctx context.Context, root string, cfg claudecode.Config, prompt claudecode.Prompt) error

// SystemSteps returns the six steps of a real run on the runner; pid is the
// launcher's own, whose /proc/<pid>/environ probe 5 aims at, and observer
// makes the preflight's observations (Observe on the runner).
func SystemSteps(pid int, observer Observer) Steps {
	return Steps{
		ManagedSettings: CheckManagedSettings,
		Checksum: func(root string, in Inputs) error {
			return InstallClaude(root, in.Tarball, in.Integrity, under(root, claudePath))
		},
		Layout: CheckLayout,
		Bubblewrap: func(ctx context.Context) error {
			return CheckBubblewrap(ctx, ExecRunner{})
		},
		Preflight: func(ctx context.Context, root string) error {
			in, ok := InputsFrom(ctx)
			if !ok {
				return errNoInputs
			}

			return Preflight(ctx, root, in, cliProber{root: root, in: in}, observer, pid)
		},
		Audit: func(ctx context.Context, root string) ([]byte, error) {
			in, ok := InputsFrom(ctx)
			if !ok {
				return nil, errNoInputs
			}

			return Audit(ctx, root, in, cliLaunch)
		},
	}
}

// Preflight runs the sandbox preflight (ADR 0040, "The sandbox preflight"):
// it writes the control's nonce, opens probe 2's listener, records the
// $GITHUB_ENV file's digest, has prober run the CLI with the plan's prompt,
// has observer make the launcher's own observations once the listener is
// closed - under a context detached from ctx's cancellation, so a run that
// was cancelled or timed out is still observed - and judges the transcript
// with the step's nonce, token and canary. The control file is removed
// afterwards, so the audit finds the work area as laid out.
func Preflight(ctx context.Context, root string, in Inputs, prober Prober, observer Observer, pid int) error {
	nonce, err := newNonce()
	if err != nil {
		return errPreflightSetup
	}

	control := under(root, controlPath)
	if err := os.WriteFile(control, []byte(nonce+"\n"), outputMode); err != nil {
		return errPreflightSetup
	}
	defer os.Remove(control)

	before, err := digest(in.GitHubEnv)
	if err != nil {
		return errPreflightSetup
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errPreflightSetup
	}

	accepted := watch(listener)

	port := 0
	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		port = addr.Port
	}

	t := Targets{Root: root, Port: port, Pid: pid, GitHubEnv: in.GitHubEnv}

	transcript, runErr := prober.Probe(ctx, claudecode.Prompt{System: preflightSystem, User: preflightPrompt(t)})

	_ = listener.Close()

	observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), observeTimeout)
	effects := observer(observeCtx, root, in.GitHubEnv, before, accepted())

	cancel()

	if runErr != nil {
		return errPreflightRun
	}

	return Judge(Judgement{
		Plan:       PreflightPlan(t),
		Nonce:      nonce,
		Token:      in.Token,
		Canary:     in.Canary,
		Transcript: transcript,
		Effects:    effects,
	})
}

// Audit runs the audit: it checks that the work area again holds only its
// empty tmp/ - nothing the preflight's run left, no result file of an earlier
// run - then has launch make one CLI run with the skill and the fixed user
// prompt, and takes the result file the run must leave in the work area only
// when it is a regular file, no link, within MaxResult, one JSON value, and
// holds neither the token nor the canary in its bytes or in any of its decoded
// strings. It returns the bytes it read and checked: the publication works
// from them, never from the path. No 64-hex rule applies to them: the audit's
// findings may quote SHA-256 pins.
func Audit(ctx context.Context, root string, in Inputs, launch Launch) ([]byte, error) {
	if err := checkWorkArea(root); err != nil {
		return nil, err
	}

	prompt, err := Prompt(root)
	if err != nil {
		return nil, err
	}

	if err := launch(ctx, root, LaunchConfig(root, in), prompt); err != nil {
		return nil, errAuditRun
	}

	return checkResult(root, in)
}

// cliLaunch is the Launch of a real run: the CLI's transcript in the state
// area, and the launcher's error there too.
func cliLaunch(ctx context.Context, root string, cfg claudecode.Config, prompt claudecode.Prompt) error {
	return askInto(ctx, root, cfg, prompt, auditTranscript, auditErrorFile, io.Discard)
}

// checkWorkArea refuses a work area that holds anything but its tmp/, a real
// directory, or whose tmp/ is not empty.
func checkWorkArea(root string) error {
	entries, err := os.ReadDir(under(root, workDir))
	if err != nil || len(entries) != 1 || entries[0].Name() != "tmp" {
		return errWorkArea
	}

	info, err := os.Lstat(under(root, workTmpDir))
	if err != nil || !info.IsDir() {
		return errWorkArea
	}

	tmp, err := os.ReadDir(under(root, workTmpDir))
	if err != nil || len(tmp) != 0 {
		return errWorkArea
	}

	return nil
}

// checkResult takes the result file and returns its bytes: a regular file and
// no link, within MaxResult, one JSON value, that holds neither the step's
// token nor its canary.
func checkResult(root string, in Inputs) ([]byte, error) {
	body, err := readResult(under(root, resultPath))
	if err != nil {
		return nil, err
	}

	strs, err := jsonStrings(body)
	if err != nil {
		return nil, errResultNotJSON
	}

	for _, secret := range []string{in.Token, in.Canary} {
		if secret == "" {
			continue
		}

		if bytes.Contains(body, []byte(secret)) || slices.ContainsFunc(strs, func(s string) bool { return strings.Contains(s, secret) }) {
			return nil, errResultSecret
		}
	}

	return body, nil
}

// readResult reads the result file at path from one descriptor, never through
// a link: the path's entry must be a regular file (Lstat, so neither a link
// nor a FIFO is ever opened), and the descriptor opened must be that same file
// (os.SameFile on its Stat), which gives O_NOFOLLOW's guarantee with the os
// package alone - os has no O_NOFOLLOW, and syscall is outside the library's
// allow list. The read is capped at MaxResult from that descriptor.
func readResult(path string) ([]byte, error) {
	entry, err := os.Lstat(path)
	if err != nil || !entry.Mode().IsRegular() {
		return nil, errNoResultFile
	}

	file, err := os.Open(path) //nolint:gosec // G304: the layout's own result path, checked as a regular file and as the entry opened
	if err != nil {
		return nil, errNoResultFile
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(entry, opened) {
		return nil, errNoResultFile
	}

	body, err := io.ReadAll(io.LimitReader(file, MaxResult+1))
	if err != nil {
		return nil, errNoResultFile
	}

	if int64(len(body)) > MaxResult {
		return nil, errResultOverCap
	}

	return body, nil
}

// jsonStrings reads body as exactly one JSON value and returns every string in
// it, as decoded, in the order the document holds them: the objects' keys and
// every value of a duplicate key included, since it walks the document's
// tokens rather than decoding it into a map, which keeps a duplicate key's
// last value alone. A secret written with JSON escapes is found in them though
// no byte of the file spells it.
func jsonStrings(body []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var strs []string

	for depth, first := 0, true; first || depth > 0; first = false {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}

		switch t := token.(type) {
		case json.Delim:
			if t == '{' || t == '[' {
				depth++
			} else {
				depth--
			}
		case string:
			strs = append(strs, t)
		}
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errResultNotJSON
	}

	return strs, nil
}

// cliProber is the Prober of a real run: the audit's launch with the
// preflight's deadline.
type cliProber struct {
	root string
	in   Inputs
}

// Probe runs the CLI and returns the transcript it printed.
func (p cliProber) Probe(ctx context.Context, prompt claudecode.Prompt) ([]byte, error) {
	cfg := LaunchConfig(p.root, p.in)
	cfg.Timeout = preflightTimeout

	var transcript cappedWriter

	err := askInto(ctx, p.root, cfg, prompt, preflightTranscript, preflightErrorFile, &transcript)

	return transcript.buf.Bytes(), err
}

// askInto runs one CLI run with cfg, its transcript copied to the state area's
// file and to also, and the launcher's error, which can quote the CLI's
// stderr, written to the state area's error file and nowhere else.
func askInto(ctx context.Context, root string, cfg claudecode.Config, prompt claudecode.Prompt, transcriptFile, errorFile string, also io.Writer) error {
	file, err := os.OpenFile(under(root, transcriptFile), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, outputMode)
	if err != nil {
		return fmt.Errorf("open the transcript: %w", err)
	}
	defer file.Close()

	cfg.Transcript = io.MultiWriter(file, also)

	if _, err := claudecode.New(cfg).Ask(ctx, prompt); err != nil {
		_ = os.WriteFile(under(root, errorFile), []byte(err.Error()+"\n"), outputMode)

		return err
	}

	return file.Close()
}

// cappedWriter keeps what it is written up to maxTranscript bytes, and fails
// beyond, which fails the run: a transcript the judgement cannot read whole is
// never judged.
type cappedWriter struct {
	buf bytes.Buffer
}

// errTranscriptCap is a transcript over maxTranscript.
var errTranscriptCap = errors.New("the preflight's transcript is over its cap")

// Write keeps p, or fails when it would pass the cap.
func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > maxTranscript {
		return 0, errTranscriptCap
	}

	return w.buf.Write(p)
}

// newNonce returns 16 random bytes in hex: shorter than the canary, so the
// control's result is never taken for it.
func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make the nonce: %w", err)
	}

	return hex.EncodeToString(raw), nil
}

// digest returns the sha256 of the file at path.
func digest(path string) ([32]byte, error) {
	body, err := readCapped(path, maxTranscript)
	if err != nil {
		return [32]byte{}, err
	}

	return sha256.Sum256(body), nil
}

// watch accepts every connection to listener until it is closed, and returns
// a function that waits for that and reports whether any was accepted.
func watch(listener net.Listener) func() bool {
	var (
		accepted atomic.Bool
		done     sync.WaitGroup
	)

	done.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			accepted.Store(true)
			_ = conn.Close()
		}
	})

	return func() bool {
		done.Wait()

		return accepted.Load()
	}
}

// Observe is the Observer of a real run: it checks probe 1's two files, dials
// Docker's socket itself, runs `sudo -n true` itself and digests the
// $GITHUB_ENV file again. A file it cannot digest counts as changed.
func Observe(ctx context.Context, root, gitHubEnv string, before [32]byte, accepted bool) Effects {
	effects := observedEffects{listener: accepted}

	effects.probeFiles = exists(under(root, probeOneFile)) || exists(under(root, probeOneTmpFile))

	dialer := net.Dialer{Timeout: dialTimeout}
	if conn, err := dialer.DialContext(ctx, "unix", under(root, dockerSocket)); err == nil {
		effects.docker = true
		_ = conn.Close()
	}

	effects.sudo = ExecRunner{}.Check(ctx, []string{sudoPath, "-n", "true"}) == nil

	after, err := digest(gitHubEnv)
	effects.envDigest = err != nil || after != before

	return effects
}

// exists reports whether path is an entry, a link included; a path that
// cannot be checked counts as one.
func exists(path string) bool {
	_, err := os.Lstat(path)

	return !errors.Is(err, fs.ErrNotExist)
}

// observedEffects are a real run's observations.
type observedEffects struct {
	probeFiles bool
	listener   bool
	docker     bool
	sudo       bool
	envDigest  bool
}

// ProbeFilesExist reports whether either of probe 1's files exists.
func (e observedEffects) ProbeFilesExist() bool { return e.probeFiles }

// ListenerAccepted reports whether probe 2's listener accepted a connection.
func (e observedEffects) ListenerAccepted() bool { return e.listener }

// DockerSocketReachable reports whether the launcher reached Docker's socket.
func (e observedEffects) DockerSocketReachable() bool { return e.docker }

// SudoAvailable reports whether `sudo -n true` ran.
func (e observedEffects) SudoAvailable() bool { return e.sudo }

// GitHubEnvDigestChanged reports whether the $GITHUB_ENV file changed.
func (e observedEffects) GitHubEnvDigestChanged() bool { return e.envDigest }
