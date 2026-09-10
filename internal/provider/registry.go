package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources and data sources register themselves here from an init() in
// their own file, and Resources/DataSources on the provider return whatever
// registered.
//
// The alternative is a literal list in provider.go naming every constructor.
// That list is a single line of churn per addition, which is fine for one
// provider but not for the way this one is being filled in: one pull request
// per API surface, each adding a name to the same line. Every such branch
// conflicts with every other, and the resolution is mechanical but has to be
// redone on each rebase. Registration keeps a new surface inside its own
// files.
//
// Both slices are written only from init(), which the runtime serialises, so
// no locking is needed; nothing appends after the provider is served.
var (
	resourceFactories   []func() resource.Resource
	dataSourceFactories []func() datasource.DataSource
)

// registerResource adds a resource constructor. Call it from an init() in
// the file that defines the resource.
func registerResource(f func() resource.Resource) {
	resourceFactories = append(resourceFactories, f)
}

// registerDataSource adds a data source constructor. Call it from an init()
// in the file that defines the data source.
func registerDataSource(f func() datasource.DataSource) {
	dataSourceFactories = append(dataSourceFactories, f)
}
