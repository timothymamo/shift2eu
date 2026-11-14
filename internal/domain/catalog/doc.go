// Package catalog models what a target EU cloud provider (OVHcloud, Scaleway,
// UpCloud) offers: its offerings, the taxonomy categories and capabilities each
// one covers, the EU regions it is available in, the plans it sells and their
// monthly prices in EUR, and the dated, sourced snapshot the data was taken
// from, with the default region estimates use (ADR 0024).
//
// A Catalog is validated when it is built with New: a target display name that
// is not blank, a snapshot dated and sourced from an absolute https URL with an
// ASCII DNS host name (not percent-encoded, and not ending in a number, so a
// browser reads no IPv4 address in it, not even 0x7f.1 or 2130706433), no user
// info, no square brackets, complete percent escapes, RFC 3986 characters only,
// no HTML character reference shape (a numeric one with or without its ';') and
// percent escapes that decode to valid text, uniquely named offerings that each
// serve at least one known category in at least one region, known capabilities,
// and free text (target, offering names, regions) that passes text.Validate.
// Offering names are compared ignoring the length of runs of white space;
// look-alike letters and Unicode normalisation are not compared. Values are
// stored as given, never trimmed or decoded; the retrieval date is kept in UTC.
// Its offerings are kept in name order, each with its categories, capabilities
// and regions sorted and de-duplicated, so everything derived from it is
// deterministic. Each offering's capabilities are expanded with the ones they
// imply (taxonomy Capability.Implies), so an offering listing
// zone-high-availability also has node-high-availability; inventory resources
// are never expanded.
//
// Money is never a float: an Amount is an exact count of ten-thousandths of a
// euro, and a plan's memory an exact count of thousandths of a GiB. New refuses
// a plan without a usable name, with sizes out of range or without a price, a
// duplicate plan name, a price that is negative, above MaxAmount, not in EUR or
// in a region the offering is not in, and two prices of one list that apply to
// the same region (a price with no region applies to every one). An offering's
// fixed fees, its monthly fee per instance such as a Kubernetes control plane
// (ADR 0026), are a price list too, and when there are any they must cover
// every region of the offering, so no region reads as free by omission. An
// offering's PlanBillingCapHours, the most hours a month its plans are billed
// (ADR 0029), is 0 for uncapped or from 1 to MaxBillingCapHours. It keeps each
// offering's plans sorted by name and every price list sorted by first region,
// the every-region price first, and a snapshot's default region as given. A
// zero Catalog has not been validated; later layers only receive catalogs built
// by New.
//
// Estimates look regions and prices up on a catalog (ADR 0027):
// Catalog.Regions lists the regions a target offers, sorted (every offering's
// regions and the default region), Offering.AvailableIn says whether an
// offering is in a region, and Offering.PlanPrice and Offering.StoragePrice
// give a plan's and a GiB's price in a region. Regions are compared exactly,
// byte for byte, never ignoring case or white space.
//
// The target is the display name reports show (for example Scaleway), not a CLI
// key: choosing an adapter from a key is the composition root's job.
//
// Domain layer: standard library and other domain packages only, no I/O, no
// clock.
package catalog
