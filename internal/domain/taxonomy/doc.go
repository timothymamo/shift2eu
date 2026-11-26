// Package taxonomy defines the provider-neutral service categories (for example
// compute.vm, compute.baremetal, kubernetes.managed, storage.object,
// database.postgresql, cache.redis, network.loadbalancer, search.opensearch,
// messaging.kafka, network.dns) and the capability vocabulary (for example ipv6,
// point-in-time-restore, dnssec) that are the shared language between source
// hyperscalers and target EU providers.
//
// IsGlobal names the categories whose offerings may be global, bound to no
// region the user chooses (network.dns alone); the catalog refuses a global
// offering of any other category.
//
// Source adapters classify discovered resources into these categories and
// target adapters declare which categories their offerings serve, so matching
// never needs a hand-written table for every provider pair.
//
// A capability may imply weaker ones (zone-high-availability implies
// node-high-availability); Capability.Implies holds that as data, so a target
// that lists only the stronger capability is read as providing both.
//
// Parsing is strict: a value that is not exactly a known category or capability
// is an error, never silently treated as unclassified. The error echoes the
// rejected value quoted and truncated (package text), so its length is bounded.
//
// Domain layer: standard library and other domain packages only, no I/O, no
// clock.
package taxonomy
