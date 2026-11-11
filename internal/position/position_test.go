package position

import (
	"math/rand"
	"slices"
	"testing"
)

func TestBetween(t *testing.T) {
	cases := [][2]string{{"", ""}, {"", "V"}, {"V", ""}, {"a", "b"}, {"a0V", "a1"}, {"zz", ""}, {"", "01"}, {"a", "a1"}}
	for _, c := range cases {
		k, err := Between(c[0], c[1])
		if err != nil {
			t.Fatalf("%q %q: %v", c[0], c[1], err)
		}
		if k <= c[0] || (c[1] != "" && k >= c[1]) {
			t.Errorf("Between(%q, %q) = %q", c[0], c[1], k)
		}
		if k[len(k)-1] == '0' {
			t.Errorf("Between(%q, %q) = %q ends in 0", c[0], c[1], k)
		}
	}
	if _, err := Between("b", "a"); err == nil {
		t.Error("want error for reversed keys")
	}
}

func TestRandomInserts(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	keys := []string{After("")}
	for i := 0; i < 2000; i++ {
		j := r.Intn(len(keys) + 1)
		a, b := "", ""
		if j > 0 {
			a = keys[j-1]
		}
		if j < len(keys) {
			b = keys[j]
		}
		k, err := Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		keys = slices.Insert(keys, j, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys not sorted")
	}
	if len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Fatal("duplicate keys")
	}
}

func TestSpread(t *testing.T) {
	for _, n := range []int{1, 5, 61, 62, 500, 5000} {
		keys := Spread(n)
		if len(keys) != n || !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != n {
			t.Fatalf("Spread(%d) bad", n)
		}
		if _, err := Between(keys[n-1], ""); err != nil {
			t.Fatal(err)
		}
	}
}

// The web app has a copy of this algorithm in web/src/lib/position.ts.
// These values are checked there too.
func TestMatchesWebClient(t *testing.T) {
	for _, c := range [][3]string{{"", "", "V"}, {"V", "", "l"}, {"", "V", "G"}} {
		if got := midpoint(c[0], c[1]); got != c[2] {
			t.Errorf("midpoint(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
