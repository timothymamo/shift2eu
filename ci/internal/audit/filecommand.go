package audit

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FileCommandPath is one of the step's file commands: the variable that names
// it ($GITHUB_ENV, $GITHUB_PATH, $GITHUB_OUTPUT, $GITHUB_STATE or
// $GITHUB_STEP_SUMMARY) and its path, which ci/auditrun has checked is a clean
// absolute path in the $GITHUB_ENV file's directory.
type FileCommandPath struct {
	Name string
	Path string
}

// FileCommands is the step's file commands as RecordFileCommands found them
// before the steps: each one's identity (Lstat, and for a regular file a
// descriptor checked with os.SameFile) and the sha256 of its content, read
// through that descriptor, at most maxFileCommandBytes, the digest's cap
// (issues #615, #614). auditrun is the only legitimate writer of these files,
// and it writes only its summary line, through Appended; Repair empties every
// one the steps changed.
type FileCommands struct {
	commands []*fileCommand
}

// fileCommand is one recorded file command.
type fileCommand struct {
	FileCommandPath

	// absent is a path that did not exist before the steps.
	absent bool
	// before is the path's Lstat before the steps, nil when absent.
	before fs.FileInfo
	// file is the regular file's descriptor, opened for reading and, when
	// the run may, writing; nil for a path that is no regular file or could
	// not be opened, which is checked by its identity alone.
	file *os.File
	// expected is the sha256 state of what the file must hold after the
	// steps: what it held, and what the run appended through Appended.
	expected summer
	// overCap is a file over maxFileCommandBytes before the steps: its
	// content cannot be compared, so it counts as changed.
	overCap bool
}

// summer is a running sha256: what sha256.New returns.
type summer interface {
	io.Writer
	Sum(b []byte) []byte
}

// errNotRecorded is a regular file whose content could not be read.
var errNotRecorded = errors.New("the file command's content could not be read")

// errNotRegular is a file command that exists but is no regular file: a
// directory, a link, a FIFO or a device, which the runner never lays down.
var errNotRegular = errors.New("it exists and is no regular file")

// CheckFileCommands refuses file commands that exist but are no regular file
// (issues #615, #614, the auditor's round-4 L1): each path is Lstat'ed, so no
// link is followed and nothing is opened, and a directory, a link, a FIFO or a
// device fails the check, as does a path Lstat cannot read. A path that does
// not exist passes: the run records it as absent. ci/auditrun runs it before
// it opens the step summary and before any step, so a run that cannot record a
// file command's content never starts.
func CheckFileCommands(paths []FileCommandPath) error {
	for _, path := range paths {
		if err := checkFileCommandEntry(path.Path); err != nil {
			return fmt.Errorf("the file command %s: %w", path.Name, err)
		}
	}

	return nil
}

// checkFileCommandEntry checks that path is a regular file or does not exist.
func checkFileCommandEntry(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return errors.New("its path cannot be checked")
	}

	if !info.Mode().IsRegular() {
		return errNotRegular
	}

	return nil
}

// OpenAppend opens the file command at path for appending without following a
// link, and creates it, with mode, when it does not exist. A path that does
// not exist is created with O_CREATE|O_EXCL, which refuses any entry there, a
// dangling link included; an existing path must be a regular file (Lstat), and
// the descriptor opened must be that same file (os.SameFile), never a link's
// target. Anything else is refused. It is not O_NOFOLLOW, which the os
// package lacks and syscall is outside the library's allow list for; the
// Lstat comes first, so a FIFO is never opened, and ci/auditrun calls it
// before any step runs.
func OpenAppend(path string, mode fs.FileMode) (*os.File, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) //nolint:gosec // G304: one of the runner's file commands, created new
		if err != nil {
			return nil, fmt.Errorf("create the file command: %w", err)
		}

		return file, nil
	}

	if err != nil {
		return nil, errors.New("check the file command: its path cannot be checked")
	}

	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // G304: one of the runner's file commands, checked as the regular file Lstat saw
	if err != nil {
		return nil, fmt.Errorf("open the file command: %w", err)
	}

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()

		return nil, errors.New("open the file command: it is not the entry it was")
	}

	return file, nil
}

// RecordFileCommands records the file commands at paths before the steps. It
// follows no link: each path is Lstat'ed, and only a regular file is opened,
// its descriptor checked to be the file Lstat saw. A path that does not exist
// is recorded as absent. One that exists but is no regular file, or that
// cannot be opened or read, fails the record (fail closed, the auditor's
// round-4 L1): the run could not tell after the steps whether they changed
// it. On an error it closes what it opened and returns no FileCommands.
func RecordFileCommands(paths []FileCommandPath) (*FileCommands, error) {
	commands := &FileCommands{commands: make([]*fileCommand, 0, len(paths))}

	for _, path := range paths {
		command, err := record(path)
		if err != nil {
			commands.Close()

			return nil, fmt.Errorf("record the file command %s: %w", path.Name, err)
		}

		commands.commands = append(commands.commands, command)
	}

	return commands, nil
}

// record records one file command.
func record(path FileCommandPath) (*fileCommand, error) {
	command := &fileCommand{FileCommandPath: path, expected: sha256.New()}

	info, err := os.Lstat(path.Path)
	if errors.Is(err, fs.ErrNotExist) {
		command.absent = true

		return command, nil
	}

	if err != nil {
		return nil, errors.New("its path cannot be checked")
	}

	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}

	command.before = info

	file, err := openRegular(path.Path, info)
	if err != nil {
		return nil, err
	}

	size, err := hashContent(command.expected, file)
	if err != nil {
		_ = file.Close()

		return nil, err
	}

	command.file = file
	command.overCap = size > maxFileCommandBytes

	return command, nil
}

// openRegular opens the regular file Lstat saw as info at path, for reading
// and writing when it may, else for reading, and checks the descriptor is
// that same file (os.SameFile), never a link's target.
func openRegular(path string, info fs.FileInfo) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // G304: one of the runner's file commands, checked as the regular file Lstat saw
	if err != nil {
		file, err = os.Open(path) //nolint:gosec // G304: as above, for reading only
		if err != nil {
			return nil, fmt.Errorf("open a file command: %w", err)
		}
	}

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()

		return nil, errors.New("a file command is not the entry it was")
	}

	return file, nil
}

// hashContent writes file's content, read from its start through the
// descriptor, at most maxFileCommandBytes+1 bytes, to h, and returns how many
// bytes it read.
func hashContent(h io.Writer, file *os.File) (int64, error) {
	size, err := io.Copy(h, io.NewSectionReader(file, 0, maxFileCommandBytes+1))
	if err != nil {
		return 0, errNotRecorded
	}

	return size, nil
}

// Appended returns the writer the run's own bytes to the file command name go
// through as well: what it is handed is what the file must hold after its
// recorded content. A name that was not recorded with its content takes
// nothing.
func (f *FileCommands) Appended(name string) io.Writer {
	for _, command := range f.commands {
		if command.Name == name && command.file != nil {
			return command.expected
		}
	}

	return io.Discard
}

// Repair checks each file command after the steps and leaves every one they
// changed as an empty regular file (issues #615, #614). A file command is
// changed when its path no longer names the entry recorded before the steps
// (Lstat, os.SameFile; a path that was absent and now exists included), or,
// for a regular file recorded with its content, when it no longer holds
// exactly that content followed by what the run appended. A file changed in
// place is truncated through its descriptor; a path swapped, removed or
// replaced gets a new empty file, written in the same directory and renamed
// over it, so the swap is atomic and no link is followed. Each repair is
// verified with an Lstat of the path. A path a directory replaced is removed
// first when that directory is empty; a non-empty one is left as it is, and
// its repair fails.
//
// The run's own summary bytes reach Appended only after the summary file took
// them whole (ci/auditrun's io.MultiWriter stops at the first writer that
// fails): a write that failed partway, or was short, leaves bytes in the file
// Appended never got, so it counts as a change too and the summary is emptied
// (fail closed); the note then names the steps as what changed it, though the
// run's own write did. A write that left nothing leaves the file as recorded.
//
// Repair tries every changed file command, whatever failed before it (the
// round-4 should-fix): one it cannot empty never leaves another, such as a
// $GITHUB_PATH line, for the runner to apply. It returns the names it emptied,
// in order, and an error joining (errors.Join) one *RepairError per file
// command it could not empty or verify; RepairFailures lists them.
func (f *FileCommands) Repair() ([]string, error) {
	var (
		emptied  []string
		failures []error
	)

	for _, command := range f.commands {
		changed, inPlace := command.changed()
		if !changed {
			continue
		}

		if err := command.empty(inPlace); err != nil {
			failures = append(failures, &RepairError{Name: command.Name, Err: err})

			continue
		}

		emptied = append(emptied, command.Name)
	}

	return emptied, errors.Join(failures...)
}

// RepairError is one file command Repair could not empty: the variable that
// names it and why.
type RepairError struct {
	Name string
	Err  error
}

// Error names the file command and the failure.
func (e *RepairError) Error() string {
	return fmt.Sprintf("empty %s: %v", e.Name, e.Err)
}

// Unwrap returns the failure.
func (e *RepairError) Unwrap() error {
	return e.Err
}

// RepairFailures lists the *RepairError values in the error Repair returned,
// in the order Repair met them; none for nil.
func RepairFailures(err error) []*RepairError {
	if err == nil {
		return nil
	}

	var errs []error
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	} else {
		errs = []error{err}
	}

	failures := make([]*RepairError, 0, len(errs))

	for _, e := range errs {
		var failure *RepairError
		if errors.As(e, &failure) {
			failures = append(failures, failure)
		}
	}

	return failures
}

// changed reports whether the steps changed the file command, and whether its
// path still names the file recorded, so it can be emptied in place.
func (c *fileCommand) changed() (changed, inPlace bool) {
	now, err := os.Lstat(c.Path)
	if err != nil {
		// Still absent is unchanged; gone, or unreadable, is a change.
		return !c.absent || !errors.Is(err, fs.ErrNotExist), false
	}

	if c.before == nil || !os.SameFile(c.before, now) || c.before.Mode().Type() != now.Mode().Type() {
		return true, false
	}

	opened, err := c.file.Stat()
	if err != nil || !now.Mode().IsRegular() || !os.SameFile(opened, now) {
		return true, false
	}

	if c.overCap {
		return true, true
	}

	got := sha256.New()

	size, err := hashContent(got, c.file)
	if err != nil || size > maxFileCommandBytes {
		return true, true
	}

	return string(got.Sum(nil)) != string(c.expected.Sum(nil)), true
}

// empty leaves the file command's path an empty regular file: truncated
// through its descriptor when inPlace, which falls back to the rename when the
// descriptor cannot truncate, and else replaced.
func (c *fileCommand) empty(inPlace bool) error {
	if inPlace && c.file.Truncate(0) == nil {
		now, err := os.Lstat(c.Path)
		if err != nil {
			return fmt.Errorf("check the emptied file: %w", err)
		}

		opened, err := c.file.Stat()
		if err != nil || !now.Mode().IsRegular() || now.Size() != 0 || !os.SameFile(opened, now) {
			return errors.New("the emptied file is not an empty regular file")
		}

		return nil
	}

	return replaceWithEmpty(c.Path)
}

// replaceWithEmpty writes a new empty file in path's directory and renames it
// over path: rename replaces the entry itself, a link included, never what it
// points at. A directory at path is removed first, which works only when it is
// empty: rename cannot put a file over a directory, and a non-empty one is left
// untouched. No temporary file is left behind, and the result is verified to
// be that empty regular file.
func replaceWithEmpty(path string) error {
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		_ = os.Remove(path)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".auditrun-*")
	if err != nil {
		return fmt.Errorf("create the empty file: %w", err)
	}

	created, statErr := temp.Stat()
	closeErr := temp.Close()

	if statErr != nil || closeErr != nil {
		_ = os.Remove(temp.Name())

		return errors.New("create the empty file: it cannot be checked")
	}

	if err := os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())

		return fmt.Errorf("rename the empty file over the path: %w", err)
	}

	now, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("check the replaced file: %w", err)
	}

	if !now.Mode().IsRegular() || now.Size() != 0 || !os.SameFile(created, now) {
		return errors.New("the replaced file is not the empty regular file the run wrote")
	}

	return nil
}

// Close closes the descriptors RecordFileCommands opened.
func (f *FileCommands) Close() {
	for _, command := range f.commands {
		if command.file != nil {
			_ = command.file.Close()
		}
	}
}
