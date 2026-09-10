package ado

import (
	"testing"

	"github.com/sveinungoverland/devopstui/internal/model"
)

var dir = []model.Person{
	{DisplayName: "Alex Kim", UniqueName: "alex.kim@contoso.com"},
	{DisplayName: "Kari Nordmann", UniqueName: "kari.nordmann@contoso.com"},
	{DisplayName: "Ola Nordmann", UniqueName: "ola.nordmann@contoso.com"},
	{DisplayName: "Sveinung Øverland", UniqueName: "sveinung@contoso.com"},
}

func names(people []model.Person) []string {
	out := make([]string, len(people))
	for i, p := range people {
		out[i] = p.DisplayName
	}
	return out
}

func TestMatchPeople(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"Alex Kim", "Kari Nordmann", "Ola Nordmann", "Sveinung Øverland"}},
		{"nord", []string{"Kari Nordmann", "Ola Nordmann"}},   // substring of both
		{"kari", []string{"Kari Nordmann"}},                   // name prefix
		{"ola.nord", []string{"Ola Nordmann"}},                // address prefix
		{"kari nord", []string{"Kari Nordmann"}},              // all words must match
		{"contoso", []string{"Alex Kim", "Kari Nordmann", "Ola Nordmann", "Sveinung Øverland"}},
		{"nobody", nil},
	}
	for _, c := range cases {
		got := names(matchPeople(dir, c.query))
		if len(got) != len(c.want) {
			t.Errorf("%q → %v, want %v", c.query, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q → %v, want %v", c.query, got, c.want)
				break
			}
		}
	}
}

func TestMatchPeopleRanksPrefixFirst(t *testing.T) {
	people := []model.Person{
		{DisplayName: "Zoe Karisson", UniqueName: "zoe@x.com"},
		{DisplayName: "Kari Nordmann", UniqueName: "kari@x.com"},
	}
	if got := names(matchPeople(people, "kari")); got[0] != "Kari Nordmann" {
		t.Errorf("prefix match should rank first, got %v", got)
	}
}

func TestMergePeopleDedupes(t *testing.T) {
	local := []model.Person{{DisplayName: "Alex Kim", UniqueName: "alex.kim@contoso.com"}}
	found := []model.Person{
		{DisplayName: "Alex Kim", UniqueName: "ALEX.KIM@contoso.com"}, // same person, different case
		{DisplayName: "Alexa Ray", UniqueName: "alexa@contoso.com"},
	}
	got := names(mergePeople(local, found, "alex"))
	if len(got) != 2 || got[0] != "Alex Kim" || got[1] != "Alexa Ray" {
		t.Fatalf("merge = %v", got)
	}
}

func TestPersonAssignment(t *testing.T) {
	if v := (model.Person{DisplayName: "A", UniqueName: "a@x.com"}).Assignment(); v != "a@x.com" {
		t.Errorf("should assign by sign-in address, got %q", v)
	}
	if v := (model.Person{DisplayName: "A"}).Assignment(); v != "A" {
		t.Errorf("should fall back to the display name, got %q", v)
	}
}

func TestIdentityProperty(t *testing.T) {
	props := map[string]any{
		"Account": map[string]any{"$type": "System.String", "$value": "kari@contoso.com"},
		"Mail":    "ignored@contoso.com",
	}
	if got := identityProperty(props, "Account", "Mail"); got != "kari@contoso.com" {
		t.Errorf("got %q", got)
	}
	if got := identityProperty(map[string]any{"Mail": "m@x.com"}, "Account", "Mail"); got != "m@x.com" {
		t.Errorf("fallback key: got %q", got)
	}
	if got := identityProperty("not a map", "Account"); got != "" {
		t.Errorf("got %q", got)
	}
}
