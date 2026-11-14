package jsonreport

import (
	"context"
	"fmt"
	"io"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
)

// comparisonDocument is the document of several targets, kind "comparison".
type comparisonDocument struct {
	SchemaVersion  int             `json:"schemaVersion"`
	Kind           string          `json:"kind"`
	InventoryName  string          `json:"inventoryName"`
	Source         string          `json:"source"`
	Targets        []targetEntry   `json:"targets"`
	SharedBlockers []sharedBlocker `json:"sharedBlockers"`
}

// targetEntry is one target of a comparison: its key and the target, summary,
// resources, blockers, workloads, estimate basis and source cost of its
// assessment, as a single-target document holds them.
type targetEntry struct {
	Key    string `json:"key"`
	Target target `json:"target"`
	// Region is the region chosen for the target, written only when one was
	// chosen, with or without estimates.
	Region    string     `json:"region,omitempty"`
	Summary   summary    `json:"summary"`
	Resources []resource `json:"resources"`
	Blockers  []blocker  `json:"blockers"`
	Workloads []workload `json:"workloads"`
	// Estimate and SourceCost are written only when the target's assessment
	// has estimates.
	Estimate   *estimateBasis `json:"estimate,omitempty"`
	SourceCost *sourceCost    `json:"sourceCost,omitempty"`
}

// sharedBlocker is a resource blocked on every target, with each target's
// reasons.
type sharedBlocker struct {
	ResourceID string          `json:"resourceId"`
	Targets    []targetReasons `json:"targets"`
}

// targetReasons is one target's reasons for a shared blocker.
type targetReasons struct {
	Key     string   `json:"key"`
	Reasons []reason `json:"reasons"`
}

// ReportComparison writes the JSON comparison document of c and meta to w, as
// the package documentation describes: one entry per target in the
// comparison's order, then the blockers every target shares.
//
// The output depends on c and meta only. ReportComparison encodes the whole
// document before writing it to w in one call; a write error is returned,
// wrapped. When ctx is already done it writes nothing and returns ctx.Err(). A
// comparison of no target, or one that fails assessment.Comparison.Validate,
// is an error, and nothing is written.
func (*Reporter) ReportComparison(ctx context.Context, w io.Writer, c assessment.Comparison, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(c.Targets) == 0 {
		return fmt.Errorf("render json comparison: the comparison holds no target")
	}

	if err := c.Validate(); err != nil {
		return fmt.Errorf("render json comparison: %w", err)
	}

	targets := make([]targetEntry, 0, len(c.Targets))
	for _, t := range c.Targets {
		targets = append(targets, targetEntry{
			Key:        t.Key,
			Target:     newTarget(t.Assessment),
			Region:     t.Assessment.Region,
			Summary:    newSummary(t.Assessment.Summary),
			Resources:  newResources(t.Assessment.Verdicts),
			Blockers:   newBlockers(t.Assessment.Blockers),
			Workloads:  newWorkloads(t.Assessment.Workloads),
			Estimate:   newEstimateBasis(t.Assessment),
			SourceCost: newAssessmentSourceCost(t.Assessment),
		})
	}

	shared := make([]sharedBlocker, 0, len(c.SharedBlockers))
	for _, b := range c.SharedBlockers {
		reasons := make([]targetReasons, 0, len(b.Targets))
		for _, t := range b.Targets {
			reasons = append(reasons, targetReasons{Key: t.Key, Reasons: newReasons(t.Reasons)})
		}

		shared = append(shared, sharedBlocker{ResourceID: b.ResourceID, Targets: reasons})
	}

	doc := comparisonDocument{
		SchemaVersion:  schemaVersion,
		Kind:           kindComparison,
		InventoryName:  meta.InventoryName,
		Source:         c.Targets[0].Assessment.Source,
		Targets:        targets,
		SharedBlockers: shared,
	}

	return write(w, doc, "json comparison report")
}
