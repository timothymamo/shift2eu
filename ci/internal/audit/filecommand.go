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

// RecordFileCommands records the file commands at paths before the steps. It
// follows no link: each path is Lstat'ed, and only a regular file is opened,
// its descriptor checked to be the file Lstat saw. A path that does not exist
// is recorded as absent; one that is no regular file, or cannot be opened or
// read, is recorded by its identity alone and named by Unopened.
func RecordFileCommands(paths []FileCommandPath) *FileCommands {
	commands := &FileCommands{commands: make([]*fileCommand, 0, len(paths))}

	for _, path := range paths {
		commands.commands = append(commands.commands, record(path))
	}

	return commands
}

// record records one file command.
func record(path FileCommandPath) *fileCommand {
	command := &fileCommand{FileCommandPath: path, expected: sha256.New()}

	info, err := os.Lstat(path.Path)
	if err != nil {
		command.absent = errors.Is(err, fs.ErrNotExist)

		return command
	}

	command.before = info

	if !info.Mode().IsRegular() {
		return command
	}

	file, err := openRegular(path.Path, info)
	if err != nil {
		return command
	}

	size, err := hashContent(command.expected, file)
	if err != nil {
		_ = file.Close()

		return command
	}

	command.file = file
	command.overCap = size > maxFileCommandBytes

	return command
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

// Unopened names the file commands that existed before the steps but are no
// regular file the run could open and read: their content is not compared,
// only whether their path still names the same entry.
func (f *FileCommands) Unopened() []string {
	var names []string

	for _, command := range f.commands {
		if command.before != nil && command.file == nil {
			names = append(names, command.Name)
		}
	}

	return names
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
// verified with an Lstat of the path. Repair returns the names it emptied, in
// order, and an error when a repair failed or could not be verified; the
// names then hold those emptied before it.
func (f *FileCommands) Repair() ([]string, error) {
	var emptied []string

	for _, command := range f.commands {
		changed, inPlace := command.changed()
		if !changed {
			continue
		}

		if err := command.empty(inPlace); err != nil {
			return emptied, fmt.Errorf("empty %s: %w", command.Name, err)
		}

		emptied = append(emptied, command.Name)
	}

	return emptied, nil
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

	if c.file == nil {
		return false, false
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
// points at. No temporary file is left behind, and the result is verified to
// be that empty regular file.
func replaceWithEmpty(path string) error {
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
