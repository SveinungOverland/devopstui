package ado

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"golang.org/x/sync/errgroup"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// maxDirectoryTeams bounds how many teams are scanned to build a project's
// people directory, so a large organisation cannot stall the picker.
const maxDirectoryTeams = 60

// People returns assignable users. The project directory (everyone on any
// of the project's teams) is cached after the first call; a non-empty
// query additionally searches the organisation's identities, so people
// outside the project's teams can be found.
func (s *SDK) People(ctx context.Context, project, query string) ([]model.Person, error) {
	dir, err := s.directory(ctx, project)
	if err != nil && len(dir) == 0 && query == "" {
		return nil, err
	}
	out := matchPeople(dir, query)
	if query != "" {
		if found := s.searchIdentities(ctx, query); len(found) > 0 {
			out = mergePeople(out, found, query)
		}
	}
	return out, nil
}

// directory is the union of every team's members in the project.
func (s *SDK) directory(ctx context.Context, project string) ([]model.Person, error) {
	s.mu.Lock()
	if p, ok := s.people[project]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	teams, err := s.Teams(ctx, project)
	if err != nil {
		return nil, err
	}
	if len(teams) > maxDirectoryTeams {
		teams = teams[:maxDirectoryTeams]
	}
	var (
		mu  sync.Mutex
		all []model.Person
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)
	for _, t := range teams {
		g.Go(func() error {
			members, err := s.core.GetTeamMembersWithExtendedProperties(gctx,
				core.GetTeamMembersWithExtendedPropertiesArgs{ProjectId: &project, TeamId: &t.ID})
			if err != nil {
				return nil // one unreadable team must not empty the picker
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range *members {
				if m.Identity == nil || deref(m.Identity.IsContainer) {
					continue
				}
				all = append(all, model.Person{
					DisplayName: deref(m.Identity.DisplayName),
					UniqueName:  deref(m.Identity.UniqueName),
				})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	all = dedupePeople(all)
	s.mu.Lock()
	s.people[project] = all
	s.mu.Unlock()
	return all, nil
}

// searchIdentities asks the organisation's identity service. It is best
// effort: a PAT without identity read simply yields nothing extra.
func (s *SDK) searchIdentities(ctx context.Context, query string) []model.Person {
	s.mu.Lock()
	c, tried := s.ident, s.identTried
	s.mu.Unlock()
	if c == nil {
		if tried {
			return nil
		}
		var err error
		c, err = identity.NewClient(ctx, s.conn)
		s.mu.Lock()
		s.identTried = true
		if err == nil {
			s.ident = c
		}
		s.mu.Unlock()
		if err != nil {
			return nil
		}
	}
	filter, membership := "General", identity.QueryMembershipValues.None
	res, err := c.ReadIdentities(ctx, identity.ReadIdentitiesArgs{
		SearchFilter: &filter, FilterValue: &query, QueryMembership: &membership,
	})
	if err != nil || res == nil {
		return nil
	}
	var out []model.Person
	for _, id := range *res {
		if deref(id.IsContainer) {
			continue
		}
		p := model.Person{DisplayName: deref(id.ProviderDisplayName)}
		if p.DisplayName == "" {
			p.DisplayName = deref(id.CustomDisplayName)
		}
		if id.Properties != nil {
			p.UniqueName = identityProperty(id.Properties, "Account", "Mail")
		}
		if p.DisplayName == "" && p.UniqueName == "" {
			continue
		}
		if p.DisplayName == "" {
			p.DisplayName = p.UniqueName
		}
		out = append(out, p)
	}
	return out
}

// identityProperty digs a string out of the identity property bag, which
// the API returns as {"Account": {"$type": "System.String", "$value": "…"}}.
func identityProperty(props any, keys ...string) string {
	m, ok := props.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if s, ok := v["$value"].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// matchPeople filters and ranks: prefix matches first, then substring,
// each alphabetically.
func matchPeople(people []model.Person, query string) []model.Person {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []model.Person
	for _, p := range people {
		if q == "" || personRank(p, q) > 0 {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := personRank(out[i], q), personRank(out[j], q)
		if ri != rj {
			return ri > rj
		}
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

// personRank is 3 for a name prefix, 2 for an address prefix, 1 for any
// substring (all words must appear), 0 for no match.
func personRank(p model.Person, q string) int {
	if q == "" {
		return 1
	}
	name, addr := strings.ToLower(p.DisplayName), strings.ToLower(p.UniqueName)
	switch {
	case strings.HasPrefix(name, q):
		return 3
	case addr != "" && strings.HasPrefix(addr, q):
		return 2
	}
	hay := name + " " + addr
	for _, w := range strings.Fields(q) {
		if !strings.Contains(hay, w) {
			return 0
		}
	}
	return 1
}

func dedupePeople(people []model.Person) []model.Person {
	seen := map[string]bool{}
	var out []model.Person
	for _, p := range people {
		if p.DisplayName == "" && p.UniqueName == "" {
			continue
		}
		k := p.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

// mergePeople appends search hits that the local list does not already
// hold, keeping the local (project) people first.
func mergePeople(local, found []model.Person, query string) []model.Person {
	seen := map[string]bool{}
	for _, p := range local {
		seen[p.Key()] = true
	}
	for _, p := range matchPeople(found, query) {
		if !seen[p.Key()] {
			seen[p.Key()] = true
			local = append(local, p)
		}
	}
	return local
}
