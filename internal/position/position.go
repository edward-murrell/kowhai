// Package position makes fractional index keys for ordering.
//
// Keys are strings over the base-62 digits 0-9A-Za-z, compared byte-wise.
// Between any two keys there is always another key, so moving an item
// only rewrites that item's key. Keys never end in "0".
package position

import (
	"errors"
	"strings"
)

const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// MaxLen is the key length that triggers a rebalance.
const MaxLen = 24

var ErrOrder = errors.New("position: a must sort before b")

// Between returns a key that sorts after a and before b. An empty a means
// "start" and an empty b means "end".
func Between(a, b string) (string, error) {
	if b != "" && a >= b {
		return "", ErrOrder
	}
	return midpoint(a, b), nil
}

// After returns a key after a.
func After(a string) string { return midpoint(a, "") }

// Before returns a key before b. b must not be empty.
func Before(b string) string { return midpoint("", b) }

func midpoint(a, b string) string {
	if b != "" {
		// Skip the common prefix, padding a with zeros.
		n := 0
		for n < len(b) {
			ca := byte('0')
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + midpoint(rest, b[n:])
		}
	}
	da := 0
	if a != "" {
		da = strings.IndexByte(digits, a[0])
	}
	db := len(digits)
	if b != "" {
		db = strings.IndexByte(digits, b[0])
	}
	if db-da > 1 {
		return string(digits[(da+db+1)/2])
	}
	// The first digits are next to each other.
	if b != "" && len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(digits[da]) + midpoint(rest, "")
}

// Spread returns n evenly spaced keys, used when rebalancing.
func Spread(n int) []string {
	keys := make([]string, n)
	// Two digits give 3843 slots, plenty for one list.
	width := 1
	for slots := len(digits) - 1; slots < n+1; slots = slots*len(digits) + len(digits) - 1 {
		width++
	}
	total := 1
	for i := 0; i < width; i++ {
		total *= len(digits)
	}
	step := total / (n + 1)
	for i := range keys {
		v := step * (i + 1)
		buf := make([]byte, width)
		for j := width - 1; j >= 0; j-- {
			buf[j] = digits[v%len(digits)]
			v /= len(digits)
		}
		keys[i] = strings.TrimRight(string(buf), "0")
	}
	return keys
}
