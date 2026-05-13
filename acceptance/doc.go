//go:build acceptance

// Package acceptance contains the cpe-labs wire-format acceptance
// suite. Every file under acceptance/ carries the //go:build acceptance
// tag so standard `go test ./...`, `go build ./...`, and `go vet ./...`
// skip it entirely.
//
// Run via `make acceptance` (or `go test -tags=acceptance ./acceptance/...`).
// Regenerate goldens with `make acceptance-update`.
//
// See acceptance/README.md for how to run, update, and add scenarios.
package acceptance
