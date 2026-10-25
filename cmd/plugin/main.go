// Command plugin validates task plugins and replays offline fixtures without
// starting the API server or initializing a database.
package main

import (
	"os"

	"eflow.local/new-api/pkg/jsplugin"
)

func main() {
	os.Exit(jsplugin.RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
