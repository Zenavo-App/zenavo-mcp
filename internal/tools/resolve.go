package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
)

type team struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"`
	Role     string `json:"role"`
}

type me struct {
	User struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Teams []team `json:"teams"`
}

type port struct {
	ID     string  `json:"id"`
	Port   int     `json:"port"`
	Type   string  `json:"type"`
	Domain *string `json:"domain"`
}

type service struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Variables []string `json:"variables"`
	Ports     []port   `json:"ports"`
}

// key is the service's ID, which API routes take; services saved before IDs existed only have a name.
func (s service) key() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Name
}

type project struct {
	ID         string    `json:"id"`
	TeamID     string    `json:"team_id"`
	Name       string    `json:"name"`
	Revision   int       `json:"revision"`
	ActiveJob  *string   `json:"active_job"`
	RuntimeJob *string   `json:"runtime_job"`
	Running    bool      `json:"running"`
	Services   []service `json:"services"`
	Blockers   []string  `json:"blockers"`
	Notes      []string  `json:"notes"`
	Ready      bool      `json:"ready"`
}

// teams lists the teams the key can see.
func (t *Tools) teams(ctx context.Context) ([]team, error) {
	var m me
	if err := t.client.Get(ctx, "/auth/me", &m); err != nil {
		return nil, err
	}
	return m.Teams, nil
}

// projects lists every project the key can see, in one team or all of them. A key limited to some projects
// only gets those back from the API.
func (t *Tools) projects(ctx context.Context, teamRef string) ([]project, map[string]string, error) {
	teams, err := t.teams(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	var out []project
	matched := false
	for _, tm := range teams {
		names[tm.ID] = tm.Name
		if teamRef != "" && tm.ID != teamRef && !strings.EqualFold(tm.Name, teamRef) {
			continue
		}
		matched = true
		var list []project
		if err := t.client.Get(ctx, "/projects?team_id="+url.QueryEscape(tm.ID), &list); err != nil {
			return nil, nil, err
		}
		out = append(out, list...)
	}
	if teamRef != "" && !matched {
		return nil, nil, fmt.Errorf("no team %q; whoami lists the teams this key can see", teamRef)
	}
	return out, names, nil
}

// project finds a project by ID or name. A name must be unique among the projects the key can see.
func (t *Tools) project(ctx context.Context, ref string) (*project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("name a project (its name or ID; list_projects shows them)")
	}
	if strings.HasPrefix(ref, "proj_") {
		var p project
		if err := t.client.Get(ctx, "/projects/"+api.P(ref), &p); err == nil {
			return &p, nil
		} else if !api.IsStatus(err, 404) {
			return nil, err
		}
	}
	all, _, err := t.projects(ctx, "")
	if err != nil {
		return nil, err
	}
	var found []project
	for _, p := range all {
		if p.ID == ref || strings.EqualFold(p.Name, ref) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no project %q that this key can see; list_projects shows them", ref)
	case 1:
		return &found[0], nil
	}
	ids := make([]string, 0, len(found))
	for _, p := range found {
		ids = append(ids, p.ID)
	}
	return nil, fmt.Errorf("%d projects are named %q (%s); name one by its ID", len(found), ref, strings.Join(ids, ", "))
}

// service finds a service of a project by ID or name.
func (p *project) service(ref string) (*service, error) {
	ref = strings.TrimSpace(ref)
	for i := range p.Services {
		if s := &p.Services[i]; s.ID == ref || strings.EqualFold(s.Name, ref) {
			return s, nil
		}
	}
	names := make([]string, 0, len(p.Services))
	for _, s := range p.Services {
		names = append(names, s.Name)
	}
	if ref == "" {
		return nil, fmt.Errorf("name a service of %s: %s", p.Name, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("project %s has no service %q; it has: %s", p.Name, ref, strings.Join(names, ", "))
}
