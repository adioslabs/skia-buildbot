// Package query provides tools for searching over structured keys.
//
// One example of such a structured key is a Trace id. A key is simply a
// string, but with a very specific format.  The parameters are serialized so
// that the parameter names appear in alphabetical order, and each name=value
// pair is delimited by a comma. For example, this map:
//
//  a := map[string]string{"d": "w", "a": "b", "c": "d"}
//
// Would be serialized as this key:
//
//   ,a=b,c=d,d=w,
//
// Structured keys are a serialization of a map[string]string, so duplicate
// parameter names are not allowed.
//
// Structuctured key parameter names and values are restricted to the following
// chars:
//
//   [a-zA-Z0-9._-]
//
package query

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"go.skia.org/infra/go/util"
)

var (
	keyRe   = regexp.MustCompile("^,([a-zA-Z0-9._\\-]+=[a-zA-Z0-9._\\-]+,)+$")
	paramRe = regexp.MustCompile("^[a-zA-Z0-9._\\-]+$")
)

// ValidateKey returns true if a key is valid, i.e. if the parameter names are
// in alphabetical order and if the param names and values are restricted to
// valid values.
func ValidateKey(key string) bool {
	if !keyRe.MatchString(key) {
		return false
	}
	parts := strings.Split(key, ",")
	if len(parts) < 3 {
		return true
	}
	parts = parts[1 : len(parts)-1]
	if !sort.IsSorted(sort.StringSlice(parts)) {
		return false
	}
	lastName := ""
	for _, s := range parts {
		pair := strings.Split(s, "=")
		if len(pair) != 2 {
			return false
		}
		if lastName == pair[0] {
			return false
		}
		lastName = pair[0]
	}
	return true
}

// MakeKey returns a structured key from the given map[string]string, or a
// non-nil error if the parameter names or values violate the structured key
// restrictions.
func MakeKey(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", fmt.Errorf("Map must have at least one entry.")
	}
	keys := make([]string, 0, len(m))
	for k, _ := range m {
		if !paramRe.MatchString(k) {
			return "", fmt.Errorf("Key contains invalid characters: %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ret := ","
	for _, k := range keys {
		if !paramRe.MatchString(m[k]) {
			return "", fmt.Errorf("Value contains invalid characters: %q", m[k])
		}
		ret += fmt.Sprintf("%s=%s,", k, m[k])
	}
	return ret, nil
}

// ParseKey parses the structured key, and if valid, returns the parsed values
// as a map[string]string, otherwise is returns a non-nil error.
func ParseKey(key string) (map[string]string, error) {
	if !keyRe.MatchString(key) {
		return nil, fmt.Errorf("Key is not valid, fails to match regex: %s", key)
	}
	ret := map[string]string{}
	parts := strings.Split(key, ",")
	if len(parts) < 3 {
		// Maybe should be an error?
		return map[string]string{}, nil
	}
	parts = parts[1 : len(parts)-1]
	if !sort.IsSorted(sort.StringSlice(parts)) {
		return nil, fmt.Errorf("Key is not valid, params are unsorted: %v", parts)
	}
	lastName := ""
	for _, s := range parts {
		pair := strings.Split(s, "=")
		if len(pair) != 2 {
			return nil, fmt.Errorf("Invalid key=value pair: %s", s)
		}
		if lastName == pair[0] {
			return nil, fmt.Errorf("Duplicate key: %s", s)
		}
		ret[pair[0]] = pair[1]
		lastName = pair[0]
	}
	return ret, nil
}

// queryParam represents a query on a particular parameter in a key.
type queryParam struct {
	keyMatch    string         // The param key, including the leading "," and trailing "=".
	keyMatchLen int            // The length of keyMatch.
	isWildCard  bool           // True if this is a wildcard value match.
	isRegex     bool           // True if this is a regex value match.
	isNegative  bool           // True if this is a negative value match.
	values      []string       // The potential matches for the value.
	reg         *regexp.Regexp // The regexp to match against, if a regexp search.
}

// Query represents a query against a key, i.e. Query.Matches can return true
// or false if a given key matches the query. For example, this query will find all
// keys that have a value of 565 for 'config' and true for 'debug':
//
//		q := New(url.Values{"config": []string{"565"}, "debug": []string{"true"}})
//
// This will find all keys that have a value of '565' or '8888':
//
//		q := New(url.Values{"config": []string{"565", "8888"}})
//
// If the first parameter value is preceeded with an '!' then the match is negated,
// i.e. this query will match all keys that have a 'config' param, but whose value
// is not '565'.
//
//		q := New(url.Values{"config": []string{"!565"}})
//
// If the parameter value is '*' then the match will match all keys that have
// that parameter name. I.e. this will match all keys that have a parameter
// named 'config', regardless of the value:
//
//		q := New(url.Values{"config": []string{"*"}})
//
// If the parameter value begins with '~' then the rest of the value is interpreted
// as a regular expression. I.e. this will match all keys that have a parameter
// named 'arch' that begin with 'x':
//
//		q := New(url.Values{"arch": []string{"~^x"}})
//
//
// Here is more complex example that matches all tests that have the 'name'
// parameter with a value of 'desk_nytimes.skp', a 'config' param that does not
// equal '565' or '8888', and has an 'extra_config' parameter of any value.
//
//	  q := New(url.Values{
//        "name": "desk_nytimes.skp",
//        "config": []string{"!565", "8888"},
//        "extra_config": []string{"*"}})
//
type Query struct {
	// These are in alphabetical order of parameter name.
	params []queryParam
}

// New creates a Query from the given url.Values. It represents a query to be
// used against keys.
func New(q url.Values) (*Query, error) {
	keys := make([]string, 0, len(q))
	for k, _ := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	params := make([]queryParam, 0, len(q))
	for _, key := range keys {
		keyMatch := "," + key + "="
		isWildCard := false
		isRegex := false
		isNegative := false
		values := q[key]
		var reg *regexp.Regexp
		var err error
		// Is this param query a wildcard?
		if len(q[key]) == 1 {
			if q[key][0] == "*" {
				isWildCard = true
			}
			if q[key][0][:1] == "~" {
				isRegex = true
				reg, err = regexp.Compile(q[key][0][1:])
				if err != nil {
					return nil, fmt.Errorf("Error compiling regexp %q: %s", q[key][0][1:], err)
				}
			}
		}
		// Is this param query a negative match?
		if len(q[key]) >= 1 {
			if strings.HasPrefix(q[key][0], "!") {
				isNegative = true
				values = []string{}
				for _, v := range q[key] {
					if strings.HasPrefix(v, "!") {
						values = append(values, v[1:])
					} else {
						values = append(values, v)
					}
				}
			}
		}
		params = append(params, queryParam{
			keyMatch:    keyMatch,
			keyMatchLen: len(keyMatch),
			isWildCard:  isWildCard,
			isRegex:     isRegex,
			isNegative:  isNegative,
			values:      values,
			reg:         reg,
		})
	}

	return &Query{params: params}, nil
}

// Matches returns true if the given structured key matches the query.
func (q *Query) Matches(s string) bool {
	// Search forward in the given structured key. Since q.params are in
	// alphabetical order and structured keys have their params in alphabetical
	// order we can always search forward in the structured key, i.e. once
	// we've matched to a certain index in the string we can shorten the string
	// and only search the remaining chars.
	for _, part := range q.params {
		//  First find the key.
		keyIndex := strings.Index(s, part.keyMatch)
		if keyIndex == -1 {
			return false
		}
		// Truncate to the key.
		s = s[keyIndex+part.keyMatchLen:]
		if part.isWildCard {
			continue
		}
		// Extract the value string.
		valueIndex := strings.Index(s, ",")
		value := s[:valueIndex]
		if part.isRegex {
			if !part.reg.MatchString(value) {
				return false
			}
		} else if part.isNegative == util.In(value, part.values) {
			return false
		}
		// Truncate to the value.
		s = s[valueIndex:]
	}
	return true
}
