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

// sectionCommands returns the commands of the ```bash blocks under the guide's
// heading, up to the next heading, in the guide's order.
func sectionCommands(guide []string, heading string) ([]string, error) {
	start := -1

	for i, line := range guide {
		if headingText(line) == heading {
			start = i + 1

			break
		}
	}

	if start < 0 {
		return nil, fmt.Errorf("the inventory guide %s has no heading %q", guideFile, heading)
	}

	var (
		commands []string
		block    []string
		inBlock  bool
	)

	for _, line := range guide[start:] {
		trimmed := strings.TrimSpace(line)

		if !inBlock && strings.HasPrefix(trimmed, "#") {
			break
		}

		switch {
		case !inBlock && trimmed == "```bash":
			inBlock = true
			block = nil
		case inBlock && trimmed == "```":
			inBlock = false
			commands = append(commands, splitCommands(block)...)
		case inBlock:
			block = append(block, line)
		}
	}

	if inBlock {
		return nil, fmt.Errorf("the inventory guide %s leaves a bash block open under %q", guideFile, heading)
	}

	if len(commands) == 0 {
		return nil, fmt.Errorf("the inventory guide %s holds no bash command under %q", guideFile, heading)
	}

	return commands, nil
}

// headingText returns the words of a Markdown heading line, without its #s, or
// "" for a line that is no heading.
func headingText(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return ""
	}

	return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
}

// splitCommands splits one bash block into the commands a visitor copies, each
// copied on its own: a blank line ends a command that holds code, and a
// comment line starts a new one after code, so a comment is copied with the
// command it describes and never forms a command of its own. Each line keeps
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

		if len(current) > 0 {
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
