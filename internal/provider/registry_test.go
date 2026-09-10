package provider

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// registeredResourceNames instantiates every registered resource and asks it
// its type name.
func registeredResourceNames(t *testing.T) []string {
	t.Helper()
	p := New("test")().(*porkbunProvider) //nolint:forcetypeassert // New returns exactly this
	names := make([]string, 0, len(resourceFactories))
	for _, f := range p.Resources(context.Background()) {
		var resp resource.MetadataResponse
		f().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "porkbun"}, &resp)
		names = append(names, resp.TypeName)
	}
	sort.Strings(names)
	return names
}

func registeredDataSourceNames(t *testing.T) []string {
	t.Helper()
	p := New("test")().(*porkbunProvider) //nolint:forcetypeassert // New returns exactly this
	names := make([]string, 0, len(dataSourceFactories))
	for _, f := range p.DataSources(context.Background()) {
		var resp datasource.MetadataResponse
		f().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "porkbun"}, &resp)
		names = append(names, resp.TypeName)
	}
	sort.Strings(names)
	return names
}

// The registry is populated from init() in each surface's own file, so a
// surface that forgets to register does not fail to compile — it simply is
// not in the provider, and every test written against it fails with a
// confusing "unknown resource type". This test is the guard: the expected
// names are listed literally, so adding a surface without registering it,
// or registering one twice, fails here with a clear diff.
func TestRegisteredSurfaces(t *testing.T) {
	t.Parallel()

	wantResources := []string{
		"porkbun_dns_record",
		"porkbun_domain_nameservers",
	}
	wantDataSources := []string{
		"porkbun_account_balance",
		"porkbun_domain",
		"porkbun_domain_nameservers",
		"porkbun_domains",
		"porkbun_pricing",
	}

	if got := registeredResourceNames(t); !equalStrings(got, wantResources) {
		t.Errorf("registered resources:\n got %v\nwant %v", got, wantResources)
	}
	if got := registeredDataSourceNames(t); !equalStrings(got, wantDataSources) {
		t.Errorf("registered data sources:\n got %v\nwant %v", got, wantDataSources)
	}
}

// A double registration is easy to introduce by copying a file, and the
// framework's own error for it is raised only when a real Terraform run
// loads the provider.
func TestRegistrationsAreUniqueAndWellFormed(t *testing.T) {
	t.Parallel()

	for _, names := range [][]string{registeredResourceNames(t), registeredDataSourceNames(t)} {
		seen := map[string]struct{}{}
		for _, n := range names {
			if _, dup := seen[n]; dup {
				t.Errorf("%q is registered more than once", n)
			}
			seen[n] = struct{}{}
			if !strings.HasPrefix(n, "porkbun_") {
				t.Errorf("%q does not carry the provider type name prefix", n)
			}
		}
	}
}

// Every registered surface must offer a schema with at least one attribute.
// A constructor wired up before its schema was written registers fine and
// then panics or renders an empty doc page.
func TestRegisteredSurfacesHaveSchemas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New("test")().(*porkbunProvider) //nolint:forcetypeassert // New returns exactly this

	var _ provider.Provider = p

	for _, f := range p.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "porkbun"}, &meta)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if len(resp.Schema.Attributes) == 0 {
			t.Errorf("resource %s has no schema attributes", meta.TypeName)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "porkbun"}, &meta)
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)
		if len(resp.Schema.Attributes) == 0 {
			t.Errorf("data source %s has no schema attributes", meta.TypeName)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
