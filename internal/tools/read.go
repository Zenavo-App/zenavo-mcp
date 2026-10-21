package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
)

type projectIn struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
}

type serviceIn struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
	Service string `json:"service" jsonschema:"the service's name or ID"`
}

func (t *Tools) whoami(ctx context.Context, _ struct{}) (string, error) {
	var m me
	if err := t.client.Get(ctx, "/auth/me", &m); err != nil {
		return "", err
	}
	scope := t.scope
	if scope == "" {
		scope = "unknown (a key from ZENAVO_MCP_TOKEN)"
	}
	teams := make([]map[string]any, 0, len(m.Teams))
	for _, tm := range m.Teams {
		teams = append(teams, map[string]any{"id": tm.ID, "name": tm.Name, "role": tm.Role, "personal": tm.Personal})
	}
	return asJSON(map[string]any{"email": m.User.Email, "control_plane": t.client.Base, "permission": scope, "teams": teams,
		"note": "A team role shown here is already capped by the key's permission (viewer for read, member for deploy)."})
}

func (t *Tools) listProjects(ctx context.Context, in struct {
	Team string `json:"team,omitempty" jsonschema:"only this team's projects (name or ID); all teams when empty"`
}) (string, error) {
	list, teamNames, err := t.projects(ctx, in.Team)
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		services := make([]map[string]any, 0, len(p.Services))
		for _, s := range p.Services {
			services = append(services, map[string]any{"name": s.Name, "id": s.ID, "source": s.Source})
		}
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "team": teamNames[p.TeamID], "running": p.Running,
			"deployed": p.RuntimeJob != nil || p.ActiveJob != nil, "services": services})
	}
	if len(out) == 0 {
		return "No projects that this key can see.", nil
	}
	return asJSON(out)
}

type serviceState struct {
	Service     string  `json:"Service"`
	State       string  `json:"State"`
	Health      *string `json:"Health"`
	Ready       int     `json:"Ready"`
	Desired     int     `json:"Desired"`
	Restarts    int     `json:"Restarts"`
	CPUMilli    *int64  `json:"CPUMilli"`
	MemoryBytes *int64  `json:"MemoryBytes"`
}

func (t *Tools) getProject(ctx context.Context, in projectIn) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	var rt struct {
		Services []serviceState `json:"services"`
		Deployed bool           `json:"deployed"`
	}
	runtimeErr := t.client.Get(ctx, "/projects/"+api.P(p.ID)+"/runtime", &rt)
	live := map[string]serviceState{}
	for _, st := range rt.Services {
		live[st.Service] = st
	}
	services := make([]map[string]any, 0, len(p.Services))
	for _, s := range p.Services {
		row := map[string]any{"name": s.Name, "id": s.ID, "source": s.Source}
		var ports []map[string]any
		for _, pt := range s.Ports {
			e := map[string]any{"port": pt.Port, "type": pt.Type}
			if pt.Domain != nil {
				e["url"] = "https://" + *pt.Domain
			}
			ports = append(ports, e)
		}
		if ports != nil {
			row["ports"] = ports
		}
		if st, ok := live[s.key()]; ok {
			state := map[string]any{"state": st.State, "ready": fmt.Sprintf("%d/%d", st.Ready, st.Desired), "restarts": st.Restarts}
			if st.Health != nil {
				state["health"] = *st.Health
			}
			if st.CPUMilli != nil {
				state["cpu_millicores"] = *st.CPUMilli
			}
			if st.MemoryBytes != nil {
				state["memory_mb"] = *st.MemoryBytes / (1 << 20)
			}
			row["live"] = state
		}
		services = append(services, row)
	}
	out := map[string]any{"id": p.ID, "name": p.Name, "running": p.Running, "services": services,
		"ready_to_deploy": p.Ready, "blockers": p.Blockers, "notes": p.Notes}
	if p.ActiveJob != nil {
		out["active_job"] = *p.ActiveJob
	}
	if runtimeErr != nil {
		out["live_state_unavailable"] = t.explain(runtimeErr).Error()
	} else if !rt.Deployed {
		out["live_state_unavailable"] = "not deployed yet"
	}
	return asJSON(out)
}

const maxLogLines = 200

func (t *Tools) getLogs(ctx context.Context, in struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
	Service string `json:"service" jsonschema:"the service's name or ID"`
	Lines   int    `json:"lines,omitempty" jsonschema:"how many of the newest lines (1 to 200, default 100)"`
	Contain string `json:"contains,omitempty" jsonschema:"only lines containing this text (case-insensitive)"`
}) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	n := in.Lines
	if n <= 0 {
		n = 100
	}
	n = min(n, maxLogLines)
	var out struct {
		Text string `json:"text"`
	}
	if err := t.client.Get(ctx, "/projects/"+api.P(p.ID)+"/logs/"+api.P(s.key()), &out); err != nil {
		return "", err
	}
	text := out.Text
	if in.Contain != "" {
		var keep []string
		needle := strings.ToLower(in.Contain)
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				keep = append(keep, line)
			}
		}
		text = strings.Join(keep, "\n")
	}
	text = lastLines(text, n)
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("No log lines from %s/%s%s.", p.Name, s.Name, map[bool]string{true: " matching " + fmt.Sprintf("%q", in.Contain)}[in.Contain != ""]), nil
	}
	return fence(fmt.Sprintf("Logs of %s/%s (newest last):", p.Name, s.Name), text), nil
}

func (t *Tools) listDeployments(ctx context.Context, in struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
	Service string `json:"service" jsonschema:"the service's name or ID"`
	Limit   int    `json:"limit,omitempty" jsonschema:"how many (1 to 30, default 10)"`
}) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 30)
	var list []struct {
		ID       string   `json:"id"`
		Kind     string   `json:"kind"`
		Status   string   `json:"status"`
		Created  float64  `json:"created"`
		Finished *float64 `json:"finished"`
		Scope    string   `json:"scope"`
		Current  bool     `json:"current"`
		Actor    string   `json:"actor"`
		Trigger  string   `json:"trigger"`
	}
	if err := t.client.Get(ctx, fmt.Sprintf("/projects/%s/services/%s/deployments?limit=%d", api.P(p.ID), api.P(s.key()), limit), &list); err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		e := map[string]any{"id": d.ID, "kind": d.Kind, "status": d.Status, "started": when(d.Created), "of": d.Scope, "current": d.Current}
		if d.Finished != nil {
			e["finished"] = when(*d.Finished)
		}
		if d.Actor != "" {
			e["by"] = d.Actor
		}
		if d.Trigger != "" {
			e["trigger"] = d.Trigger
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return fmt.Sprintf("%s/%s has not been deployed yet.", p.Name, s.Name), nil
	}
	return asJSON(out)
}

func when(unix float64) string {
	return time.Unix(int64(unix), 0).UTC().Format(time.RFC3339)
}

type job struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	Kind      string            `json:"kind"`
	Status    string            `json:"status"`
	Created   float64           `json:"created"`
	Finished  *float64          `json:"finished"`
	Log       *string           `json:"log"`
	Progress  map[string]string `json:"progress"`
}

func (j *job) done() bool { return j.Status != "queued" && j.Status != "running" }

// pollEvery is how often get_job checks a job it waits for; tests shorten it.
var pollEvery = 3 * time.Second

const maxWait = 120

func (t *Tools) getJob(ctx context.Context, in struct {
	Job         string `json:"job" jsonschema:"the job ID (job_…)"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"wait up to this many seconds (at most 120) for the job to finish"`
}) (string, error) {
	id := strings.TrimSpace(in.Job)
	if id == "" {
		return "", fmt.Errorf("name a job ID")
	}
	deadline := time.Now().Add(time.Duration(min(max(in.WaitSeconds, 0), maxWait)) * time.Second)
	var j job
	for {
		if err := t.client.Get(ctx, "/jobs/"+api.P(id), &j); err != nil {
			return "", err
		}
		if j.done() || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollEvery):
		}
	}
	head := map[string]any{"id": j.ID, "kind": j.Kind, "status": j.Status, "started": when(j.Created)}
	if j.Finished != nil {
		head["finished"] = when(*j.Finished)
	}
	if len(j.Progress) > 0 {
		head["progress"] = j.Progress
	}
	if !j.done() {
		head["note"] = "Still " + j.Status + "; call get_job again (with wait_seconds) to follow it."
	}
	summary, err := asJSON(head)
	if err != nil {
		return "", err
	}
	if j.Log == nil || strings.TrimSpace(*j.Log) == "" {
		return summary, nil
	}
	log, cut := tail(*j.Log, 6000)
	what := "Job log"
	if cut {
		what = "End of the job log"
	}
	return summary + "\n\n" + fence(what+":", log), nil
}

// reference matches ${NAME}: a value made only of references names other variables and is not secret.
var reference = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

func onlyReferences(v string) bool {
	return reference.MatchString(v) && strings.TrimSpace(reference.ReplaceAllString(v, "")) == ""
}

func (t *Tools) listEnv(ctx context.Context, in serviceIn) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	m, err := t.manifest(ctx, p.ID)
	if err != nil {
		return "", err
	}
	svc := m.service(s)
	if svc == nil {
		return "", fmt.Errorf("service %s is not in the project's configuration", s.Name)
	}
	env, _ := svc["env"].(map[string]any)
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]map[string]string, 0, len(names))
	for _, k := range names {
		v, _ := env[k].(string)
		e := map[string]string{"name": k}
		switch {
		case v == "":
			e["value"] = "(empty)"
		case onlyReferences(v):
			e["value"] = v
		default:
			e["value"] = "(set, hidden)"
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return fmt.Sprintf("%s/%s has no environment variables of its own.", p.Name, s.Name), nil
	}
	return asJSON(out)
}
