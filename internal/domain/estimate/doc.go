// Package estimate picks the plan a resource would run on at a target and
// estimates its monthly list price there (PRD #190 milestone 6, ADR 0027).
//
// For takes one resource's mapping onto a catalog and the region the estimate
// is for. It chooses the plan among the best candidate offerings (those tied
// with the best one on fit and missing capabilities): the cheapest plan in the
// region whose vCPUs and memory are each at least the resource's, ties broken
// by fewer vCPUs, less memory, plan name and offering name. The estimate is the
// plan price × count × hoursPerMonth / 730 (× min(hoursPerMonth, cap) / cap
// instead when the offering bills its plans at most cap hours a month,
// ADR 0029), plus the storage above what the plan includes × the offering's
// per-GiB price, plus the offering's fixed fee once per resource ×
// hoursPerMonth / 730, never capped (ADR 0026). Storage categories have no
// plan: their estimate is the stored GiB × the per-GiB price.
//
// A resource that cannot be priced is never priced at 0: its Estimate says it
// is not estimated, and why (Reason). Sum totals only the estimated resources
// and counts how many there were.
//
// Money is exact: an amount of euros held as a rational number (math/big), so
// no product of the inventory's and the catalog's capped numbers can wrap. It
// is rounded to the cent, half away from zero, only when it is formatted
// (ADR 0024). Each inventory number is read as the decimal it prints as,
// rounded to six fractional digits, so 0.1 is exactly 1/10 and the
// denominators stay bounded (ADR 0027).
//
// SumSourceCost sums what the source itself charges (the inventory's
// MonthlyCost) exactly, one sum per currency, never converted, and counts how
// many resources have a cost (ADR 0028). It reads each amount as an inventory
// number is read, and is a fact from the inventory, not an estimate.
//
// Domain layer: standard library and other domain packages only, no I/O, no
// clock.
package estimate
