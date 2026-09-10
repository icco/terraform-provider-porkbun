package porkbun

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Domain is Porkbun's per-domain metadata, shared by /domain/get and
// /domain/listAll. The API sends the boolean-ish fields as 0/1 integers.
type Domain struct {
	Domain       string  `json:"domain"`
	Status       string  `json:"status"`
	TLD          string  `json:"tld"`
	CreateDate   string  `json:"createDate"`
	ExpireDate   string  `json:"expireDate"`
	SecurityLock flexInt `json:"securityLock"`
	WhoisPrivacy flexInt `json:"whoisPrivacy"`
	AutoRenew    flexInt `json:"autoRenew"`
	// APIAccess is the per-domain API opt-in gate. A key cannot operate on a
	// domain with apiAccess 0, no matter how well scoped it is.
	APIAccess flexInt `json:"apiAccess"`
	// NotLocal is 1 when the domain is delegated away from Porkbun's
	// nameservers. Porkbun still accepts /dns/* writes for such a domain and
	// still answers SUCCESS, but nothing resolves them, because no resolver
	// asks Porkbun for that zone.
	NotLocal flexInt `json:"notLocal"`
}

// ListDomainsOptions filters /domain/listAll. A nil pointer means "no filter".
type ListDomainsOptions struct {
	APIAccess          *bool
	AutoRenew          *bool
	NameContains       string
	TLDs               []string
	ExpiringWithinDays *int64
}

// GetDomain reads metadata for one domain in the account.
func (c *Client) GetDomain(ctx context.Context, domain string) (*Domain, error) {
	var out struct {
		Domain Domain `json:"domain"`
	}
	if err := c.get(ctx, "domain/get/"+escapePath(domain), nil, &out); err != nil {
		return nil, err
	}
	return &out.Domain, nil
}

// ListDomains pages through /domain/listAll and returns every matching
// domain. Porkbun returns at most 1000 per call; paging is handled here so
// callers never see `start`.
func (c *Client) ListDomains(ctx context.Context, opts ListDomainsOptions) ([]Domain, error) {
	const pageSize = 1000

	base := url.Values{}
	if opts.APIAccess != nil {
		base.Set("apiAccess", yesNo(*opts.APIAccess))
	}
	if opts.AutoRenew != nil {
		base.Set("autoRenew", yesNo(*opts.AutoRenew))
	}
	if opts.NameContains != "" {
		base.Set("nameContains", opts.NameContains)
	}
	if opts.ExpiringWithinDays != nil {
		base.Set("expiringWithinDays", strconv.FormatInt(*opts.ExpiringWithinDays, 10))
	}
	for _, tld := range opts.TLDs {
		tld = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tld)), ".")
		if tld != "" {
			base.Add("tlds[]", tld)
		}
	}

	var all []Domain
	for start := 0; ; start += pageSize {
		q := url.Values{}
		for k, vs := range base {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("start", strconv.Itoa(start))

		var page struct {
			Count   int      `json:"count"`
			Domains []Domain `json:"domains"`
		}
		if err := c.get(ctx, "domain/listAll", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Domains...)
		if len(page.Domains) < pageSize {
			return all, nil
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
