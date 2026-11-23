package lint_test

import (
	"fmt"
	"slices"
	"testing"
)

// This file holds the check the two audit publish environment groups share. It
// is no pin of its own: the tables live with each group's test, in
// auditpublishmainenv_test.go and auditpublishgithubenv_test.go.

// forbidGroupRefusal is a name a forbidigo group must refuse, with the msg that
// must match the msg of the entry refusing it: the whole msg of the group's
// exception entry, for the one name that entry is for, and forbidGroupAnyEntry,
// for every other name.
type forbidGroupRefusal struct {
	rootName
	msg string
}

// forbidGroupAnyEntry is the msg a refusal carries when any entry of the group
// but its exceptions must refuse the name: the wording of a broad entry's msg is
// not pinned, only an exception's, which a linters.exclusions.rules rule quotes.
const forbidGroupAnyEntry = ""

// forbidGroupException is the msg of a group entry that a
// linters.exclusions.rules rule narrows on, and the one name of the group's
// lists that entry may refuse. Every other name it refuses passes in the file
// the rule covers, which is the gap it pins: with os.Environ and os.StartProcess
// in one entry, a rule that drops that entry's os.Environ refusal in a test file
// drops its os.StartProcess refusal there too.
type forbidGroupException struct {
	msg  string
	name rootName
}

// checkForbidigoGroupPatterns checks the entries of one forbidigo group, read
// from .golangci.yml with groupEntries, offline and without golangci-lint: every
// name the group must refuse is refused by the entry its refusal names, every
// exception entry refuses its one name and no other listed name, no name the
// group must leave alone is refused by an entry, and every entry refuses a
// listed name. It is the pin a reviewer reads when a group changes: without it an
// entry could be moved in .golangci.yml and lint would stay green.
func checkForbidigoGroupPatterns(t *testing.T, tag string, refused []forbidGroupRefusal, exceptions []forbidGroupException, allowed []rootName) {
	t.Helper()

	entries := groupEntries(t, moduleRoot(t), tag)
	exceptionMsgs := make([]string, 0, len(exceptions))
	for _, x := range exceptions {
		exceptionMsgs = append(exceptionMsgs, x.msg)
	}
	// mustRefuse reports whether e is an entry the refusal r names: the
	// exception entry with its whole msg, or any entry of the group but the
	// exceptions.
	mustRefuse := func(e forbidEntry, r forbidGroupRefusal) bool {
		if !e.refuses(r.rootName) {
			return false
		}
		if r.msg == forbidGroupAnyEntry {
			return !slices.Contains(exceptionMsgs, e.msg)
		}
		return e.msg == r.msg
	}

	for _, r := range refused {
		var msgs []string

		for _, e := range entries {
			if mustRefuse(e, r) {
				msgs = append(msgs, e.msg)
			}
		}

		if len(msgs) == 0 {
			want := fmt.Sprintf("an entry of group %s other than its exceptions", tag)
			if r.msg != forbidGroupAnyEntry {
				want = fmt.Sprintf("the entry with msg %q", r.msg)
			}
			t.Errorf("%s (pkg %q) must be refused by %s, but no such entry refuses it; the entries that refuse it have msgs %q", r.name, r.pkg, want, refusedBy(entries, r.rootName))
		}
	}

	names := make([]rootName, 0, len(refused)+len(allowed))
	for _, r := range refused {
		names = append(names, r.rootName)
	}
	names = append(names, allowed...)

	for _, x := range exceptions {
		var found bool

		for _, e := range entries {
			if e.msg != x.msg {
				continue
			}
			found = true
			if !e.refuses(x.name) {
				t.Errorf("the entry with msg %q must refuse %s (pkg %q), the one name its exclusion rule is for", x.msg, x.name.name, x.name.pkg)
			}
			for _, n := range names {
				if n == x.name || !e.refuses(n) {
					continue
				}
				t.Errorf("the entry with msg %q must refuse %s (pkg %q) alone, but it also refuses %s (pkg %q); the exclusion rule narrows on its msg, so that name would pass in the file the rule covers", x.msg, x.name.name, x.name.pkg, n.name, n.pkg)
			}
		}

		if !found {
			t.Errorf(".golangci.yml holds no %s entry with msg %q, the one an exclusion rule narrows on", tag, x.msg)
		}
	}

	for _, n := range allowed {
		for _, e := range entries {
			if e.refuses(n) {
				t.Errorf("%s (pkg %q) must pass, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
			}
		}
	}

	for _, e := range entries {
		if !slices.ContainsFunc(refused, func(r forbidGroupRefusal) bool { return e.refuses(r.rootName) }) {
			t.Errorf("the entry with msg %q refuses no name of the group's list; add the names it refuses", e.msg)
		}
	}
}

// forbidGroupEntry is one entry of a group whose linters.exclusions.rules never
// narrow on an entry's msg, named by that msg and the package path its pkg
// matches, with the names its pattern must refuse. The msg alone names no entry:
// composition-root-env's entry for os.Getenv and its entry for syscall.Getenv
// carry one msg and differ in their pkg, which is why the pkg is part of the
// name.
type forbidGroupEntry struct {
	msg, pkg string
	refuses  []rootName
}

// checkForbidigoGroupEntries checks the entries of one forbidigo group, read from
// .golangci.yml with groupEntries, offline and without golangci-lint, entry by
// entry: the entry named by each pin refuses every name the pin lists and no
// other listed name of the group's table, no entry refuses a name of allowed,
// and every entry of the group is pinned. It is the pin for a group no exclusion
// rule narrows on (compositionRootRuleProblems), where the name sets are the
// only place that says which entry refuses what: a name moved from one entry's
// pattern into another's fails here, as does one an entry stops refusing.
func checkForbidigoGroupEntries(t *testing.T, tag string, pinned []forbidGroupEntry, allowed []rootName) {
	t.Helper()

	entries := groupEntries(t, moduleRoot(t), tag)
	names := slices.Clone(allowed)
	for _, p := range pinned {
		names = append(names, p.refuses...)
	}

	for _, p := range pinned {
		var entry *forbidEntry
		found := 0
		for i := range entries {
			if entries[i].msg == p.msg && entries[i].pkg.MatchString(p.pkg) {
				entry = &entries[i]
				found++
			}
		}
		switch found {
		case 0:
			t.Errorf(".golangci.yml holds no %s entry that matches pkg %q with msg %q", tag, p.pkg, p.msg)
			continue
		case 1:
		default:
			t.Errorf(".golangci.yml holds %d %s entries that match pkg %q with msg %q; the pin names no one entry", found, tag, p.pkg, p.msg)
			continue
		}

		for _, n := range names {
			refuses, must := entry.refuses(n), slices.Contains(p.refuses, n)
			if must && !refuses {
				t.Errorf("the entry with msg %q that matches pkg %q must refuse %s (pkg %q), and it does not", p.msg, p.pkg, n.name, n.pkg)
			}
			if !must && refuses {
				t.Errorf("the entry with msg %q that matches pkg %q must refuse no other name of the group, but it refuses %s (pkg %q); a name moved into it from another entry of the group passes where that entry's pattern does not", p.msg, p.pkg, n.name, n.pkg)
			}
		}
	}

	for _, n := range allowed {
		for _, e := range entries {
			if e.refuses(n) {
				t.Errorf("%s (pkg %q) must pass, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
			}
		}
	}

	for _, e := range entries {
		if !slices.ContainsFunc(pinned, func(p forbidGroupEntry) bool { return p.msg == e.msg && e.pkg.MatchString(p.pkg) }) {
			t.Errorf("the entry with msg %q and pkg %q is not pinned; add the names its pattern refuses to the group's table", e.msg, e.pkg)
		}
	}
}

// refuses reports whether e matches n: both its pkg and its pattern. It is the
// one place the package reads a group entry's match, so that the refusal a test
// checks and the refusal it reports can never disagree.
func (e forbidEntry) refuses(n rootName) bool {
	return e.pkg.MatchString(n.pkg) && e.pattern.MatchString(n.name)
}

// refusedBy returns the msgs of the entries of entries that refuse n, for the
// failure message of a name no entry the refusal names refuses.
func refusedBy(entries []forbidEntry, n rootName) []string {
	var msgs []string

	for _, e := range entries {
		if e.refuses(n) {
			msgs = append(msgs, e.msg)
		}
	}

	return msgs
}
