// Package container holds plain tests that pin the repository's container build
// files, the Dockerfile and .dockerignore at the repository root, to go.mod and
// .goreleaser.yaml, so each image compiles its binary the way the release does:
// the Go version of go.mod, no cgo, -trimpath, the internal/version ldflags, a
// pinned base image for every stage, a minimal non-root stage per image whose
// base is not the debug variant and that runs no RUN instruction, and a build
// context that holds no inventory, report or git data and keeps every embedded
// file.
//
// There is one image: the last stage, the CLI, shift2eu, which docker build .
// and make image build. The HTTP API's stage, shift2eu-server (PRD #103, ADR
// 0019), is gone with the server (issue #393), and .goreleaser.yaml has the
// CLI's builds entry alone; the tests hold that entry to the CLI's go build.
//
// The tests read the files from the repository root. They need no Docker daemon,
// no network and no credentials, and they carry no build tag, so make test, make
// check and CI's make cover run them. The package has no code of its own.
package container
