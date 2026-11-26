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
	"path/filepath"
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
	errAuditCwd       = errors.New("the audit's working directory is not an empty real directory before the audit")
	errFileCommands   = errors.New("the file-command directory of the $GITHUB_ENV file could not be read, is over a cap, or changed across the audit's run")
)

// Prober runs the CLI once with the preflight's prompt and returns its
// stream-json transcript.
type Prober interface {
	Probe(ctx context.Context, p claudecode.Prompt) ([]byte, error)
}

// Observer makes the launcher's own observations after the preflight's run:
// probe 1's files under root, Docker's socket, sudo, and the digest of the
// file-command directory holding the $GITHUB_ENV file against before;
// accepted is whether probe 2's listener took a connection. Preflight calls
// it once, after the listener is closed, under a
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
// it writes the control's nonce, opens probe 2's listener, records the digest
// of the $GITHUB_ENV file's directory, has prober run the CLI with the plan's
// prompt, has observer make the launcher's own observations once the listener
// is closed - under a context detached from ctx's cancellation, so a run that
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
	// The removal's error is dropped on purpose: the audit's checkWorkArea is
	// the guard, and refuses a work area that still holds the control file.
	defer func() { _ = os.Remove(control) }()

	// The preflight's digest, and Observe's, leave no entry out: nothing
	// legitimate writes the step summary while the preflight runs, since Run
	// prints its one line after the last step.
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
		// A failed run is still judged by what the launcher observed itself:
		// an effect seen means the sandbox did not hold, whatever the run did.
		if verdict := effectsVerdict(effects); verdict != nil {
			return verdict
		}

		return keepModeError(errPreflightRun, runErr)
	}

	return Judge(Judgement{
		Plan:           PreflightPlan(t),
		Nonce:          nonce,
		Token:          in.Token,
		Canary:         in.Canary,
		Transcript:     transcript,
		Effects:        effects,
		PermissionMode: permissionMode,
	})
}

// Audit runs the audit: it checks that the work area again holds only its
// empty tmp/ - nothing the preflight's run left, no result file of an earlier
// run - and that its own working directory cwd-audit/ is still an empty real
// directory, then has launch make one CLI run with the skill and the fixed
// user prompt, the digest of the runner's file-command directory (the
// $GITHUB_ENV file's, every entry, the step summary included) taken before the
// run and after it and any change, or a directory over a cap, failing the run,
// and takes the result file the run must leave in the work area only when it
// is a regular file, no link, within MaxResult, one JSON value, and holds
// neither the token nor the canary in its bytes or in any of its decoded
// strings. It returns the bytes it read and checked: the publication works
// from them, never from the path. No 64-hex rule applies to them: the audit's
// findings may quote SHA-256 pins.
func Audit(ctx context.Context, root string, in Inputs, launch Launch) ([]byte, error) {
	if err := checkWorkArea(root); err != nil {
		return nil, err
	}

	if err := checkAuditCwd(root); err != nil {
		return nil, err
	}

	prompt, err := Prompt(root)
	if err != nil {
		return nil, err
	}

	// The step summary is digested like every other entry: auditrun creates
	// it before the run, and writes the run's line only after the last step
	// returns, so nothing legitimate writes it between the two digests.
	before, err := digest(in.GitHubEnv)
	if err != nil {
		return nil, errFileCommands
	}

	runErr := launch(ctx, root, LaunchConfig(root, in), prompt)

	// The runner's file commands decide what a later step of the job runs
	// with: any change to their directory across the audit's run fails it,
	// whatever the run did (issues #615, #614).
	after, err := digest(in.GitHubEnv)
	if err != nil || after != before {
		return nil, errFileCommands
	}

	if runErr != nil {
		return nil, keepModeError(errAuditRun, runErr)
	}

	return checkResult(root, in)
}

// keepModeError returns class, a step's constant failure, wrapping the
// launcher's *claudecode.PermissionModeError when runErr holds one, so Run
// prints its fixed sentence (issue #592). Nothing else of runErr is kept: its
// text can quote the CLI's output.
func keepModeError(class, runErr error) error {
	var modeErr *claudecode.PermissionModeError
	if errors.As(runErr, &modeErr) {
		return fmt.Errorf("%w: %w", class, &claudecode.PermissionModeError{Expected: modeErr.Expected, Reported: modeErr.Reported})
	}

	return class
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

// checkAuditCwd refuses an audit's working directory that is not an empty real
// directory (Lstat, no link followed): the CLI reads the directory it starts
// in, so nothing a preflight session could have left may be there (issues
// #615, #614).
func checkAuditCwd(root string) error {
	info, err := os.Lstat(under(root, auditCwdDir))
	if err != nil || !info.IsDir() {
		return errAuditCwd
	}

	entries, err := os.ReadDir(under(root, auditCwdDir))
	if err != nil || len(entries) != 0 {
		return errAuditCwd
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
// a link: the path's entry must be a regular file (Lstat), and the descriptor
// opened must be that same file (os.SameFile on its Stat), so what is read is
// the entry Lstat saw, never a link's target. It is not O_NOFOLLOW, which the
// os package lacks and syscall is outside the library's allow list for: the
// entry can change between Lstat and Open, and a FIFO swapped in at path in
// that window makes the Open wait for a writer, the risk ADR 0040 records
// (it needs a process outliving the CLI, and should the Open return, the
// SameFile check refuses what it opened). The read is capped at MaxResult
// from that descriptor.
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

// maxResultDepth is the deepest nesting of objects and arrays the result file
// may hold (ADR 0040): the audit's own limit.
const maxResultDepth = 10000

// jsonStrings reads body as exactly one JSON value and returns every string in
// it, as decoded, in the order the document holds them: the objects' keys and
// every value of a duplicate key included, since it walks the document's
// tokens rather than decoding it into a map, which keeps a duplicate key's
// last value alone. A secret written with JSON escapes is found in them though
// no byte of the file spells it. A document nested deeper than maxResultDepth
// is refused as no JSON, by this walk itself rather than by encoding/json's
// own limit.
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
				if depth > maxResultDepth {
					return nil, errResultNotJSON
				}
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
	cfg.WorkDir = under(p.root, preflightCwdDir)

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

// The caps of the runner's file-command directory the launch digests (issues
// #615, #614): a directory over either is refused, never digested in part.
const (
	maxFileCommandEntries = 64
	maxFileCommandBytes   = 4 << 20
)

// The caps' failures, each naming its cap. Neither reaches a printed line:
// Audit and Preflight report their own constant class.
var (
	errFileCommandEntries = fmt.Errorf("the file-command directory holds more than %d entries", maxFileCommandEntries)
	errFileCommandBytes   = fmt.Errorf("the file-command directory holds more than %d bytes", maxFileCommandBytes)
)

// FileCommandDir returns the directory a file command's path names,
// filepath.Dir of the cleaned path: the runner's file-command directory when
// path is $GITHUB_ENV's. auditrun refuses a step whose $GITHUB_PATH,
// $GITHUB_OUTPUT, $GITHUB_STATE or step summary is in another directory than
// $GITHUB_ENV's, one the digest would not cover (issues #615, #614).
func FileCommandDir(path string) string {
	return filepath.Dir(filepath.Clean(path))
}

// IsCleanAbsolute reports whether path is absolute and already its own
// cleaned form, as the runner sets every file command's path. auditrun
// refuses any other (issue #614): FileCommandDir's comparison is lexical, so a
// path that names its directory only after Clean - a . or .. element, a
// redundant separator - or a relative one, which names whatever directory the
// step runs in, could pass it while the runner reads another file.
func IsCleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && path == filepath.Clean(path)
}

// digest returns the sha256 of the runner's file-command directory, the one
// holding the $GITHUB_ENV file at gitHubEnv beside $GITHUB_PATH,
// $GITHUB_OUTPUT, $GITHUB_STATE and the step summary: every entry's name, its
// type and, for a regular file, its bytes, the entries in name order, none
// left out (issues #615, #614). An entry is taken as it is (Lstat): a link is
// its type and its own target's text, never what it points at, so a file
// replaced by a link to the same bytes is a change. Mode bits and link counts
// are not hashed: the runner reads only a file command's bytes. A directory of
// more than maxFileCommandEntries entries, or more than maxFileCommandBytes of
// content, is an error naming that cap, as is any entry that cannot be read.
func digest(gitHubEnv string) ([32]byte, error) {
	// The $GITHUB_ENV file itself must be there, a regular file.
	if info, err := os.Lstat(gitHubEnv); err != nil || !info.Mode().IsRegular() {
		return [32]byte{}, errors.New("the $GITHUB_ENV file is no regular file")
	}

	dir := FileCommandDir(gitHubEnv)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return [32]byte{}, fmt.Errorf("read the file-command directory: %w", err)
	}

	if len(entries) > maxFileCommandEntries {
		return [32]byte{}, errFileCommandEntries
	}

	hash := sha256.New()
	total := 0

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		info, err := os.Lstat(path)
		if err != nil {
			return [32]byte{}, fmt.Errorf("check a file command: %w", err)
		}

		var body []byte

		switch {
		case info.Mode().IsRegular():
			body, err = readEntry(path, info, maxFileCommandBytes-total)
		case info.Mode()&fs.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(path)
			body = []byte(target)
		}

		if err != nil {
			return [32]byte{}, err
		}

		total += len(body)
		if total > maxFileCommandBytes {
			return [32]byte{}, errFileCommandBytes
		}

		// Each field is length-prefixed, so no two directories hash alike.
		for _, field := range [][]byte{[]byte(entry.Name()), []byte(info.Mode().Type().String()), body} {
			_, _ = fmt.Fprintf(hash, "%d:", len(field))
			_, _ = hash.Write(field)
		}
	}

	var sum [32]byte

	copy(sum[:], hash.Sum(nil))

	return sum, nil
}

// readEntry reads the regular file at path that Lstat saw as entry, from one
// descriptor that must be that same file (os.SameFile), never a link's target,
// and fails past limit bytes with errFileCommandBytes. The open is O_RDWR,
// never O_RDONLY, because on Linux an O_RDWR open never blocks on a FIFO a
// concurrent process swapped in between the Lstat and the open, where an
// O_RDONLY one waits for a writer (issues #615, #614); the descriptor is only
// read through. An entry it cannot open O_RDWR - one the run may read but not
// write - fails the digest (fail closed): the runner's file commands are its
// own and writable.
func readEntry(path string, entry fs.FileInfo, limit int) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // G304: an entry of the runner's file-command directory, checked as the regular file Lstat saw
	if err != nil {
		return nil, fmt.Errorf("open a file command for reading and writing: %w", err)
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(entry, opened) {
		return nil, errors.New("a file command is not the entry it was")
	}

	body, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("read a file command: %w", err)
	}

	if len(body) > limit {
		return nil, errFileCommandBytes
	}

	return body, nil
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
// $GITHUB_ENV file's directory again. A directory it cannot digest counts as
// changed.
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
	effects.fileCommands = err != nil || after != before

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
	probeFiles   bool
	listener     bool
	docker       bool
	sudo         bool
	fileCommands bool
}

// ProbeFilesExist reports whether either of probe 1's files exists.
func (e observedEffects) ProbeFilesExist() bool { return e.probeFiles }

// ListenerAccepted reports whether probe 2's listener accepted a connection.
func (e observedEffects) ListenerAccepted() bool { return e.listener }

// DockerSocketReachable reports whether the launcher reached Docker's socket.
func (e observedEffects) DockerSocketReachable() bool { return e.docker }

// SudoAvailable reports whether `sudo -n true` ran.
func (e observedEffects) SudoAvailable() bool { return e.sudo }

// FileCommandsChanged reports whether the runner's file-command directory, the
// $GITHUB_ENV file's, changed.
func (e observedEffects) FileCommandsChanged() bool { return e.fileCommands }
