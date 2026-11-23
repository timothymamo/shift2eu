//go:build !unix

// Holds the empty half of the layout's table of cases for every target that is
// no Unix host: the FIFO and the socket, the two entry types only a Unix file
// system lays down, are in layout_unix_test.go (issue #277, round D finding 3),
// and make lint type-checks this package for windows and js/wasm too.
package audit_test

// unixLayoutCases is empty here: a Windows or js/wasm host can lay neither a
// FIFO nor a unix socket down, so the layout's table has no case of its own to
// add on this target.
var unixLayoutCases []layoutCase
