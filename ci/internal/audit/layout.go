package audit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// The audit directory's paths, relative to the root that stands in for /
// (ADR 0040, "The audit directory").
const (
	auditDir    = "srv/audit"
	checkoutDir = auditDir + "/checkout"
	checksDir   = auditDir + "/checks"
	workDir     = auditDir + "/work"
	workTmpDir  = workDir + "/tmp"
	stateDir    = auditDir + "/state"
	homeDir     = stateDir + "/home"
	stateTmpDir = stateDir + "/tmp"
	stateBinDir = stateDir + "/bin"
	stateOutDir = stateDir + "/out"
	binDir      = auditDir + "/bin"
	wrapperPath = binDir + "/audit-exec"
	claudePath  = stateBinDir + "/claude"
	resultPath  = workDir + "/result.json"
)

// wrapperMode is the wrapper's one mode: runnable and readable by all,
// writable by nobody.
const wrapperMode fs.FileMode = 0o555

// writeBits are the permission bits that let anyone write.
const writeBits fs.FileMode = 0o222

// under returns rel, a slash-separated path of the layout, under root.
func under(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// dirRule is what the layout holds of one directory.
type dirRule struct {
	path string
	// readOnly is a directory no one may write to.
	readOnly bool
	// entries, when non-nil, are the only names the directory may hold.
	entries []string
}

// layoutDirs are the audit directory's directories, parents first. Each is
// checked as an entry (Lstat), so a symbolic link anywhere in the layout is
// refused rather than followed.
var layoutDirs = []dirRule{
	{path: "srv"},
	{path: auditDir},
	{path: checkoutDir, readOnly: true},
	{path: checksDir, readOnly: true},
	{path: workDir, entries: []string{"tmp"}},
	{path: workTmpDir, entries: []string{}},
	{path: stateDir},
	{path: homeDir, entries: []string{}},
	{path: stateTmpDir, entries: []string{}},
	{path: stateBinDir},
	{path: stateOutDir},
	{path: binDir},
}

// CheckLayout checks that the audit directory under root is laid out as ADR
// 0040 gives it: every directory a directory and no symbolic link, the
// checkout copy and the checks' results without a write bit, the work area
// holding only its empty tmp/, the state area's home/ and tmp/ empty, and the
// wrapper a regular file of mode 0555. It creates nothing: a missing path is
// the model-free step that did not run. The wrapper's owner (root) is the
// model-free step's `sudo install -o root`; this check holds its mode.
func CheckLayout(root string) error {
	for _, dir := range layoutDirs {
		if err := checkDir(under(root, dir.path), dir); err != nil {
			return err
		}
	}

	info, err := os.Lstat(under(root, wrapperPath))
	if err != nil {
		return fmt.Errorf("the audit directory: the wrapper: %w", err)
	}

	if !info.Mode().IsRegular() || info.Mode().Perm() != wrapperMode {
		return fmt.Errorf("the audit directory: the wrapper is %s, want a regular file of mode %s", info.Mode(), wrapperMode)
	}

	return nil
}

// errLayout is a path of the audit directory that is not as laid out.
var errLayout = errors.New("not as laid out")

// checkDir checks one directory of the layout at path.
func checkDir(path string, rule dirRule) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("the audit directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("the audit directory: %s is %s, want a directory: %w", rule.path, info.Mode().Type(), errLayout)
	}

	if rule.readOnly && info.Mode().Perm()&writeBits != 0 {
		return fmt.Errorf("the audit directory: %s has a write bit (%s): %w", rule.path, info.Mode().Perm(), errLayout)
	}

	if rule.entries == nil {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("the audit directory: %w", err)
	}

	for _, entry := range entries {
		if !slices.Contains(rule.entries, entry.Name()) {
			return fmt.Errorf("the audit directory: %s holds %q: %w", rule.path, entry.Name(), errLayout)
		}
	}

	return nil
}

// managedSettingsPaths are the paths the CLI loads a managed policy from, the
// directory itself in each case, relative to the root.
var managedSettingsPaths = []string{
	"etc/claude-code",
	"Library/Application Support/ClaudeCode",
}

// CheckManagedSettings refuses a root that holds a Claude Code managed-settings
// path: /etc/claude-code or, though the runner is Linux, /Library/Application
// Support/ClaudeCode, each checked with Lstat so a symbolic link counts whether
// or not its target exists. A path that cannot be checked is refused too.
func CheckManagedSettings(root string) error {
	for _, rel := range managedSettingsPaths {
		_, err := os.Lstat(under(root, rel))

		switch {
		case err == nil:
			return fmt.Errorf("managed settings: %s exists, and its policy would override the audit's settings (ADR 0040)", rel)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("managed settings: %s cannot be checked: %w", rel, err)
		}
	}

	return nil
}
