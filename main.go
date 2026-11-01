package main

import (
	"github.com/alex-tw-lam/terraform-provider-gaussdb/gaussdb"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: gaussdb.Provider})
}
