package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
)

// manifestDoc is a project's configuration as GET /projects/{pid}/manifest returns it. The manifest is kept as
// generic JSON so fields this program does not know survive an edit unchanged.
type manifestDoc struct {
	Manifest map[string]any `json:"manifest"`
	Revision int            `json:"revision"`
	ServerID string         `json:"server_id"`
}

func (t *Tools) manifest(ctx context.Context, projectID string) (*manifestDoc, error) {
	var m manifestDoc
	if err := t.client.Get(ctx, "/projects/"+api.P(projectID)+"/manifest", &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// service finds a service's entry in the manifest by ID, or by name for services saved before IDs.
func (m *manifestDoc) service(s *service) map[string]any {
	list, _ := m.Manifest["services"].([]any)
	for _, item := range list {
		svc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := svc["id"].(string); id != "" && id == s.ID {
			return svc
		}
		if name, _ := svc["name"].(string); s.ID == "" && name == s.Name {
			return svc
		}
	}
	return nil
}

// editEnv changes one service's environment and saves the project, retrying once if someone else saved in between.
func (t *Tools) editEnv(ctx context.Context, p *project, s *service, change func(env map[string]any) error) error {
	for attempt := 0; ; attempt++ {
		m, err := t.manifest(ctx, p.ID)
		if err != nil {
			return err
		}
		svc := m.service(s)
		if svc == nil {
			return fmt.Errorf("service %s is not in the project's configuration", s.Name)
		}
		env, _ := svc["env"].(map[string]any)
		if env == nil {
			env = map[string]any{}
		}
		if err := change(env); err != nil {
			return err
		}
		svc["env"] = env
		err = t.client.Do(ctx, "PUT", "/projects/"+api.P(p.ID), map[string]any{"manifest": m.Manifest, "server_id": m.ServerID, "revision": m.Revision}, nil)
		if api.IsStatus(err, 409) && attempt == 0 && strings.Contains(err.Error(), "reload") {
			continue // the configuration changed since we read it
		}
		return err
	}
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type queued struct {
	ID string `json:"id"`
}

func (t *Tools) deploy(ctx context.Context, in struct {
	Project         string `json:"project" jsonschema:"the project's name or ID"`
	Service         string `json:"service,omitempty" jsonschema:"only this service (name or ID); the whole project when empty"`
	AcknowledgeInit bool   `json:"acknowledge_init,omitempty" jsonschema:"set only after the user agreed to run the service's initialization commands, which can change data"`
}) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	path, what := "/projects/"+api.P(p.ID)+"/deploy", p.Name
	if in.Service != "" {
		s, err := p.service(in.Service)
		if err != nil {
			return "", err
		}
		path, what = "/projects/"+api.P(p.ID)+"/services/"+api.P(s.key())+"/deploy", p.Name+"/"+s.Name
	}
	var q queued
	err = t.client.Post(ctx, path, map[string]bool{"acknowledge_init": in.AcknowledgeInit}, &q)
	if api.IsStatus(err, 409) && strings.Contains(err.Error(), "Initialization commands") {
		return "", fmt.Errorf("%w. This deploy runs initialization commands that can change data. Ask the user; only if they agree, call deploy again with acknowledge_init=true", err)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Deploy of %s queued as job %s. Follow it with get_job (job=%q, wait_seconds=60).", what, q.ID, q.ID), nil
}

func (t *Tools) rollback(ctx context.Context, in struct {
	Project    string `json:"project" jsonschema:"the project's name or ID"`
	Service    string `json:"service,omitempty" jsonschema:"roll back only this service (name or ID); the whole project when empty"`
	Deployment string `json:"deployment" jsonschema:"the ID of an earlier successful deployment, from list_deployments"`
}) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(in.Deployment)
	if target == "" {
		return "", fmt.Errorf("name the deployment to return to (an ID from list_deployments)")
	}
	path, what := "/projects/"+api.P(p.ID)+"/rollback", p.Name
	if in.Service != "" {
		s, err := p.service(in.Service)
		if err != nil {
			return "", err
		}
		path, what = "/projects/"+api.P(p.ID)+"/services/"+api.P(s.key())+"/rollback", p.Name+"/"+s.Name
	}
	var q queued
	if err := t.client.Post(ctx, path, map[string]string{"target_job": target}, &q); err != nil {
		return "", err
	}
	return fmt.Sprintf("Rollback of %s to %s queued as job %s. Follow it with get_job.", what, target, q.ID), nil
}

func (t *Tools) restart(ctx context.Context, in serviceIn) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	var q queued
	if err := t.client.Post(ctx, "/projects/"+api.P(p.ID)+"/services/"+api.P(s.key())+"/restart", nil, &q); err != nil {
		return "", err
	}
	if q.ID == "" {
		return fmt.Sprintf("Restarting %s/%s.", p.Name, s.Name), nil
	}
	return fmt.Sprintf("Restart of %s/%s queued as job %s.", p.Name, s.Name, q.ID), nil
}

func (t *Tools) setEnv(ctx context.Context, in struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
	Service string `json:"service" jsonschema:"the service's name or ID"`
	Name    string `json:"name" jsonschema:"the variable's name, like DATABASE_URL"`
	Value   string `json:"value" jsonschema:"the value; it is stored encrypted and never shown back. ${OTHER} references another variable"`
	Deploy  bool   `json:"deploy,omitempty" jsonschema:"deploy the service right after saving, so the change takes effect now"`
}) (string, error) {
	if !envName.MatchString(in.Name) {
		return "", fmt.Errorf("%q is not a valid variable name (letters, digits and _, not starting with a digit)", in.Name)
	}
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	existed := false
	if err := t.editEnv(ctx, p, s, func(env map[string]any) error {
		_, existed = env[in.Name]
		env[in.Name] = in.Value
		return nil
	}); err != nil {
		return "", err
	}
	verb := map[bool]string{true: "Updated", false: "Added"}[existed]
	msg := fmt.Sprintf("%s %s on %s/%s (value hidden).", verb, in.Name, p.Name, s.Name)
	if !in.Deploy {
		return msg + " It takes effect on the next deploy of this service.", nil
	}
	var q queued
	if err := t.client.Post(ctx, "/projects/"+api.P(p.ID)+"/services/"+api.P(s.key())+"/deploy", map[string]bool{}, &q); err != nil {
		return "", fmt.Errorf("%s But the deploy did not start: %w", msg, t.explain(err))
	}
	return fmt.Sprintf("%s Deploy queued as job %s; follow it with get_job.", msg, q.ID), nil
}

func (t *Tools) unsetEnv(ctx context.Context, in struct {
	Project string `json:"project" jsonschema:"the project's name or ID"`
	Service string `json:"service" jsonschema:"the service's name or ID"`
	Name    string `json:"name" jsonschema:"the variable's name"`
}) (string, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return "", err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return "", err
	}
	if err := t.editEnv(ctx, p, s, func(env map[string]any) error {
		if _, ok := env[in.Name]; !ok {
			return fmt.Errorf("%s/%s has no variable %s", p.Name, s.Name, in.Name)
		}
		delete(env, in.Name)
		return nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed %s from %s/%s. It takes effect on the next deploy of this service.", in.Name, p.Name, s.Name), nil
}
