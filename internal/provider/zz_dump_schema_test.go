package provider

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestZZDumpProviderSchema(t *testing.T) {
	out := os.Getenv("DUMP_SCHEMA_TO")
	if out == "" {
		t.Skip("no DUMP_SCHEMA_TO")
	}
	p := New("test")()
	resp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, resp)
	m := map[string]string{}
	for name, attr := range resp.Schema.Attributes {
		m[name] = attr.GetMarkdownDescription()
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
