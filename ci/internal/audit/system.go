package audit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
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
	// outputMode is the mode of the files the launcher writes.
	outputMode fs.FileMode = 0o600
)

// The real steps' failures. Run prints none of them: each step's line is its
// constant class.
var (
	errNoInputs       = errors.New("the step was handed no inputs")
	errPreflightRun   = errors.New("the preflight's CLI run failed")
	errAuditRun       = errors.New("the audit's CLI run failed")
	errNoResultFile   = errors.New("the audit wrote no result file")
	errPreflightSetup = errors.New("the preflight could not be set up")
)

// Prober runs the CLI once with the preflight's prompt and returns its
// stream-json transcript.
type Prober interface {
	Probe(ctx context.Context, p claudecode.Prompt) ([]byte, error)
}

// SystemSteps returns the six steps of a real run on the runner; pid is the
// launcher's own, whose /proc/<pid>/environ probe 5 aims at.
func SystemSteps(pid int) Steps {
	return Steps{
		ManagedSettings: CheckManagedSettings,
		Checksum: func(root string, in Inputs) error {
			return InstallClaude(in.Tarball, in.Integrity, under(root, claudePath))
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

			return Preflight(ctx, root, in, cliProber{root: root, in: in}, pid)
		},
		Audit: func(ctx context.Context, root string) error {
			in, ok := InputsFrom(ctx)
			if !ok {
				return errNoInputs
			}

			return Audit(ctx, root, in)
		},
	}
}

// Preflight runs the sandbox preflight (ADR 0040, "The sandbox preflight"):
// it writes the control's nonce, opens probe 2's listener, records the
// $GITHUB_ENV file's digest, has prober run the CLI with the plan's prompt,
// observes the effects itself and judges the transcript. The control file is
// removed afterwards, so the audit finds the work area as laid out.
func Preflight(ctx context.Context, root string, in Inputs, prober Prober, pid int) error {
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

	t := targets{root: root, port: port, pid: pid, gitHubEnv: in.GitHubEnv}

	transcript, runErr := prober.Probe(ctx, claudecode.Prompt{System: preflightSystem, User: preflightPrompt(t)})

	_ = listener.Close()

	effects := observe(ctx, root, in.GitHubEnv, before, accepted())

	if runErr != nil {
		return errPreflightRun
	}

	return Judge(Judgement{
		Plan:       PreflightPlan(),
		Nonce:      nonce,
		Canary:     in.Canary,
		Transcript: transcript,
		Effects:    effects,
	})
}

// Audit runs the audit: one CLI run with the skill and the fixed user prompt,
// its transcript in the state area, and a result file the run must leave in
// the work area.
func Audit(ctx context.Context, root string, in Inputs) error {
	prompt, err := Prompt(root)
	if err != nil {
		return err
	}

	cfg := LaunchConfig(root, in)

	if err := askInto(ctx, root, cfg, prompt, auditTranscript, auditErrorFile, io.Discard); err != nil {
		return errAuditRun
	}

	info, err := os.Lstat(under(root, resultPath))
	if err != nil || !info.Mode().IsRegular() {
		return errNoResultFile
	}

	return nil
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

// observe makes the launcher's own observations after the preflight's run.
func observe(ctx context.Context, root, gitHubEnv string, before [32]byte, accepted bool) observedEffects {
	effects := observedEffects{listener: accepted}

	effects.probeFiles = exists(under(root, probeOneFile)) || exists(under(root, probeOneTmpFile))

	dialer := net.Dialer{Timeout: dialTimeout}
	if conn, err := dialer.DialContext(ctx, "unix", under(root, dockerSocket)); err == nil {
		effects.docker = true
		_ = conn.Close()
	}

	effects.sudo = ExecRunner{}.Run(ctx, []string{sudoPath, "-n", "true"}) == nil

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
