package lint_test

import "testing"

// TestAdaptersBuiltInCmdProbe runs depguard with the repository's
// .golangci.yml on a probe module laid out like the repository
// (testdata/adaptersbuiltincmd/mod). adapters-are-built-in-cmd must refuse an
// import of an adapter under each adapter subtree (report, source, target),
// the package at the root of each of those subtrees, and an import of a
// helper under adapters/internal/ that the rule does not name, with the
// helper's own message. The two helpers the rule names stay allowed, and the
// composition root may import every adapter, the package at a subtree root
// included. A subtree's entries end in "/" and cover only what is under it,
// so the package at its root needs an entry of its own (issue #119). An
// import of each adapter subtree from internal/foo, a package no strict rule
// holds, is refused by adapters-are-built-in-cmd alone, so the probe fails
// if the lax rule stops firing. Every want on the probe is on a
// line the rule must refuse, and every issue must match a want, so a rule that
// stops firing fails here; that is how the shadowing in issue #119 went
// unnoticed. make lint runs this probe with SHIFT2EU_LINT_PROBES=require on
// the host, and every entry of CI's lint-target matrix runs the step that sets
// it.
func TestAdaptersBuiltInCmdProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{dir: "adaptersbuiltincmd", depguard: true})
}
