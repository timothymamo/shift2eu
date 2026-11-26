// Package assessment turns the mapping of every resource in an inventory onto a
// target's catalog into a migration assessment: a verdict per resource (its fit,
// best offering, reasons and the capabilities it must do without), the blockers
// that stop resources with fit none from moving, and summary counts per fit.
// Fits and reasons come from package mapping unchanged.
//
// AssessWith takes the choices of PRD #190 (ADR 0027): one target region that
// scopes availability (mapping.MapIn) and is the estimate region, and whether to
// estimate. With estimates on, each verdict carries its plan and monthly
// estimate from package estimate, and the assessment their total. Sizes never
// change a fit.
//
// Every assessment also groups its verdicts by workload (ADR 0028): one
// WorkloadSummary per workload name, white space runs collapsed, in name order,
// then the "No workload" group, each with its own fit counts, blockers, estimate
// total and source cost. Each verdict notes in AffectedBy the resources it
// depends on or replicates that are blocked (direct relations only), and the
// assessment sums the source's own monthly cost. None of these changes a fit.
// ShowWorkloads tells a report whether a per-workload section is worth showing.
//
// Compare puts several targets' assessments of one inventory side by side, in
// target key order, and finds the shared blockers: the resources with fit none
// on every target. It ranks no target.
//
// What every report format shares lives here, so the formats cannot drift
// apart: Verdict.Ties names the other candidates a reader could take for the
// best one, Verdict.BestOfferingGlobal and Verdict.GlobalTies which of the
// offerings a report names are global (catalog.Offering.Global, ADR 0048), so
// every format marks the same ones, Verdict.ChangesToList gives the required changes a report lists
// (those of a partial fit only), and
// Comparison.Validate the preconditions a comparison must meet before any
// reporter lays it out (the same resources in the same order on every target,
// the same workload groups, and shared blockers that name only compared
// targets).
//
// Domain layer: standard library and other domain packages only, no I/O, no
// clock.
package assessment
