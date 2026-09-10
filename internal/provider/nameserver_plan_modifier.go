package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"

	"github.com/icco/terraform-provider-porkbun/internal/porkbun"
)

// suppressNameserverRespelling keeps the state value when the plan denotes
// the same delegation spelled differently. Without it an imported domain
// plans a pointless update forever, having no config to take spelling from.
type suppressNameserverRespelling struct{}

func (m suppressNameserverRespelling) Description(_ context.Context) string {
	return "Ignores case, trailing dots and ordering when comparing nameserver sets."
}

func (m suppressNameserverRespelling) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m suppressNameserverRespelling) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	// setIsFullyKnown, not IsUnknown: a known set can still hold an unknown
	// element (a list mixing literals with a computed value), and ElementsAs
	// turns that into an "unhandled unknown value" error at plan time.
	if !setIsFullyKnown(req.StateValue) || !setIsFullyKnown(req.PlanValue) {
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
