// Package reqlimits centralizes the maximum request body sizes used by
// HTTP handlers. Seam is a single-user local deployment, so the caps
// are sized generously to accommodate large notes, embedded assets, and
// pasted logs, while still bounding memory so a pathological request
// cannot exhaust the process.
package reqlimits

const (
	// MaxJSONBody is the cap for JSON request bodies.
	MaxJSONBody = 100 << 20 // 100 MB

	// MaxUploadBody is the cap for multipart/binary uploads
	// (voice recordings, URL-capture payloads).
	MaxUploadBody = 500 << 20 // 500 MB
)
