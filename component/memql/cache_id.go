package memql

import "github.com/znasllc-io/memql/core/id"

// cacheIdEngine is the shared id engine for cache key generation.
// Query keys do not need Exists history; retain only the bounded byte table.
var cacheIdEngine = id.NewUntracked()
