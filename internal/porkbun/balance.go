package porkbun

import "context"

// Balance is the account's available credit, from /account/balance.
type Balance struct {
	// Cents is the balance in cents, never a currency amount in dollars.
	Cents int64
	// Display is Porkbun's own rendering, e.g. "$12.34". It is carried
	// through verbatim rather than formatted from Cents: it is the only part
	// of the response that names a currency.
	Display string
}

// GetBalance reads the available account credit. It is account-wide, so it
// needs no per-domain API opt-in.
func (c *Client) GetBalance(ctx context.Context) (*Balance, error) {
	// The spec types `balance` as an integer and `display` as a string, but
	// Porkbun serves integers as decimal strings on several other endpoints,
	// so both go through the flex decoders. flexInt refuses a decimal such
	// as "12.34" rather than truncating it into a plausible-looking 12.
	var out struct {
		Balance flexInt    `json:"balance"`
		Display flexString `json:"display"`
	}
	if err := c.get(ctx, "account/balance", nil, &out); err != nil {
		return nil, err
	}
	return &Balance{Cents: out.Balance.Int64(), Display: string(out.Display)}, nil
}
