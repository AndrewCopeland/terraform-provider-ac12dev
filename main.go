package main

import (
	"context"
	"log"

	"github.com/AndrewCopeland/terraform-provider-ac12dev/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/AndrewCopeland/ac12dev",
	}
	err := providerserver.Serve(context.Background(), provider.New(), opts)
	if err != nil {
		log.Fatal(err)
	}
}
