// Command aireview is the AI reviewer's entry point (PRD #161 milestone 4, ADR
// 0033), run by the ai-review workflow from main's checkout as
//
//	aireview <absolute path of the claude CLI>
//
// It reads its settings from the environment (review.SettingsFromEnv, through
// os.LookupEnv alone), reads main's CLAUDE.md and review skills from the
// working directory, wires the GitHub client to its production base URL, a
// constant never read from the environment, and the model to a run of the
// Claude Code CLI at the path it is given, which the workflow installed at a
// pinned version and verified against a pinned checksum. It then runs
// review.Run under a deadline of 15 minutes (runDeadline), inside the
// workflow job's timeout of 20. It exits 0 after posting a review and 1 on any failure, with one
// workflow error annotation whose text is escaped, so no text of the pull
// request, the model or an API can become a workflow command. It prints no
// credential.
//
// A run that ends in the skipped outcome (a diff too large to review, or no
// usable answer from the model twice; issues #367, #376) has posted a review
// saying so: the program prints one ::warning annotation, appends one line to
// the job summary ($GITHUB_STEP_SUMMARY, when set) and exits 0. A job summary
// it cannot write is one more warning, never a failure.
//
// It is no part of the product: a main package, which nothing can import, built
// by no release (.goreleaser.yaml and the Dockerfile build cmd/ only).
package main
