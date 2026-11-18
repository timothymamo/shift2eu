// Command aireview is the AI reviewer's entry point (PRD #161 milestone 4, ADR
// 0033), run by the ai-review workflow from main's checkout: it reads its
// settings from the environment (review.SettingsFromEnv, through os.LookupEnv
// alone), reads main's CLAUDE.md and review skills from the working directory,
// wires the GitHub and Anthropic clients to their production base URLs, which
// are constants and never read from the environment, and runs review.Run. It
// exits 0 after posting a review and 1 on any failure, with one workflow error
// annotation whose text is escaped, so no text of the pull request, the model
// or an API can become a workflow command. It prints no credential.
//
// It is no part of the product: a main package, which nothing can import, built
// by no release (.goreleaser.yaml and the Dockerfile build cmd/ only).
package main
