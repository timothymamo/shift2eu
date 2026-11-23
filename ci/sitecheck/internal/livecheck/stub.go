package livecheck

import "errors"

// errNotImplemented is what every stub returns until the behaviour is
// implemented (the TDD chain of PRD #297 milestone 3, PR B).
var errNotImplemented = errors.New("livecheck: not implemented")

// stub returns errNotImplemented; its arguments are the stub's parameters, so
// the linters see them used until the body is written.
func stub(_ ...any) error {
	return errNotImplemented
}
