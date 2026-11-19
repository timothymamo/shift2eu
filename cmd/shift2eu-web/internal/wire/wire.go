package wire

import (
	"github.com/timothymamo/shift2eu/internal/adapter/report/htmlreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/web"
)

// Deps returns the adapters of the web adapter, as the package documentation
// lists them. Every call returns a new Deps; constructing it reads nothing:
// a catalog is loaded, and a reporter built, only when a run asks for it.
func Deps() web.Deps {
	return web.Deps{
		NewInventorySource: func(doc []byte) analysis.InventorySource {
			return inventoryfile.FromBytes(doc)
		},
		Targets: map[string]func() analysis.CatalogSource{
			"ovhcloud": func() analysis.CatalogSource { return ovhcloud.New() },
			"scaleway": func() analysis.CatalogSource { return scaleway.New() },
			"upcloud":  func() analysis.CatalogSource { return upcloud.New() },
		},
		Markdown: web.Format{
			NewReporter:           func(int) analysis.Reporter { return markdown.New() },
			NewComparisonReporter: func(int) analysis.ComparisonReporter { return markdown.New() },
		},
		JSON: web.Format{
			NewReporter: func(schemaVersion int) analysis.Reporter {
				return jsonreport.New(schemaVersion)
			},
			NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter {
				return jsonreport.New(schemaVersion)
			},
		},
		HTML: web.Format{
			NewReporter:           func(int) analysis.Reporter { return htmlreport.New() },
			NewComparisonReporter: func(int) analysis.ComparisonReporter { return htmlreport.New() },
		},
	}
}
