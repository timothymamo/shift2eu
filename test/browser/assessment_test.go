//go:build browser

package browser_test

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// This file holds the assessment itself in the browser (PRD #297, Assessment in
// the browser): a bundled example through one target, an inventory pasted with
// several targets, a region and the estimate switch. What is pinned is what a
// user sees in the page, read from the page's own report element and controls,
// and the page is the built bundle served from a local origin.

// reportView is what the page's report element holds, as the test reads it.
type reportView struct {
	// Text is the report's text, with runs of whitespace collapsed.
	Text string `json:"text"`
	// Sections are the report's own headings, h2 to h5, in order.
	Sections []string `json:"sections"`
	// Tables are the report's tables, with their header cells and body rows.
	Tables []table `json:"tables"`
	// Tags are the distinct element names the report holds, lower case.
	Tags []string `json:"tags"`
	// IDs is the number of report elements carrying an id or a name.
	IDs int `json:"ids"`
	// Handlers is the number of attributes starting with "on" in the report.
	Handlers int `json:"handlers"`
}

// table is one table of the report.
type table struct {
	// Headers are the table's header cells.
	Headers []string `json:"headers"`
	// Rows is the number of body rows.
	Rows int `json:"rows"`
}

// reportExpression reads the page's report element into a reportView.
const reportExpression = `(() => {
	const report = document.getElementById("report");
	if (report === null) {
		return {text: "", sections: [], tables: [], tags: [], ids: 0, handlers: 0};
	}
	const one = function (el) { return el.textContent.replace(/\s+/g, " ").trim(); };
	const all = Array.from(report.querySelectorAll("*"));
	const tables = Array.from(report.querySelectorAll("table")).map(function (t) {
		return {
			headers: Array.from(t.querySelectorAll("thead th")).map(one),
			rows: t.querySelectorAll("tbody tr").length,
		};
	});
	const handlers = all.reduce(function (count, el) {
		return count + Array.from(el.attributes).filter(function (attr) {
			return attr.name.toLowerCase().indexOf("on") === 0;
		}).length;
	}, 0);
	return {
		text: one(report),
		sections: Array.from(report.querySelectorAll("h2, h3, h4, h5")).map(one),
		tables: tables,
		tags: Array.from(new Set(all.map(function (el) { return el.tagName.toLowerCase(); }))).sort(),
		ids: report.querySelectorAll("[id], [name]").length,
		handlers: handlers,
	};
})()`

// reportOf reads the page's report element.
func reportOf(t *testing.T, ctx context.Context) reportView {
	t.Helper()

	return eval[reportView](t, ctx, reportExpression)
}

// hasSection reports whether the report shows a section with the name.
func (r reportView) hasSection(name string) bool {
	return slices.Contains(r.Sections, name)
}

// hasTag reports whether the report holds an element with the tag.
func (r reportView) hasTag(tag string) bool {
	return slices.Contains(r.Tags, tag)
}

// tableWith returns the report's first table whose header cells hold the names,
// in that order: a table may carry more columns than the names.
func (r reportView) tableWith(headers ...string) (table, bool) {
	for _, tbl := range r.Tables {
		if headersInOrder(tbl.Headers, headers) {
			return tbl, true
		}
	}

	return table{}, false
}

// headersInOrder reports whether headers holds names as a subsequence.
func headersInOrder(headers, names []string) bool {
	next := 0

	for _, header := range headers {
		if next < len(names) && header == names[next] {
			next++
		}
	}

	return next == len(names)
}

// resourceCount returns how many resources the inventory document holds.
func resourceCount(t *testing.T, document string) int {
	t.Helper()

	var doc struct {
		Resources []struct {
			ID string `json:"id"`
		} `json:"resources"`
	}

	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		t.Fatalf("decode the inventory the page pasted: %v", err)
	}

	return len(doc.Resources)
}

// listPricesDatePattern matches the date a report's estimate caveat carries.
var listPricesDatePattern = regexp.MustCompile(`list prices on (\d{4}-\d{2}-\d{2})`)

// TestAssessmentShowsTheReport pins the first assessment criterion (PRD #297,
// Assessment in the browser): a bundled example, one target, run — and the page
// shows the assessment: the summary, every resource with its fit, best offering,
// blockers and required changes, the workloads, and the estimates with the
// catalog snapshot's date and the caveat. Every bundled example gets its own
// run, so an example that stops being assessable fails here.
func TestAssessmentShowsTheReport(t *testing.T) {
	t.Parallel()

	for _, example := range []string{"aws", "azure", "gcp"} {
		t.Run("the "+example+" example with one target", func(t *testing.T) {
			t.Parallel()

			s := newSite(t)
			ctx := openAssessment(t, s)

			loadExample(t, ctx, example)
			inventory := eval[string](t, ctx, `document.getElementById("inventory").value`)

			setTargets(t, ctx, "ovhcloud")
			snapshot := snapshotOf(t, ctx, "ovhcloud")
			runAssessment(t, ctx)

			if message := alertOf(t, ctx).Text; message != "" {
				t.Fatalf("the page shows the error %q for the %s example, want the report", message, example)
			}

			report := reportOf(t, ctx)

			// The sections the assessment always has: the summary, the resources,
			// the workloads and the estimates. Blockers and required changes are
			// checked below, against what the CLI reports for the same inventory.
			for _, section := range []string{"Summary", "Resources", "Workloads", "Cost estimate"} {
				if !report.hasSection(section) {
					t.Errorf("the report of the %s example shows no %q section, so the page does not show the whole assessment: %v",
						example, section, report.Sections)
				}
			}

			summary, ok := report.tableWith("Fit", "Resources")
			if !ok {
				t.Fatalf("the report of the %s example holds no summary table of fits, only %v", example, report.Tables)
			}

			if summary.Rows < 4 {
				t.Errorf("the summary table of the %s example holds %d rows, want at least the four fits direct, partial, none and total", example, summary.Rows)
			}

			resources, ok := report.tableWith("ID", "Fit", "Best offering")
			if !ok {
				t.Fatalf("the report of the %s example holds no resources table with a fit and a best offering per resource, only %v", example, report.Tables)
			}

			if want := resourceCount(t, inventory); resources.Rows != want {
				t.Errorf("the resources table of the %s example holds %d rows, want one per resource of the inventory, %d", example, resources.Rows, want)
			}

			// Every resource row carries its fit, in the Fit column.
			everyRowHasAFit(t, ctx, example)

			// The estimates: every caveat carries the catalog snapshot's date
			// the page shows next to the target, and says what the prices are.
			dates := listPricesDatePattern.FindAllStringSubmatch(report.Text, -1)
			if len(dates) == 0 {
				t.Fatalf("the report of the %s example states no estimate caveat %q, so no estimate carries the snapshot's date: %q",
					example, "list prices on "+snapshot, report.Text)
			}

			for _, match := range dates {
				if match[1] != snapshot {
					t.Errorf("the report of the %s example dates its estimates %q, but the catalog snapshot the page shows for OVHcloud is %q: every estimate carries the snapshot's date",
						example, match[1], snapshot)
				}
			}

			if !strings.Contains(report.Text, "excluding VAT and discounts") {
				t.Errorf("the report of the %s example carries no caveat saying what the estimates are (list prices on the snapshot's date, excluding VAT and discounts): %q",
					example, report.Text)
			}

			// The page's own prose carries the caveat that they are no quote,
			// which the report's own wording leaves to the page.
			prose := eval[string](t, ctx, `document.querySelector("main").textContent.replace(/\s+/g, " ")`)
			if !strings.Contains(prose, "not a quote") {
				t.Errorf("the assessment page's prose carries no caveat that the estimates are list prices on that date, not a quote: %q", prose)
			}

			// The blockers and required changes the report has are the ones the
			// CLI reports for the same inventory and target: present when the
			// CLI's report has the section, absent when it has not.
			cli := reportOfCLI(t, inventory, "ovhcloud")

			for _, section := range []string{"Blockers", "Required changes"} {
				switch {
				case cli.sectionPresent(section) && !report.hasSection(section):
					t.Errorf("the CLI's report of the %s example has a %q section and the page shows none: the page must show the whole assessment", example, section)
				case !cli.sectionPresent(section) && report.hasSection(section):
					t.Errorf("the page's report of the %s example shows a %q section, but the CLI's report has none for the same inventory and target", example, section)
				}
			}
		})
	}
}

// cliReportView is the sections of one report the CLI wrote, read without a
// browser: the same inventory and options, for what the page must show.
type cliReportView struct {
	sections map[string]bool
}

// sectionPresent reports whether the Markdown report has the section, which
// the Markdown reporter writes as a level-2 heading.
func (r cliReportView) sectionPresent(section string) bool {
	return r.sections[section]
}

// reportOfCLI runs the CLI over the inventory for the target and reads the
// sections and text of its Markdown report.
func reportOfCLI(t *testing.T, inventory, target string) cliReportView {
	t.Helper()

	markdown := string(cliReport(t, cliBinary(t), inventory, "--target", target))
	view := cliReportView{sections: make(map[string]bool, 8)}

	for _, line := range strings.Split(markdown, "\n") {
		if section, ok := strings.CutPrefix(line, "## "); ok {
			view.sections[strings.TrimSpace(section)] = true
		}
	}

	return view
}

// everyRowHasAFit fails the test when a resource row of the report's resources
// table has no fit: the criterion says every resource is shown with its fit.
func everyRowHasAFit(t *testing.T, ctx context.Context, example string) {
	t.Helper()

	ok := eval[bool](t, ctx, `(() => {
		const tables = Array.from(document.querySelectorAll("#report table"));
		const table = tables.find(function (t) {
			const first = t.querySelector("thead th");
			return first !== null && first.textContent.trim() === "ID";
		});
		if (table === undefined) {
			return false;
		}
		const headers = Array.from(table.querySelectorAll("thead th")).map(function (th) {
			return th.textContent.trim();
		});
		const fit = headers.indexOf("Fit");
		if (fit < 0) {
			return false;
		}
		return Array.from(table.querySelectorAll("tbody tr")).every(function (row) {
			const cell = row.children[fit];
			return cell !== undefined && ["direct", "partial", "none"].indexOf(cell.textContent.trim()) >= 0;
		});
	})()`)

	if !ok {
		t.Errorf("a resource row of the %s example's report carries no fit of direct, partial or none, so the page does not show every resource with its fit", example)
	}
}

// schemaVersion1Inventory is a small inventory document of schema version 1,
// which has no sizing, workloads or costs: the comparison must show it too.
const schemaVersion1Inventory = `{
  "schemaVersion": 1,
  "source": "aws",
  "resources": [
    {"id": "i-0123456789abcdef0", "rawType": "AWS::EC2::Instance", "category": "compute.vm", "region": "eu-west-1", "capabilities": ["ipv6"]},
    {"id": "bucket-example", "rawType": "AWS::S3::Bucket", "category": "storage.object", "region": "eu-west-1"},
    {"id": "fn-example", "rawType": "AWS::Lambda::Function", "category": "serverless.function", "region": "eu-west-1"}
  ]
}`

// schemaVersion2Inventory is a small inventory document of schema version 2,
// with a workload and a relation: the comparison must show both versions.
const schemaVersion2Inventory = `{
  "schemaVersion": 2,
  "source": "aws",
  "resources": [
    {"id": "web-one", "rawType": "AWS::EC2::Instance", "category": "compute.vm", "region": "eu-west-1",
     "capabilities": ["ipv6"], "size": {"vcpus": 2, "memoryGiB": 8}, "workload": "shop",
     "relations": [{"type": "depends-on", "target": "db-one"}]},
    {"id": "db-one", "rawType": "AWS::RDS::DBInstance", "category": "database.postgresql", "region": "eu-west-1",
     "capabilities": ["read-replicas"], "workload": "shop"}
  ]
}`

// TestAssessmentComparison pins the comparison criterion (PRD #297, Assessment
// in the browser): an inventory of schema version 1 or 2 pasted into the page,
// run with several targets and with all of them, shows the comparison as
// `shift2eu assess --target all` reports it.
func TestAssessmentComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		document  string
		targets   []string
		resources int
	}{
		{
			name:      "a schema version 1 inventory with several targets",
			document:  schemaVersion1Inventory,
			targets:   []string{"ovhcloud", "scaleway"},
			resources: 3,
		},
		{
			name:      "a schema version 2 inventory with every target",
			document:  schemaVersion2Inventory,
			targets:   []string{"ovhcloud", "scaleway", "upcloud"},
			resources: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newSite(t)
			ctx := openAssessment(t, s)

			pasteInventory(t, ctx, tc.document)
			setTargets(t, ctx, tc.targets...)
			runAssessment(t, ctx)

			if message := alertOf(t, ctx).Text; message != "" {
				t.Fatalf("the page shows the error %q, want the comparison", message)
			}

			report := reportOf(t, ctx)

			if !report.hasSection("Comparison") {
				t.Errorf("the report shows no Comparison section for %d targets: %v", len(tc.targets), report.Sections)
			}

			summary, ok := report.tableWith(append([]string{"Fit"}, targetNames(tc.targets)...)...)
			if !ok {
				t.Fatalf("the report's summary table has no column per target, want the header cells %v, only %v",
					append([]string{"Fit"}, targetNames(tc.targets)...), report.Tables)
			}

			if summary.Rows < 4 {
				t.Errorf("the comparison's summary table holds %d rows, want at least the four fits direct, partial, none and total", summary.Rows)
			}

			resources, ok := report.tableWith("ID", "Category", "Region")
			if !ok {
				t.Fatalf("the comparison holds no resources table, only %v", report.Tables)
			}

			if resources.Rows != tc.resources {
				t.Errorf("the comparison's resources table holds %d rows, want one per resource of the pasted inventory, %d", resources.Rows, tc.resources)
			}

			// Every target has its fit and its best offering column.
			for _, name := range targetNames(tc.targets) {
				if !slices.Contains(resources.Headers, name+" fit") || !slices.Contains(resources.Headers, name+" best offering") {
					t.Errorf("the comparison's resources table has no fit and best offering column for %q: %v", name, resources.Headers)
				}
			}
		})
	}
}

// targetNames maps the target keys to the names the page and the reports give
// them.
func targetNames(targets []string) []string {
	names := map[string]string{"ovhcloud": "OVHcloud", "scaleway": "Scaleway", "upcloud": "UpCloud"}

	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, names[target])
	}

	return out
}

// TestAssessmentRegionAndEstimate pins the third assessment criterion (PRD
// #297, Assessment in the browser): a region chosen for a target scopes the
// report as --region <target>=<region> scopes it, and the estimate switch turned
// off removes the estimates as --no-estimate does.
func TestAssessmentRegionAndEstimate(t *testing.T) {
	t.Parallel()

	t.Run("a region scopes the report", func(t *testing.T) {
		t.Parallel()

		s := newSite(t)
		ctx := openAssessment(t, s)

		loadExample(t, ctx, "aws")
		setTargets(t, ctx, "ovhcloud")

		region := regionOf(t, ctx, "ovhcloud")
		chooseRegion(t, ctx, "ovhcloud", region)
		runAssessment(t, ctx)

		if message := alertOf(t, ctx).Text; message != "" {
			t.Fatalf("the page shows the error %q for the region %q, want the report", message, region)
		}

		report := reportOf(t, ctx)
		if want := "Region: " + region + " (chosen with --region)"; !strings.Contains(report.Text, want) {
			t.Errorf("the report does not state the chosen region as %q, so the region did not scope it: %q", want, report.Text)
		}

		// The same report without a region states the target's default region
		// instead, so the region line above is the choice and not the default.
		if strings.Contains(report.Text, "the target's default for estimates") {
			t.Errorf("the report with a chosen region still dates its estimates to the target's default region: %q", report.Text)
		}
	})

	t.Run("the estimate switch removes the estimates", func(t *testing.T) {
		t.Parallel()

		s := newSite(t)
		ctx := openAssessment(t, s)

		loadExample(t, ctx, "aws")
		setTargets(t, ctx, "ovhcloud")
		setEstimate(t, ctx, false)
		runAssessment(t, ctx)

		if message := alertOf(t, ctx).Text; message != "" {
			t.Fatalf("the page shows the error %q with the estimate switch off, want the report", message)
		}

		report := reportOf(t, ctx)

		if report.hasSection("Cost estimate") {
			t.Errorf("the report shows the %q section although the estimate switch is off: --no-estimate writes none", "Cost estimate")
		}

		for _, unwanted := range []string{"list prices on", "excluding VAT and discounts", "Monthly estimate", "€", "Total:"} {
			if strings.Contains(report.Text, unwanted) {
				t.Errorf("the report with the estimate switch off still carries %q: --no-estimate removes every estimate", unwanted)
			}
		}

		if _, ok := report.tableWith("ID", "Raw type", "Category"); !ok {
			t.Errorf("the report with the estimate switch off holds no resources table, only %v", report.Tables)
		}
	})
}
