// Command shift2eu stands for the CLI composition root, for
// TestCLITestImportersProbe: its production file imports the CLI, which
// cli-only-in-named-tests does not judge, so no want comment.
package main

import _ "github.com/timothymamo/shift2eu/internal/cli"

func main() {}
