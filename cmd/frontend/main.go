// Command frontend is a service that serves the web frontend and API.
package main

import (
	"github.com/sourcegraph/sourcegraph/cmd/frontend/shared"
	"github.com/sourcegraph/sourcegraph/cmd/sourcegraph-oss/osscmd"
)

func main() {
	println("wrfspwffp")
	osscmd.DeprecatedSingleServiceMainOSS(shared.Service)
}
