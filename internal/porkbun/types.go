package porkbun

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// flexInt decodes a JSON value that Porkbun sends inconsistently as a number,
// a decimal string, or null. `ttl` is an integer in CreateDnsRequest and a
// string in DnsRecordsResponse; `prio` is a nullable string on read and an
// integer on write; `apiAccess` and friends are 0/1 integers. Null decodes to
// zero, which is also Porkbun's own default for prio.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		*f = 0
		return nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return fmt.Errorf("porkbun: %q is not an integer: %w", str, err)
		}
		*f = flexInt(n)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	i, err := n.Int64()
	if err != nil {
		// Tolerate a float only when it is exactly integral. Truncating
		// silently would turn a malformed TTL of 600.5 into a plausible 600
		// and write it to state as if the API had said so.
		fl, ferr := n.Float64()
		if ferr != nil {
			return err
		}
		i = int64(fl)
		if float64(i) != fl {
			return err
		}
	}
	*f = flexInt(i)
	return nil
}

func (f flexInt) MarshalJSON() ([]byte, error) { return json.Marshal(int64(f)) }

// Int64 returns the decoded value.
func (f flexInt) Int64() int64 { return int64(f) }

// Bool interprets Porkbun's 0/1 integer flags.
func (f flexInt) Bool() bool { return f != 0 }

// flexString decodes a JSON value that may arrive as a string, a number, or
// null. Record ids come back both ways depending on the endpoint.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		*f = ""
		return nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = flexString(str)
		return nil
	}
	*f = flexString(s)
	return nil
}

// escapePath escapes a value being interpolated into an API path.
func escapePath(s string) string { return url.PathEscape(strings.TrimSpace(s)) }
