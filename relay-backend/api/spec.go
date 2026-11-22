// Package apispec embeds the OpenAPI contract so the server can serve it.
package apispec

import _ "embed"

// Spec is the raw openapi.yaml document.
//
//go:embed openapi.yaml
var Spec []byte
