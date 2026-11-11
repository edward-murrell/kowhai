package notify

import (
	"slices"
	"testing"
)

func TestMentions(t *testing.T) {
	got := Mentions("Hi @Alice and @bob.smith, see `@notme` and email x@y.com. @alice again")
	want := []string{"alice", "bob.smith"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMatch(t *testing.T) {
	people := []Person{
		{ID: "1", Email: "alice@example.com", DisplayName: "Alice Smith"},
		{ID: "2", Email: "", DisplayName: "Bob Jones"},
		{ID: "3", Email: "carol@example.com", DisplayName: "Carol"},
	}
	got := Match([]string{"alice", "bobjones"}, people)
	if !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPrefs(t *testing.T) {
	p := ParsePrefs(`{"comment": false}`)
	if p.On("comment") || !p.On("mention") {
		t.Fatal("prefs wrong")
	}
}
