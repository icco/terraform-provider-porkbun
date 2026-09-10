package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/icco/terraform-provider-porkbun/internal/provider"
)

// Docs are generated from the schemas and the examples/ directory by
// tfplugindocs, which lives in the tools/ module so its dependency tree stays
// out of the provider's. Run `make docs`.

// version is stamped by goreleaser at build time.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address:         "registry.terraform.io/icco/porkbun",
		Debug:           debug,
		ProtocolVersion: 6,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
