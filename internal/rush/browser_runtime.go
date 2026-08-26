package rush

import _ "embed"

//go:generate go run generate_runtime.go

// browserRuntimeJS contains the immutable in-page test API. Keeping it outside
// suite bundles avoids rebuilding and transferring the same framework code for
// every test file.
//
//go:embed browser_runtime.generated.js
var browserRuntimeJS []byte
