package htmlreport

import (
	"context"
	"fmt"
	"io"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
)

// errNotImplemented is what the skeleton's Report and ReportComparison return.
var errNotImplemented = fmt.Errorf("html report: not implemented")

// Reporter renders an assessment, or a comparison of several targets, as an
// HTML fragment. It holds no state, so one Reporter may render any number of
// reports, concurrently too.
type Reporter struct{}

// New returns an HTML reporter.
func New() *Reporter {
	return &Reporter{}
}

// Report writes the HTML fragment of a and meta to w, laid out as the package
// documentation describes.
//
// The output depends on a and meta only. Report builds the whole fragment
// before writing it to w in one call, so w never receives a half-rendered
// fragment from a failure while rendering; a write error is returned, wrapped.
// When ctx is already done Report writes nothing and returns ctx.Err().
func (*Reporter) Report(ctx context.Context, _ io.Writer, _ assessment.Assessment, _ analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return errNotImplemented
}

// ReportComparison writes the HTML fragment of the comparison c and meta to w,
// laid out as the package documentation describes.
//
// The output depends on c and meta only. ReportComparison builds the whole
// fragment before writing it to w in one call; a write error is returned,
// wrapped. A comparison markdown.Reporter refuses (no target, targets that
// assess different resources, a shared blocker naming a target not compared)
// is refused too, and nothing is written. When ctx is already done
// ReportComparison writes nothing and returns ctx.Err().
func (*Reporter) ReportComparison(ctx context.Context, _ io.Writer, _ assessment.Comparison, _ analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return errNotImplemented
}
