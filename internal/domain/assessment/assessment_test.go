package assessment_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

const sourceName = "aws"

// newInventory is the only way to build an Inventory, so Assess receives the
// same canonical form the rest of the program does.
func newInventory(t *testing.T, resources []inventory.Resource) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New(sourceName, resources)
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// awsResources is one inventory covering every fit: direct, partial and none, and
// an unclassified resource that must be neither dropped nor mapped.
func awsResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "bucket-example",
			RawType:      "AWS::S3::Bucket",
			Category:     taxonomy.StorageObject,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.ObjectVersioning},
		},
		{
			ID:           "db-example",
			RawType:      "AWS::RDS::DBInstance",
			Category:     taxonomy.DatabasePostgreSQL,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.PointInTimeRestore, taxonomy.ReadReplicas},
		},
		{
			ID:       "fn-example",
			RawType:  "AWS::Lambda::Function",
			Category: taxonomy.ServerlessFunction,
			Region:   "eu-west-1",
		},
		{
			ID:           "i-0123456789abcdef0",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		},
		{
			ID:       "misc-example",
			RawType:  "AWS::CloudFormation::CustomResource",
			Category: taxonomy.Unclassified,
			Region:   "eu-west-1",
		},
		{
			ID:           "partial-example",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.Snapshots},
		},
	}
}

func verdictIDs(verdicts []assessment.Verdict) []string {
	if len(verdicts) == 0 {
		return nil
	}

	ids := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		ids = append(ids, v.Resource.ID)
	}

	return ids
}

func candidateNames(candidates []mapping.Candidate) []string {
	if len(candidates) == 0 {
		return nil
	}

	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.Offering.Name)
	}

	return names
}

// verdictFor returns the verdict of the resource with that ID, failing the test
// when the resource was dropped.
func verdictFor(t *testing.T, verdicts []assessment.Verdict, id string) assessment.Verdict {
	t.Helper()

	for _, v := range verdicts {
		if v.Resource.ID == id {
			return v
		}
	}

	t.Fatalf("no verdict for resource %q: the resource was dropped", id)

	return assessment.Verdict{}
}

// TestAssess pins what the report shows for one inventory: a verdict per
// resource in resource ID order, the best offering, its required changes, and a
// summary and blockers derived from them.
func TestAssess(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, awsResources())
	cat := testutil.ScalewayCatalog(t)

	got := assessment.Assess(inv, cat)

	t.Run("copied metadata", func(t *testing.T) {
		t.Parallel()

		if got.Target != cat.Target() {
			t.Errorf("Target = %q, want %q", got.Target, cat.Target())
		}

		if got.Source != inv.Source() {
			t.Errorf("Source = %q, want %q", got.Source, inv.Source())
		}

		if diff := cmp.Diff(cat.Snapshot(), got.Snapshot); diff != "" {
			t.Errorf("Snapshot mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("verdicts", func(t *testing.T) {
		t.Parallel()

		// One verdict per resource, in resource ID order, none dropped.
		if diff := cmp.Diff([]string{
			"bucket-example",
			"db-example",
			"fn-example",
			"i-0123456789abcdef0",
			"misc-example",
			"partial-example",
		}, verdictIDs(got.Verdicts)); diff != "" {
			t.Errorf("verdict IDs mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			name                string
			id                  string
			wantFit             mapping.Fit
			wantBestOffering    string
			wantCandidates      []string
			wantRequiredChanges []taxonomy.Capability
		}{
			{
				name:             "all capabilities offered",
				id:               "bucket-example",
				wantFit:          mapping.Direct,
				wantBestOffering: "Object Storage",
				wantCandidates:   []string{"Object Storage"},
			},
			{
				// Two direct candidates: the name decides.
				name:             "best direct candidate by name",
				id:               "db-example",
				wantFit:          mapping.Direct,
				wantBestOffering: "Managed Database for PostgreSQL",
				wantCandidates: []string{
					"Managed Database for PostgreSQL",
					"Managed Database for PostgreSQL and MySQL",
				},
			},
			{
				name:             "unserved category",
				id:               "fn-example",
				wantFit:          mapping.None,
				wantBestOffering: "",
				wantCandidates:   nil,
			},
			{
				// The fixture's only compute.vm offering: Elastic Metal is a
				// bare-metal server and no VM candidate (issue #187, ADR 0023).
				name:             "the only VM candidate",
				id:               "i-0123456789abcdef0",
				wantFit:          mapping.Direct,
				wantBestOffering: "Instances",
				wantCandidates:   []string{"Instances"},
			},
			{
				// Never dropped, never mapped.
				name:             "unclassified resource",
				id:               "misc-example",
				wantFit:          mapping.None,
				wantBestOffering: "",
				wantCandidates:   nil,
			},
			{
				// The best candidate misses a capability the resource uses, so it
				// becomes a required change.
				name:                "missing capability",
				id:                  "partial-example",
				wantFit:             mapping.Partial,
				wantBestOffering:    "Instances",
				wantCandidates:      []string{"Instances"},
				wantRequiredChanges: []taxonomy.Capability{taxonomy.Snapshots},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				verdict := verdictFor(t, got.Verdicts, tc.id)

				if verdict.Fit != tc.wantFit {
					t.Errorf("Fit = %q, want %q", verdict.Fit, tc.wantFit)
				}

				if verdict.BestOffering != tc.wantBestOffering {
					t.Errorf("BestOffering = %q, want %q", verdict.BestOffering, tc.wantBestOffering)
				}

				if diff := cmp.Diff(tc.wantCandidates, candidateNames(verdict.Candidates)); diff != "" {
					t.Errorf("candidate names mismatch (-want +got):\n%s", diff)
				}

				if diff := cmp.Diff(tc.wantRequiredChanges, verdict.RequiredChanges); diff != "" {
					t.Errorf("RequiredChanges mismatch (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("summary", func(t *testing.T) {
		t.Parallel()

		want := assessment.Summary{Total: 6, Direct: 3, Partial: 1, None: 2}
		if diff := cmp.Diff(want, got.Summary); diff != "" {
			t.Errorf("Summary mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("blockers", func(t *testing.T) {
		t.Parallel()

		// One blocker per reason of every Fit None verdict, in resource ID order.
		// Partial and direct resources are not blockers.
		want := []assessment.Blocker{
			{
				ResourceID: "fn-example",
				Reason: mapping.Reason{
					Code:     mapping.NoOffering,
					Target:   "Scaleway",
					Category: taxonomy.ServerlessFunction,
				},
			},
			{
				ResourceID: "misc-example",
				Reason:     mapping.Reason{Code: mapping.NotClassified},
			},
		}

		if diff := cmp.Diff(want, got.Blockers); diff != "" {
			t.Errorf("Blockers mismatch (-want +got):\n%s", diff)
		}
	})
}

// A verdict is mapping.Map plus the best candidate's name and missing
// capabilities: assessment must not invent a fit or a reason of its own.
func TestAssessVerdictsComeFromMapping(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, awsResources())
	cat := testutil.ScalewayCatalog(t)

	got := assessment.Assess(inv, cat)

	for _, resource := range inv.Resources() {
		t.Run(resource.ID, func(t *testing.T) {
			t.Parallel()

			want := mapping.Map(resource, cat)
			verdict := verdictFor(t, got.Verdicts, resource.ID)

			if diff := cmp.Diff(resource, verdict.Resource); diff != "" {
				t.Errorf("Verdict.Resource mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(want.Fit, verdict.Fit); diff != "" {
				t.Errorf("Verdict.Fit mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(want.Candidates, verdict.Candidates); diff != "" {
				t.Errorf("Verdict.Candidates mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(want.Reasons, verdict.Reasons); diff != "" {
				t.Errorf("Verdict.Reasons mismatch (-want +got):\n%s", diff)
			}

			best, ok := want.Best()
			if !ok {
				if verdict.BestOffering != "" {
					t.Errorf("BestOffering = %q, want empty without a candidate", verdict.BestOffering)
				}

				if verdict.RequiredChanges != nil {
					t.Errorf("RequiredChanges = %v, want nil without a candidate", verdict.RequiredChanges)
				}

				return
			}

			if verdict.BestOffering != best.Offering.Name {
				t.Errorf("BestOffering = %q, want the best candidate %q", verdict.BestOffering, best.Offering.Name)
			}

			if diff := cmp.Diff(best.Missing, verdict.RequiredChanges); diff != "" {
				t.Errorf("RequiredChanges are not the best candidate's Missing (-want +got):\n%s", diff)
			}
		})
	}
}

// TestAssessBlockerForKafkaOnUpCloud pins that a resource of a category the
// target does not serve is a blocker that names its category: PRD #571's
// messaging.kafka against UpCloud, whose product list has no Kafka, so its fit
// is none, its summary counts it as not portable, and its one blocker carries
// the reason the report shows (PRD #571, Mapping and report).
func TestAssessBlockerForKafkaOnUpCloud(t *testing.T) {
	t.Parallel()

	kafka := taxonomy.MessagingKafka

	inv := newInventory(t, []inventory.Resource{{
		ID:       "kafka-example",
		RawType:  "AWS::MSK::Cluster",
		Category: kafka,
		Region:   "eu-west-1",
	}})
	cat := testutil.UpCloudCatalog(t)

	got := assessment.Assess(inv, cat)

	verdict := verdictFor(t, got.Verdicts, "kafka-example")
	if verdict.Fit != mapping.None {
		t.Errorf("Fit = %q, want %q", verdict.Fit, mapping.None)
	}

	wantSummary := assessment.Summary{Total: 1, None: 1}
	if diff := cmp.Diff(wantSummary, got.Summary); diff != "" {
		t.Errorf("Summary mismatch (-want +got):\n%s", diff)
	}

	wantBlockers := []assessment.Blocker{{
		ResourceID: "kafka-example",
		Reason:     mapping.Reason{Code: mapping.NoOffering, Target: "UpCloud", Category: kafka},
	}}

	if len(got.Blockers) != 1 {
		t.Fatalf("Blockers = %v, want exactly one", got.Blockers)
	}

	if diff := cmp.Diff(wantBlockers, got.Blockers); diff != "" {
		t.Errorf("Blockers mismatch (-want +got):\n%s", diff)
	}

	const wantReason = "no offering in the UpCloud catalog for messaging.kafka"

	if reason := got.Blockers[0].Reason.String(); reason != wantReason {
		t.Errorf("blocker reason = %q, want %q", reason, wantReason)
	}
}

// TestAssessEmptyInventory pins that an empty inventory is a valid run: no
// verdicts, no blockers, zero counts.
func TestAssessEmptyInventory(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, nil)
	cat := testutil.ScalewayCatalog(t)

	got := assessment.Assess(inv, cat)

	if len(got.Verdicts) != 0 {
		t.Errorf("Verdicts = %v, want none", verdictIDs(got.Verdicts))
	}

	if len(got.Blockers) != 0 {
		t.Errorf("Blockers = %v, want none", got.Blockers)
	}

	if diff := cmp.Diff(assessment.Summary{}, got.Summary); diff != "" {
		t.Errorf("Summary mismatch (-want +got):\n%s", diff)
	}

	if got.Target != cat.Target() {
		t.Errorf("Target = %q, want %q", got.Target, cat.Target())
	}

	if got.Source != inv.Source() {
		t.Errorf("Source = %q, want %q", got.Source, inv.Source())
	}
}

// The same inventory must give the same assessment, whatever order its resources
// were listed in: a report may not depend on file or map order.
func TestAssessIsDeterministic(t *testing.T) {
	t.Parallel()

	resources := awsResources()
	cat := testutil.ScalewayCatalog(t)

	first := assessment.Assess(newInventory(t, resources), cat)
	second := assessment.Assess(newInventory(t, resources), cat)

	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("two assessments of the same inventory differ (-first +second):\n%s", diff)
	}

	shuffled := slices.Clone(resources)
	slices.Reverse(shuffled)

	if diff := cmp.Diff(first, assessment.Assess(newInventory(t, shuffled), cat)); diff != "" {
		t.Errorf("assessment depends on the order resources were listed in (-want +got):\n%s", diff)
	}
}
