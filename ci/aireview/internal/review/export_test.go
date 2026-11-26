package review

// The delimiter rule of the host scan is unexported: only hasHostname uses it.
// The black-box test reaches it through this alias, which exists in test builds
// alone (issue #649).

// NamespaceMayFollow is namespaceMayFollow, for the tests: whether the text
// before an Azure provider namespace lets it be one, byte by byte.
var NamespaceMayFollow = namespaceMayFollow
