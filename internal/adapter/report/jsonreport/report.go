package jsonreport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// schemaVersion is the version of the JSON report format this package writes,
// the schemaVersion of every document (docs/adr/0018, docs/adr/0030).
const schemaVersion = 2

// The kinds of document the format defines.
const (
	kindAssessment = "assessment"
	kindComparison = "comparison"
)

// dateLayout is how a document shows the catalog's retrieval date: a calendar
// date, as the Markdown report prints it.
const dateLayout = "2006-01-02"

// indent is the indentation of one nesting level of a document.
const indent = "  "

// Reporter renders an assessment, or a comparison of several targets, as a JSON
// document. It holds no state, so one Reporter may render any number of
// documents, concurrently too.
type Reporter struct{}

// New returns a JSON reporter.
func New() *Reporter {
	return &Reporter{}
}

// assessmentDocument is the single-target document, kind "assessment".
type assessmentDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	InventoryName string `json:"inventoryName"`
	Source        string `json:"source"`
	Target        target `json:"target"`
	// Region is the region chosen for the target, written only when one was
	// chosen, with or without estimates.
	Region    string     `json:"region,omitempty"`
	Summary   summary    `json:"summary"`
	Resources []resource `json:"resources"`
	Blockers  []blocker  `json:"blockers"`
	Workloads []workload `json:"workloads"`
	// Estimate and SourceCost are written only when the assessment has
	// estimates.
	Estimate   *estimateBasis `json:"estimate,omitempty"`
	SourceCost *sourceCost    `json:"sourceCost,omitempty"`
}

// target is the target a document assesses against and its catalog snapshot.
type target struct {
	Name     string   `json:"name"`
	Snapshot snapshot `json:"snapshot"`
}

// snapshot is where a catalog was taken from and the UTC date it was retrieved.
type snapshot struct {
	Source    string `json:"source"`
	Retrieved string `json:"retrieved"`
}

// summary counts the resources per fit, and in total.
type summary struct {
	Direct  int `json:"direct"`
	Partial int `json:"partial"`
	None    int `json:"none"`
	Total   int `json:"total"`
}

// resource is the verdict on one resource.
type resource struct {
	ID              string     `json:"id"`
	RawType         string     `json:"rawType"`
	Category        string     `json:"category"`
	Region          string     `json:"region"`
	Fit             string     `json:"fit"`
	BestOffering    string     `json:"bestOffering"`
	Ties            []string   `json:"ties"`
	Reasons         []reason   `json:"reasons"`
	RequiredChanges []string   `json:"requiredChanges"`
	Relations       []relation `json:"relations"`
	AffectedBy      []string   `json:"affectedBy"`
	// Plan is written when the estimate names a plan, and Estimate whenever
	// the assessment has estimates.
	Plan     *plan             `json:"plan,omitempty"`
	Estimate *resourceEstimate `json:"estimate,omitempty"`
}

// reason is one mapping.Reason: its code, its plain sentence, and the names it
// mentions, each present only when the reason sets it.
type reason struct {
	Code       string `json:"code"`
	Text       string `json:"text"`
	Target     string `json:"target,omitempty"`
	Category   string `json:"category,omitempty"`
	Offering   string `json:"offering,omitempty"`
	Capability string `json:"capability,omitempty"`
}

// blocker is one reason a resource cannot move.
type blocker struct {
	ResourceID string `json:"resourceId"`
	Reason     reason `json:"reason"`
}

// Report writes the JSON document of a and meta to w, as the package
// documentation describes.
//
// The output depends on a and meta only. Report encodes the whole document
// before writing it to w in one call; a write error is returned, wrapped. When
// ctx is already done Report writes nothing and returns ctx.Err().
func (*Reporter) Report(ctx context.Context, w io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	doc := assessmentDocument{
		SchemaVersion: schemaVersion,
		Kind:          kindAssessment,
		InventoryName: meta.InventoryName,
		Source:        a.Source,
		Target:        newTarget(a),
		Region:        a.Region,
		Summary:       newSummary(a.Summary),
		Resources:     newResources(a.Verdicts),
		Blockers:      newBlockers(a.Blockers),
		Workloads:     newWorkloads(a.Workloads),
		Estimate:      newEstimateBasis(a),
		SourceCost:    newAssessmentSourceCost(a),
	}

	return write(w, doc, "json report")
}

// write encodes doc, indented and ended by one newline, and writes it to w in
// one call. what names the document in an error. json.MarshalIndent escapes
// <, >, &, U+2028 and U+2029, and writes each invalid UTF-8 byte as U+FFFD, so
// the document is valid JSON and valid UTF-8 whatever the values hold.
func write(w io.Writer, doc any, what string) error {
	data, err := json.MarshalIndent(doc, "", indent)
	if err != nil {
		return fmt.Errorf("encode %s: %w", what, err)
	}

	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}

	return nil
}

// newTarget returns the target and catalog snapshot of a.
func newTarget(a assessment.Assessment) target {
	return target{
		Name: a.Target,
		Snapshot: snapshot{
			Source:    a.Snapshot.SourceURL,
			Retrieved: a.Snapshot.RetrievedOn.UTC().Format(dateLayout),
		},
	}
}

// newSummary returns the counts of s.
func newSummary(s assessment.Summary) summary {
	return summary{Direct: s.Direct, Partial: s.Partial, None: s.None, Total: s.Total}
}

// newResources returns one resource per verdict, in the order given, never nil.
func newResources(verdicts []assessment.Verdict) []resource {
	out := make([]resource, 0, len(verdicts))

	for _, v := range verdicts {
		ties := v.Ties()
		if ties == nil {
			ties = []string{}
		}

		listed := v.ChangesToList()
		changes := make([]string, 0, len(listed))

		for _, c := range listed {
			changes = append(changes, string(c))
		}

		out = append(out, resource{
			ID:              v.Resource.ID,
			RawType:         v.Resource.RawType,
			Category:        string(v.Resource.Category),
			Region:          v.Resource.Region,
			Fit:             string(v.Fit),
			BestOffering:    v.BestOffering,
			Ties:            ties,
			Reasons:         newReasons(v.Reasons),
			RequiredChanges: changes,
			Relations:       newRelations(v),
			AffectedBy:      orEmpty(v.AffectedBy),
			Plan:            newPlan(v.Estimate),
			Estimate:        newResourceEstimate(v.Estimate),
		})
	}

	return out
}

// newReasons returns one reason per mapping.Reason, in the order given, never
// nil.
func newReasons(reasons []mapping.Reason) []reason {
	out := make([]reason, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, newReason(r))
	}

	return out
}

// newReason returns the reason object of r. Its text is the reason's plain
// sentence, mapping.Reason.String: JSON encoding escapes it, so it needs no
// escaping of its own.
func newReason(r mapping.Reason) reason {
	return reason{
		Code:       string(r.Code),
		Text:       r.String(),
		Target:     r.Target,
		Category:   string(r.Category),
		Offering:   r.Offering,
		Capability: string(r.Capability),
	}
}

// newBlockers returns one blocker per assessment.Blocker, in the order given,
// never nil.
func newBlockers(blockers []assessment.Blocker) []blocker {
	out := make([]blocker, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, blocker{ResourceID: b.ResourceID, Reason: newReason(b.Reason)})
	}

	return out
}
