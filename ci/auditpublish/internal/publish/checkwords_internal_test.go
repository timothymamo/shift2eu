// This file is in package publish rather than publish_test on purpose: the
// fixed table (checkPhrases), the range's ends (checkNone, checkEnd) and the
// one phrase the six checks of a private finding's draft share
// (privateFindingNotFiled) are unexported. The black-box tests next door drive
// the refusals that print them, one row per refusal site; this file holds the
// table itself, so a check cannot ship without its words (issue #655 point 2).

package publish

import (
	"strings"
	"testing"
)

// TestEveryFailedCheckHasItsWords pins the fixed table as a whole: every
// FailedCheck from the first up to checkEnd has a class and a phrase, and no
// phrase holds a ":". The class decides the exit code, and the phrase is the
// part of the summary line between the two colons, so a phrase holding one
// would pass itself off for another part of the line.
func TestEveryFailedCheckHasItsWords(t *testing.T) {
	t.Parallel()

	for check := checkNone + 1; check < checkEnd; check++ {
		entry := checkPhrases[check]

		if entry.class == nil {
			t.Errorf("FailedCheck(%d) has no class, want the one class it belongs to: the class decides the exit code", check)
		}

		if entry.phrase == "" {
			t.Errorf("FailedCheck(%d) has no phrase, want its fixed words: the summary line names every check", check)

			continue
		}

		if strings.Contains(entry.phrase, ":") {
			t.Errorf("FailedCheck(%d)'s phrase is %q, want no %q: it is the summary line's separator", check, entry.phrase, ":")
		}
	}
}

// TestThePrivateFindingPhraseIsFixedWords pins the one phrase the six checks of
// a private finding's draft advisory share, which the table is not the only
// holder of: used as a phrase it follows the table's rule, fixed words and no
// ":". Its words state that no draft was filed, never which check refused it
// (ADR 0040, "Exit codes and the summary line").
func TestThePrivateFindingPhraseIsFixedWords(t *testing.T) {
	t.Parallel()

	switch {
	case privateFindingNotFiled == "":
		t.Error("privateFindingNotFiled is empty, want the six checks' one phrase")
	case strings.Contains(privateFindingNotFiled, ":"):
		t.Errorf("privateFindingNotFiled is %q, want no %q: it is the summary line's separator", privateFindingNotFiled, ":")
	}
}

// TestNoPhraseIsRunningGH pins issue #655's decision: the phrase "running gh"
// is dropped. OutcomeOf takes the first mark of the outcome's class, so a
// caller's own mark always wins over the client's, and gh's failure is already
// named by the call it failed; a phrase of its own would only ever print for a
// call no caller marks. No check of the table may carry it.
func TestNoPhraseIsRunningGH(t *testing.T) {
	t.Parallel()

	for check := checkNone + 1; check < checkEnd; check++ {
		if phrase := checkPhrases[check].phrase; phrase == "running gh" {
			t.Errorf("FailedCheck(%d)'s phrase is %q, want the check dropped: gh's own failure is named by the call it failed (issue #655)", check, phrase)
		}
	}
}
