// Terraform provider for Uptimecheck: monitors as code, alert channels, projects and status pages.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/uptimecheck-com/terraform-provider-uptimecheck/internal/provider"
)

// Set by the release build.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/uptimecheck-com/uptimecheck",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
