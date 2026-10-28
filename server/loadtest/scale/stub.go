//go:build !loadtest

// The scale tool (`loadtest_scale.go`, `tenancy.go`) is behind `-tags loadtest`,
// so `go build ./...`, `go vet ./...` and the coverage floor do not see a 400
// statements of test scaffolding. A directory whose every file is tagged is a
// build error ("build constraints exclude all Go files"), so this file is the
// untagged shape of the same package: it keeps `./...` green in both
// configurations, and it says out loud how to get the real tool.
//
// Run the measurement with scripts/scale-tenancy.sh (docs/development.md →
// "Scale load test").

package main

func main() {
	println("ourway scale tool: build with -tags loadtest, run with scripts/scale-tenancy.sh")
}
