package markdown

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// dateLayout is how a report shows the catalog's retrieval date: a calendar date.
const dateLayout = "2006-01-02"

// reasonSeparator joins the reasons of one resource in its table cell.
const reasonSeparator = "; "

// The heading markers of a report's sections: sectionHeading in a single-target
// report and for a comparison's own sections, subsectionHeading for the sections
// inside them, such as a target's summary in a comparison, one level lower.
const (
	sectionHeading    = "##"
	subsectionHeading = "###"
)

// nameSeparator joins names in one line or cell: tied offerings and required
// changes.
const nameSeparator = ", "

// Reporter renders an assessment as a Markdown report. It holds no state, so
// one Reporter may render any number of reports, concurrently too.
type Reporter struct{}

// New returns a Markdown reporter.
func New() *Reporter {
	return &Reporter{}
}

// Report writes the Markdown report of a and meta to w, laid out as the package
// documentation describes.
//
// The output depends on a and meta only. Report builds the whole report before
// writing it to w in one call, so w never receives a half-rendered report from a
// failure while rendering; a write error is returned, wrapped.
// When ctx is already done Report writes nothing and returns ctx.Err().
func (*Reporter) Report(ctx context.Context, w io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var b strings.Builder

	writeHeader(&b, a, meta)
	writeBody(&b, a, sectionHeading)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write markdown report: %w", err)
	}

	return nil
}

// writeHeader writes the title and the lines that say what was assessed against
// what. The inventory file line is left out when the inventory has no file name.
func writeHeader(b *strings.Builder, a assessment.Assessment, meta analysis.ReportMeta) {
	target := codeSpan(a.Target, false)

	b.WriteString("# Migration assessment: " + target + "\n\n")

	if meta.InventoryName != "" {
		b.WriteString("- Inventory file: " + codeSpan(meta.InventoryName, false) + "\n")
	}

	b.WriteString("- Source: " + codeSpan(a.Source, false) + "\n")
	b.WriteString("- Catalog: " + target + " snapshot from " + codeSpan(a.Snapshot.SourceURL, false) +
		", retrieved " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout) + "\n")

	if region := renderRegion(a); region != "" {
		b.WriteString("- Region: " + region + "\n")
	}

	b.WriteString("\n")
}

// writeBody writes the sections of the report of a: its summary, resources,
// required changes and blockers, then the per-workload section when
// a.ShowWorkloads and the cost summary when a has estimates, each headed by the
// heading marker heading and the section's name. It ends with a single newline.
func writeBody(b *strings.Builder, a assessment.Assessment, heading string) {
	writeSummary(b, a.Summary, heading)
	writeResources(b, a, heading)
	writeRequiredChanges(b, a.Verdicts, heading)
	writeBlockers(b, a.Blockers, a.Verdicts, heading)

	if a.ShowWorkloads() {
		writeWorkloads(b, a, heading)
	}

	if hasEstimates(a) {
		writeCostEstimate(b, a, heading)
	}
}

// writeSummary writes the number of resources per fit, and in total.
func writeSummary(b *strings.Builder, s assessment.Summary, heading string) {
	b.WriteString(heading + " Summary\n\n")
	writeRow(b, "Fit", "Resources")
	writeSeparator(b, 2)
	writeRow(b, string(mapping.Direct), strconv.Itoa(s.Direct))
	writeRow(b, string(mapping.Partial), strconv.Itoa(s.Partial))
	writeRow(b, string(mapping.None), strconv.Itoa(s.None))
	writeRow(b, "total", strconv.Itoa(s.Total))
	b.WriteString("\n")
}

// writeResources writes one table row per verdict of a, in its order. The Plan
// and Monthly estimate columns are there only when a has estimates, and the
// Relations column only when some resource has a relation, so a report without
// either has the same table as before they existed.
func writeResources(b *strings.Builder, a assessment.Assessment, heading string) {
	estimates := hasEstimates(a)
	relations := hasRelations(a.Verdicts)

	header := []string{"ID", "Raw type", "Category", "Fit", "Best offering"}
	if estimates {
		header = append(header, "Plan", "Monthly estimate")
	}

	if relations {
		header = append(header, "Relations")
	}

	header = append(header, "Reasons")

	b.WriteString(heading + " Resources\n\n")
	writeRow(b, header...)
	writeSeparator(b, len(header))

	for _, v := range a.Verdicts {
		row := []string{
			codeSpan(v.Resource.ID, true),
			codeSpan(v.Resource.RawType, true),
			codeSpan(string(v.Resource.Category), true),
			escapeText(string(v.Fit)),
			renderBestOffering(v),
		}

		if estimates {
			row = append(row, renderPlan(v.Estimate), renderEstimate(v.Estimate))
		}

		if relations {
			row = append(row, renderRelations(v))
		}

		row = append(row, renderReasons(v.Reasons, true))
		writeRow(b, row...)
	}

	b.WriteString("\n")
}

// writeRequiredChanges writes one list item per partial verdict that has
// required changes, in the order given: the capabilities the best offering
// lacks, and that offering. It writes "None." when there is no such verdict.
func writeRequiredChanges(b *strings.Builder, verdicts []assessment.Verdict, heading string) {
	b.WriteString(heading + " Required changes\n\n")

	items := 0

	for _, v := range verdicts {
		listed := v.ChangesToList()
		if len(listed) == 0 {
			continue
		}

		changes := make([]string, 0, len(listed))
		for _, c := range listed {
			changes = append(changes, codeSpan(string(c), false))
		}

		b.WriteString("- " + codeSpan(v.Resource.ID, false) + ": " + strings.Join(changes, nameSeparator) +
			" (not offered by " + codeSpan(v.BestOffering, false) + ")\n")

		items++
	}

	if items == 0 {
		b.WriteString("None.\n")
	}

	b.WriteString("\n")
}

// writeBlockers writes one list item per blocker, in the order given, then one
// item per "affected by" note of the verdicts (assessment.Verdict.AffectedBy), in
// verdict order, or "None." when there is no blocker. It ends with a single
// newline.
func writeBlockers(b *strings.Builder, blockers []assessment.Blocker, verdicts []assessment.Verdict, heading string) {
	b.WriteString(heading + " Blockers\n\n")

	if len(blockers) == 0 {
		b.WriteString("None.\n")

		return
	}

	writeBlockerItems(b, blockers, "")

	for _, v := range verdicts {
		for _, id := range v.AffectedBy {
			b.WriteString("- " + codeSpan(v.Resource.ID, false) + ": affected by a blocker on " +
				codeSpan(id, false) + "\n")
		}
	}
}

// writeBlockerItems writes one list item per blocker, "ID: reason", each line
// starting with indent.
func writeBlockerItems(b *strings.Builder, blockers []assessment.Blocker, indent string) {
	for _, blocker := range blockers {
		b.WriteString(indent + "- " + codeSpan(blocker.ResourceID, false) + ": " +
			renderReason(blocker.Reason, false) + "\n")
	}
}

// writeRow writes one table row of cells that are already escaped for a table.
func writeRow(b *strings.Builder, cells ...string) {
	b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
}

// writeSeparator writes the row that separates a table's header from its body.
func writeSeparator(b *strings.Builder, columns int) {
	b.WriteString("|" + strings.Repeat("---|", columns) + "\n")
}

// renderBestOffering renders the best-offering cell of v: the best offering's
// name, followed by "(tie: ...)" with the other candidates a reader could take
// for it, those with the best candidate's fit and as many missing capabilities,
// in candidate order. The best candidate is the first one.
func renderBestOffering(v assessment.Verdict) string {
	best := codeSpan(v.BestOffering, true)

	names := v.Ties()
	if len(names) == 0 {
		return best
	}

	ties := make([]string, 0, len(names))
	for _, name := range names {
		ties = append(ties, codeSpan(name, true))
	}

	return best + " (tie: " + strings.Join(ties, nameSeparator) + ")"
}

// renderReasons renders reasons one after the other, separated by "; ".
func renderReasons(reasons []mapping.Reason, inTable bool) string {
	rendered := make([]string, 0, len(reasons))
	for _, r := range reasons {
		rendered = append(rendered, renderReason(r, inTable))
	}

	return strings.Join(rendered, reasonSeparator)
}

// renderReason renders a reason from its parts: literal sentence text as escaped
// text, every other part, a name, as a code span. It never uses the reason's
// String method, which is not safe for Markdown.
func renderReason(r mapping.Reason, inTable bool) string {
	var b strings.Builder

	for _, p := range r.Parts() {
		if p.Kind == mapping.PartLiteral {
			b.WriteString(escapeText(p.Text))

			continue
		}

		// PartTarget, PartOffering, PartCategory, PartCapability, and any kind
		// added later: a value is shown literally whatever it is.
		b.WriteString(codeSpan(p.Text, inTable))
	}

	return b.String()
}

// textEscaper puts a backslash before every character that could start Markdown
// syntax in running text or a table cell: emphasis, code, links, images, HTML,
// headings, strikethrough, entity references and cell boundaries. Dots are left
// alone: they start no syntax in the middle of a line. Line endings become
// character references, so no text can end a line, a table row or a heading.
var textEscaper = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"[", `\[`,
	"]", `\]`,
	"(", `\(`,
	")", `\)`,
	"<", `\<`,
	">", `\>`,
	"|", `\|`,
	"!", `\!`,
	"~", `\~`,
	"#", `\#`,
	"&", `\&`,
	"\n", "&#10;",
	"\r", "&#13;",
)

// escapeText returns s escaped for Markdown running text and table cells. It is
// for the report's own words only, literal reason text and fits; a value from
// the assessment or the metadata is a code span.
func escapeText(s string) string {
	return textEscaper.Replace(s)
}

// codeSpanLineEndings turns line endings into spaces, as CommonMark does inside a
// code span, so a value cannot end the line its code span is on.
var codeSpanLineEndings = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// codeSpan returns s as a Markdown code span that shows s as given. The fence is
// one backtick longer than the longest run of backticks in s, and s is padded
// with a space on each side when it starts or ends with a backtick or a space,
// which CommonMark would otherwise merge into the fence or strip. In a table
// cell a pipe is written as "\|", the only escape a code span in a cell needs.
// An empty s renders as nothing.
func codeSpan(s string, inTable bool) string {
	if s == "" {
		return ""
	}

	s = codeSpanLineEndings.Replace(s)
	fence := strings.Repeat("`", longestRun(s, '`')+1)

	pad := ""
	if needsPadding(s) {
		pad = " "
	}

	if inTable {
		s = strings.ReplaceAll(s, "|", `\|`)
	}

	return fence + pad + s + pad + fence
}

// needsPadding reports whether code span content s must be padded with a space
// on each side: it starts or ends with a backtick, which would join the fence,
// or with a space, which CommonMark strips from content that starts and ends
// with one. Content of spaces only is never stripped, so it is never padded.
func needsPadding(s string) bool {
	if strings.Trim(s, " ") == "" {
		return false
	}

	first, last := s[0], s[len(s)-1]

	return first == '`' || last == '`' || first == ' ' || last == ' '
}

// longestRun returns the length of the longest run of byte c in s.
func longestRun(s string, c byte) int {
	longest, run := 0, 0

	for i := range len(s) {
		if s[i] != c {
			run = 0

			continue
		}

		run++
		longest = max(longest, run)
	}

	return longest
}
