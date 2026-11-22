package audit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
// checkout copy and the checks' results without a write bit anywhere in them
// and without a link that leaves its own tree, the checkout copy's .git a
// directory whose config names no program, credential header, hook directory,
// driver or include, the work area
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

	for _, tree := range []string{checkoutDir, checksDir} {
		if err := checkReadOnlyTree(under(root, tree)); err != nil {
			return fmt.Errorf("the audit directory: %s: %w", tree, err)
		}
	}

	return checkGitConfig(under(root, checkoutDir))
}

// checkReadOnlyTree walks the tree at top, no link followed (WalkDir's Lstat
// semantics), and refuses any entry with a write bit and any symbolic link
// that is absolute or does not resolve - every link of its chain followed - to
// a path inside the tree.
func checkReadOnlyTree(top string) error {
	realTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		return fmt.Errorf("resolve the tree: %w", err)
	}

	return filepath.WalkDir(top, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			return checkTreeLink(realTop, path)
		}

		if info.Mode().Perm()&writeBits != 0 {
			return fmt.Errorf("%s has a write bit (%s): %w", path, info.Mode().Perm(), errLayout)
		}

		return nil
	})
}

// checkTreeLink refuses the symbolic link at path unless it is relative and
// resolves to a path that exists inside realTop.
func checkTreeLink(realTop, path string) error {
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("read the link %s: %w", path, err)
	}

	if filepath.IsAbs(target) {
		return fmt.Errorf("%s is an absolute link: %w", path, errLayout)
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("%s does not resolve: %w", path, errLayout)
	}

	rel, err := filepath.Rel(realTop, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves outside its tree: %w", path, errLayout)
	}

	return nil
}

// gitConfigRefusedKeys are the git configuration keys, lower-case as git
// compares them, that name a program, a hook directory or a credential:
// http.extraheader (any URL's), core.fsmonitor, core.sshCommand,
// core.hooksPath, and the program-naming keys beside them (core.gitProxy,
// core.askPass, core.pager, diff.external, a driver's textconv).
var gitConfigRefusedKeys = []string{
	"extraheader",
	"fsmonitor",
	"sshcommand",
	"hookspath",
	"gitproxy",
	"askpass",
	"pager",
	"external",
	"textconv",
}

// gitConfigRefusedDrivers are the sections that, with a subsection, define a
// driver git runs: [filter "x"], [diff "x"] and [merge "x"].
var gitConfigRefusedDrivers = []string{"filter", "diff", "merge"}

// gitConfigRefusedSections are the sections that pull another file's
// configuration in: [include] and [includeIf "..."].
var gitConfigRefusedSections = []string{"include", "includeif"}

// checkGitConfig checks the checkout copy's git directory: .git a real
// directory, no file or link, and its config, when there is one, a regular
// file that holds no key or section the wrapper's git would run a program
// with, send a credential with or read another file's configuration from.
func checkGitConfig(checkout string) error {
	gitDir := filepath.Join(checkout, ".git")

	info, err := os.Lstat(gitDir)
	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's .git: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("the audit directory: the checkout copy's .git is %s, want a directory: %w", info.Mode().Type(), errLayout)
	}

	config := filepath.Join(gitDir, "config")

	info, err = os.Lstat(config)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's git config: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("the audit directory: the checkout copy's git config is %s, want a regular file: %w", info.Mode().Type(), errLayout)
	}

	body, err := readCapped(config, maxGitConfig)
	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's git config: %w", err)
	}

	if refused, ok := refusedGitConfig(string(body)); ok {
		return fmt.Errorf("the audit directory: the checkout copy's git config holds %s: %w", refused, errLayout)
	}

	return nil
}

// maxGitConfig caps the git config the layout check reads.
const maxGitConfig = 1 << 20

// refusedGitConfig returns the first key or section of a git config that the
// layout refuses, lower-cased, and true; or false when there is none. It reads
// every line as git might: a section header, possibly with a key after it on
// the same line, or a key, matched case-insensitively. A line it cannot place
// is read as a key too, so a continued value can only add a refusal.
func refusedGitConfig(body string) (string, bool) {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "[") {
			header, rest, _ := strings.Cut(line[1:], "]")

			if refused, ok := refusedGitSection(header); ok {
				return refused, true
			}

			line = strings.TrimSpace(rest)
		}

		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == '=' || r == ' ' || r == '\t'
		})
		if len(fields) == 0 {
			continue
		}

		if key := strings.ToLower(fields[0]); slices.Contains(gitConfigRefusedKeys, key) {
			return key, true
		}
	}

	return "", false
}

// refusedGitSection returns a section header's name, lower-cased, and true
// when the layout refuses it: an include, or a driver's section with a
// subsection, in the quoted ([diff "x"]) or the old dotted ([diff.x]) form.
func refusedGitSection(header string) (string, bool) {
	header = strings.TrimSpace(header)

	name, subsection := header, false
	if i := strings.IndexAny(header, " \t\"."); i >= 0 {
		name, subsection = header[:i], true
	}

	name = strings.ToLower(name)

	switch {
	case slices.Contains(gitConfigRefusedSections, name):
		return "[" + name + "]", true
	case subsection && slices.Contains(gitConfigRefusedDrivers, name):
		return "[" + name + " \"...\"]", true
	default:
		return "", false
	}
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
