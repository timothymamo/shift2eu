package audit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// bwrapPath is bubblewrap's path on the runner, the package the model-free
// step installs from the Ubuntu archive.
const bwrapPath = "/usr/bin/bwrap"

// bwrapCheck is the start check's invocation after the program (ADR 0040, "The
// runner's user-namespace restriction"): every namespace unshared, no sandbox
// outliving the launcher, the whole filesystem read-only and /bin/true.
var bwrapCheck = []string{"--unshare-all", "--die-with-parent", "--ro-bind", "/", "/", "/bin/true"}

// Runner runs one command, argv[0] an absolute path, and reports whether it
// started and exited 0.
type Runner interface {
	Run(ctx context.Context, argv []string) error
}

// CheckBubblewrap starts bubblewrap once, with the invocation the ADR gives,
// through runner. A start that fails is an error, whatever the cause; it is
// not retried.
func CheckBubblewrap(ctx context.Context, runner Runner) error {
	argv := append([]string{bwrapPath}, bwrapCheck...)

	if err := runner.Run(ctx, argv); err != nil {
		return fmt.Errorf("bubblewrap cannot start: %w", err)
	}

	return nil
}

// ExecRunner is the Runner of a real run: the command starts with an
// environment of PATH alone, in /, with no stdin and its output discarded.
type ExecRunner struct{}

// Run starts argv and waits for it.
func (ExecRunner) Run(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("run: no command")
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv is the launcher's own fixed invocation (bwrap, sudo), never a caller's
	cmd.Env = []string{"PATH=" + searchPath}
	cmd.Dir = "/"

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", argv[0], err)
	}

	return nil
}
