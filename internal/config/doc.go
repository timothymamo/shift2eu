// Package config loads and validates runtime configuration (which source
// provider and accounts to analyse, which target providers to compare, output
// format) from flags, environment variables and an optional config file.
//
// It has no code yet. The strict depguard rule config-is-pure holds it to
// errors$ (issue #152), because a composition root may import any module
// package and could otherwise hand it a read it may not make itself. The
// environment read this comment announces needs, in the same change, os in
// that rule's allow list, a forbidigo group that refuses os's environment
// reads here (test/architecture's TestEveryOsAllowingRuleHasAnEnvironmentGroup
// fails without one), and an exception for each named variable, decided in an
// ADR, as ai-reviewer-env's for ci/aireview/main.go (ADR 0033).
package config
