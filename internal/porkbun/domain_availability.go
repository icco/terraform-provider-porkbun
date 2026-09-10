package porkbun

import "context"

// DomainPrice is one price line: the primary registration price, or the
// renewal and transfer prices Porkbun returns alongside it.
//
// Amounts stay strings. They are USD decimals, and rounding money through a
// float on the way to Terraform state would be a silent price change.
type DomainPrice struct {
	// Type is Porkbun's label for the line, e.g. "registration".
	Type string
	// Price is what the caller would pay now, promotional rate included.
	Price string
	// RegularPrice is the undiscounted rate, which is what a renewal costs
	// after a promotional first year.
	RegularPrice string
}

// DomainAvailability is the result of /domain/checkDomain.
type DomainAvailability struct {
	// Domain is filled in by CheckDomain. The API does not echo the name
	// back, so without it a caller holding only the response cannot tell
	// which domain was checked.
	Domain string

	Available bool
	// Registration is the primary price line, priced per year.
	Registration DomainPrice
	// FirstYearPromo is true when Registration.Price is a discounted
	// first-year rate and Registration.RegularPrice is what year two costs.
	FirstYearPromo bool
	// Premium is true for a registry-priced premium name. Those cannot be
	// registered through the API at all, whatever the price says.
	Premium bool
	// MinDuration is the registry-minimum registration term in years.
	MinDuration int64

	Renewal  DomainPrice
	Transfer DomainPrice
}

// checkDomainResponse is the wire shape. The flex types stay on this side of
// the boundary: every documented field of this endpoint is a string, but
// Porkbun spells the same yes/no flags as 1/0 integers on other endpoints
// and minDuration has been seen both ways.
type checkDomainResponse struct {
	Response struct {
		Avail          flexString `json:"avail"`
		Type           flexString `json:"type"`
		Price          flexString `json:"price"`
		FirstYearPromo flexString `json:"firstYearPromo"`
		RegularPrice   flexString `json:"regularPrice"`
		Premium        flexString `json:"premium"`
		MinDuration    flexInt    `json:"minDuration"`
		Additional     struct {
			Renewal  wirePrice `json:"renewal"`
			Transfer wirePrice `json:"transfer"`
		} `json:"additional"`
	} `json:"response"`
	// limits and ttlRemaining are siblings of response and describe the
	// account's rate-limit budget at this instant. Deliberately not decoded:
	// they change on every call, so anything recording them would churn.
}

type wirePrice struct {
	Type         flexString `json:"type"`
	Price        flexString `json:"price"`
	RegularPrice flexString `json:"regularPrice"`
}

func (w wirePrice) price() DomainPrice {
	return DomainPrice{
		Type:         string(w.Type),
		Price:        string(w.Price),
		RegularPrice: string(w.RegularPrice),
	}
}

// isYes reads Porkbun's textual booleans. The spec enums these as yes/no,
// but the same flags are 1/0 integers on other endpoints, so both spellings
// are accepted rather than silently reading as false.
func isYes(v flexString) bool {
	switch v {
	case "yes", "Yes", "YES", "1", "true", "True", "TRUE":
		return true
	}
	return false
}

// CheckDomain reads availability and current pricing for any domain name. It
// is not restricted to domains in the authenticated account.
//
// It is a POST despite being a read, so the request carries an
// Idempotency-Key it has no use for; harmless, and cheaper than a second
// code path through the client.
//
// Rate limited hard: one check per 10 seconds per account by default,
// configurable per API key. Porkbun reports the limit as a body-level
// RATE_LIMIT_EXCEEDED on an ordinary response rather than as an HTTP 429 on
// some paths, which the retrying transport does not consider retryable, so
// that error surfaces to the caller instead of being waited out.
func (c *Client) CheckDomain(ctx context.Context, domain string) (*DomainAvailability, error) {
	var out checkDomainResponse
	if err := c.post(ctx, "domain/checkDomain/"+escapePath(domain), map[string]any{}, &out); err != nil {
		return nil, err
	}

	r := out.Response
	return &DomainAvailability{
		Domain:    domain,
		Available: isYes(r.Avail),
		Registration: DomainPrice{
			Type:         string(r.Type),
			Price:        string(r.Price),
			RegularPrice: string(r.RegularPrice),
		},
		FirstYearPromo: isYes(r.FirstYearPromo),
		Premium:        isYes(r.Premium),
		MinDuration:    r.MinDuration.Int64(),
		Renewal:        r.Additional.Renewal.price(),
		Transfer:       r.Additional.Transfer.price(),
	}, nil
}
