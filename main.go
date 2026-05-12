package main

import "github.com/platform-engineering-labs/formae/pkg/plugin/sdk"

ffunc main() {
	sdk.RunWithManifest(&Plugin{}, sdk.RunConfig{})
}
