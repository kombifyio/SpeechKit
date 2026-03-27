//go:build !kombify

// Package kombify is the build-tag seam between OSS and kombify builds.
// In OSS mode (default), nothing is registered.
// In kombify mode (-tags kombify), the kombify_cloud.go file imports the private module.
package kombify
