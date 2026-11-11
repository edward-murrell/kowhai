// Package apispec embeds the OpenAPI contract so the server can publish it.
package apispec

import _ "embed"

//go:embed openapi.yaml
var YAML []byte
