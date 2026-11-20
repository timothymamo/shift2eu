// Package testutil holds helpers shared by tests: golden-file comparison
// (with an -update flag), fixture loading from testdata, small builders for
// domain values, a JSON Schema validator for the tests that hold a document
// to a schema, and a reader of the estimate reasons the domain declares, which
// the report guards hold a report's phrases and order to (issue #308). It must
// only be imported from _test.go files.
//
// The application layer's tests may import it, so what it can do through os
// (file access, the golden files), fmt, time and flag is, beside what testing
// itself offers (t.TempDir, test output, and t.Setenv/t.Chdir until #153), the
// I/O the lint rules leave those tests. Its imports, its own tests' included, are a strict allow
// list (depguard rule testutil-imports in .golangci.yml): a new import needs
// an entry there and in test/architecture's pin. It reads no environment
// variable, locates no home, config, cache or temp directory and starts no
// process (forbidigo group testutil-no-env, CLAUDE.md rule 3): a helper takes
// what it needs as a parameter. Those rules are a guardrail on imports and
// named calls, not a sandbox: they cannot refuse a hard-coded path, so a new
// file path or os, fmt, time or flag call here is a widening a reviewer must
// read.
package testutil
