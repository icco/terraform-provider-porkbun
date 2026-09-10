package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

// suppressNameserverRespelling keeps the value already in state whenever the
// planned set denotes the same delegation, differing only in case, trailing
// dots, ordering or duplicates.
//
// Terraform compares a Required attribute's configuration against state
// literally, so without this a config that spells a nameserver
// "ns1.example.com." while state holds "ns1.example.com" plans a pointless
// in-place update forever — which is exactly the state a fleet of domains
// lands in right after `terraform import`, since an import has no
// configuration to take its spelling from.
type suppressNameserverRespelling struct{}

func (m suppressNameserverRespelling) Description(_ context.Context) string {
	return "Ignores case, trailing dots and ordering when comparing nameserver sets."
}

func (m suppressNameserverRespelling) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m suppressNameserverRespelling) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	var stateNS, planNS []string
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &stateNS, false)...)
	resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planNS, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if porkbun.SameNameserverSet(stateNS, planNS) {
		resp.PlanValue = req.StateValue
	}
}
