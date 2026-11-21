package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// This file reads the inventory page's commands from the inventory guide
// (issue #373, item 15): docs/guide/inventory.md is their one source of truth,
// so the page carries no copy of them that could drift from it.

// guideFile is the inventory guide, relative to the module root.
const guideFile = "docs/guide/inventory.md"

// guideSource is one source the inventory page documents: its key, which names
// its section and its program (docs/guide/inventory/<key>.jq), the name its
// button shows, and the guide's two headings whose bash blocks are its
// commands, one to collect the provider's output and one to build the
// inventory from it.
type guideSource struct {
	key, name, collect, build string
}

// guideSources are the three sources, in the order the page lists them and the
// guide documents them.
var guideSources = []guideSource{
	{key: "aws", name: "AWS", collect: "Collect from AWS", build: "Build the AWS inventory"},
	{key: "gcp", name: "GCP", collect: "Collect from GCP", build: "Build the GCP inventory"},
	{key: "azure", name: "Azure", collect: "Collect from Azure", build: "Build the Azure inventory"},
}

// sourceData is one source as the inventory page shows it: the commands of its
// two guide sections, each one command a visitor copies on its own.
type sourceData struct {
	Key             string
	Name            string
	CollectCommands []string
	BuildCommands   []string
}

// sourcesData reads each source's commands from the guide under root, by the
// source's key, failing on a section that holds no command: a page without
// them would tell a visitor nothing. The page writes each source's section by
// hand, so its buttons name sections the source page holds, and takes the
// commands with {{with index .Sources "<key>"}}.
func sourcesData(root string) (map[string]sourceData, error) {
	data, err := readIn(root, filepath.FromSlash(guideFile))
	if err != nil {
		return nil, fmt.Errorf("read the inventory guide: %w", err)
	}

	guide := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	out := make(map[string]sourceData, len(guideSources))

	for _, s := range guideSources {
		collect, err := sectionCommands(guide, s.collect)
		if err != nil {
			return nil, err
		}

		build, err := sectionCommands(guide, s.build)
		if err != nil {
			return nil, err
		}

		out[s.key] = sourceData{Key: s.key, Name: s.name, CollectCommands: collect, BuildCommands: build}
	}

	return out, nil
}

// fence is the marker that opens and closes a fenced block of the guide.
const fence = "```"

// sectionCommands returns the commands of the ```bash blocks under the guide's
// heading, up to the next heading, in the guide's order. Every fenced block is
// tracked, whatever its info string, so a line starting with # inside one is
// text and no heading. It fails, naming the heading, when the guide holds the
// heading never or twice, when a fenced block under it is not bash or is left
// open, and when the section yields no command: each would ship a page that
// silently lost commands.
func sectionCommands(guide []string, heading string) ([]string, error) {
	start, err := headingLine(guide, heading)
	if err != nil {
		return nil, err
	}

	var (
		commands []string
		block    []string
		inBlock  bool
	)

	for _, line := range guide[start+1:] {
		trimmed := strings.TrimSpace(line)

		switch {
		case inBlock && trimmed == fence:
			inBlock = false
			commands = append(commands, splitCommands(block)...)
		case inBlock && strings.HasPrefix(trimmed, fence):
			// An opening fence inside a block: the block before it was never closed.
			return nil, fmt.Errorf("the inventory guide %s leaves a bash block open under %q", guideFile, heading)
		case inBlock:
			block = append(block, line)
		case strings.HasPrefix(trimmed, fence):
			if info := strings.TrimSpace(strings.TrimPrefix(trimmed, fence)); info != "bash" {
				return nil, fmt.Errorf("the inventory guide %s holds a fenced block with info string %q under %q, want only bash blocks: the page would drop it",
					guideFile, info, heading)
			}

			inBlock = true
			block = nil
		case headingText(line) != "":
			return finish(commands, heading)
		}
	}

	if inBlock {
		return nil, fmt.Errorf("the inventory guide %s leaves a bash block open under %q", guideFile, heading)
	}

	return finish(commands, heading)
}

// finish returns a section's commands, failing when it holds none.
func finish(commands []string, heading string) ([]string, error) {
	if len(commands) == 0 {
		return nil, fmt.Errorf("the inventory guide %s holds no bash command under %q", guideFile, heading)
	}

	return commands, nil
}

// headingLine returns the index of the guide's one line that is the heading,
// outside every fenced block, failing when it holds none or more than one.
func headingLine(guide []string, heading string) (int, error) {
	found := -1
	inBlock := false

	for i, line := range guide {
		trimmed := strings.TrimSpace(line)

		switch {
		case inBlock:
			inBlock = trimmed != fence
		case strings.HasPrefix(trimmed, fence):
			inBlock = true
		case headingText(line) == heading:
			if found >= 0 {
				return -1, fmt.Errorf("the inventory guide %s holds the heading %q twice, on lines %d and %d",
					guideFile, heading, found+1, i+1)
			}

			found = i
		}
	}

	if found < 0 {
		return -1, fmt.Errorf("the inventory guide %s has no heading %q", guideFile, heading)
	}

	return found, nil
}

// headingText returns the words of a Markdown heading line, without its #s, or
// "" for a line that is no heading: one to six #s, then a space or the line's
// end.
func headingText(line string) string {
	trimmed := strings.TrimSpace(line)
	words := strings.TrimLeft(trimmed, "#")

	if level := len(trimmed) - len(words); level == 0 || level > 6 {
		return ""
	}

	if words != "" && words[0] != ' ' && words[0] != '\t' {
		return ""
	}

	return strings.TrimSpace(words)
}

// splitCommands splits one bash block into the commands a visitor copies, each
// copied on its own: a blank line ends a command that holds code, and a
// comment line starts a new one after code, so a comment is copied with the
// command it describes and never forms a command of its own: a run of comments
// with no code line is dropped, so a block of comments only yields none. Each line keeps
// its text but its trailing white space; a command's leading and trailing
// blank lines are dropped.
func splitCommands(block []string) []string {
	var (
		commands []string
		current  []string
	)

	hasCode := func() bool {
		for _, line := range current {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				return true
			}
		}

		return false
	}

	flush := func() {
		for len(current) > 0 && strings.TrimSpace(current[0]) == "" {
			current = current[1:]
		}

		for len(current) > 0 && strings.TrimSpace(current[len(current)-1]) == "" {
			current = current[:len(current)-1]
		}

		if hasCode() {
			commands = append(commands, strings.Join(current, "\n"))
		}

		current = nil
	}

	for _, line := range block {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			if hasCode() {
				flush()
			}
		case strings.HasPrefix(trimmed, "#"):
			if hasCode() {
				flush()
			}

			current = append(current, line)
		default:
			current = append(current, line)
		}
	}

	flush()

	return commands
}
