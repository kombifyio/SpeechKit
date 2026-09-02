// Package catalog ships the built-in SpeechKit provider and model catalog.
//
// [DefaultCatalog] returns the immutable set of [speechkit.ProviderProfile]
// values SpeechKit knows about, grouped by [speechkit.Mode]; [Catalog.With]
// extends it with host-owned providers without mutating the built-in set.
// [DefaultProviderDefaults] and [DefaultProviderMatrix] describe per-provider
// auth, transport and feature support for setup UIs, and
// [DefaultModelRegistry] is the source of truth for model IDs, lifecycles and
// freshness metadata.
//
// The root speechkit package owns the contracts this catalog is expressed in
// (profiles, modes, capabilities, [speechkit.RuntimePolicy]); this package
// owns the data. Hosts that never show a provider picker do not need to import
// it.
package catalog
