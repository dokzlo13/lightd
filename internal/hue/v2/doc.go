// Package v2 provides a custom Hue V2 API (CLIP) client.
//
// This is a hand-written implementation because there is currently no
// publicly available Go library that supports the Hue V2 API with SSE
// (Server-Sent Events) for real-time event streaming.
//
// This package may be replaced with a third-party library in the future
// if one becomes available with proper SSE support.
//
// The V2 API uses HTTPS with self-signed certificates (requires TLS skip verify).
package v2
