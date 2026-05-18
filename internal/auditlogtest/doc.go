// Package auditlogtest provides test-only helpers for resetting the audit
// log package state between test cases. It is in a dedicated subpackage so
// that the production internal/auditlog package does not export test-
// lifecycle functions that production code could accidentally call.
// Import this package only from _test.go files.
package auditlogtest
