// Package upload defines optional HTTP upload extensions.
package upload

import "net/http"

// HTTPUploadExtension is an HTTP-mounted upload protocol. Prefix identifies
// its mount path; Handler owns request parsing, upload lifecycle, and the
// response containing the completed file reference.
type HTTPUploadExtension interface {
	Prefix() string
	Handler() http.Handler
}
