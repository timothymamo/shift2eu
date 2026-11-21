// The composition root: adapters are constructed here, so an adapter import is
// not reported by adapters-are-built-in-cmd. That holds for the package at the
// root of an adapter subtree too, which the rule must name with an entry of its
// own (issue #119).
package main

import (
	"github.com/timothymamo/shift2eu/internal/adapter/report"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
)

func main() {
	_ = report.Name + markdown.Name +
		source.Name + inventoryfile.Name + target.Name + ovhcloud.Name
}
