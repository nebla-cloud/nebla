package httpapi

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/firewalls"
)

func apiFirewall(f firewalls.Firewall) apigen.Firewall {
	out := apigen.Firewall{Id: f.ID, Name: f.Name, Labels: f.Labels, Created: f.Created,
		Rules: make([]apigen.FirewallRule, len(f.Rules)), AppliedTo: make([]apigen.FirewallResource, len(f.AppliedTo))}
	for i, r := range f.Rules {
		ips := make([]string, len(r.IPs))
		for j, p := range r.IPs {
			ips[j] = p.String()
		}
		rule := apigen.FirewallRule{Direction: apigen.FirewallRuleDirection(r.Direction), Protocol: apigen.FirewallRuleProtocol(r.Protocol),
			Port: r.Port, Description: &r.Description}
		if r.Direction == "in" {
			rule.SourceIps = &ips
		} else {
			rule.DestinationIps = &ips
		}
		out.Rules[i] = rule
	}
	for i, r := range f.AppliedTo {
		res := apigen.FirewallResource{Type: apigen.FirewallResourceType(r.Type)}
		if r.ServerID != nil {
			res.Server = &struct {
				Id int64 `json:"id"`
			}{Id: *r.ServerID}
		}
		if r.Selector != nil {
			res.LabelSelector = &struct {
				Selector string `json:"selector"`
			}{Selector: *r.Selector}
			matched := make([]struct {
				Server struct {
					Id int64 `json:"id"`
				} `json:"server"`
				Type apigen.FirewallResourceAppliedToResourcesType `json:"type"`
			}, len(r.MatchedServerIDs))
			for j, id := range r.MatchedServerIDs {
				matched[j].Server.Id, matched[j].Type = id, firewalls.TypeServer
			}
			res.AppliedToResources = &matched
		}
		out.AppliedTo[i] = res
	}
	return out
}

func apiActions(as []actions.Action) []apigen.Action {
	out := make([]apigen.Action, len(as))
	for i, a := range as {
		out[i] = apiAction(a)
	}
	return out
}

func ruleInputs(rs []apigen.FirewallRule) ([]firewalls.RuleInput, error) {
	out := make([]firewalls.RuleInput, len(rs))
	for i, r := range rs {
		ips, err := firewalls.IPsFor(i, string(r.Direction), deref(r.SourceIps), deref(r.DestinationIps))
		if err != nil {
			return nil, err
		}
		out[i] = firewalls.RuleInput{Direction: string(r.Direction), Protocol: string(r.Protocol), Port: r.Port, IPs: ips,
			Description: deref(r.Description)}
	}
	return out, nil
}

func resourceInputs(rs []apigen.FirewallResource) []firewalls.ResourceInput {
	out := make([]firewalls.ResourceInput, len(rs))
	for i, r := range rs {
		out[i].Type = string(r.Type)
		if r.Server != nil {
			out[i].ServerID = &r.Server.Id
		}
		if r.LabelSelector != nil {
			out[i].Selector = &r.LabelSelector.Selector
		}
	}
	return out
}

func (s *server) GetFirewalls(ctx context.Context, req apigen.GetFirewallsRequestObject) (apigen.GetFirewallsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	f := firewalls.ListFilter{LabelSelector: deref(req.Params.LabelSelector), Name: deref(req.Params.Name)}
	fs, total, err := s.firewalls.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetFirewalls200JSONResponse{Firewalls: make([]apigen.Firewall, len(fs)), Meta: meta(page, perPage, total)}
	for i, f := range fs {
		out.Firewalls[i] = apiFirewall(f)
	}
	return out, nil
}

func (s *server) PostFirewalls(ctx context.Context, req apigen.PostFirewallsRequestObject) (apigen.PostFirewallsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	rules, err := ruleInputs(deref(b.Rules))
	if err != nil {
		return nil, err
	}
	in := firewalls.CreateInput{Name: b.Name, Rules: rules, ApplyTo: resourceInputs(deref(b.ApplyTo))}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	fw, as, err := s.firewalls.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	return apigen.PostFirewalls201JSONResponse{Firewall: apiFirewall(fw), Actions: apiActions(as)}, nil
}

func (s *server) GetFirewall(ctx context.Context, req apigen.GetFirewallRequestObject) (apigen.GetFirewallResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	fw, err := s.firewalls.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetFirewall200JSONResponse(apiFirewall(fw)), nil
}

func (s *server) PutFirewall(ctx context.Context, req apigen.PutFirewallRequestObject) (apigen.PutFirewallResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := firewalls.UpdateInput{Name: req.Body.Name}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	fw, err := s.firewalls.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return apigen.PutFirewall200JSONResponse(apiFirewall(fw)), nil
}

func (s *server) DeleteFirewall(ctx context.Context, req apigen.DeleteFirewallRequestObject) (apigen.DeleteFirewallResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	as, err := s.firewalls.Delete(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteFirewall200JSONResponse{ActionsCreatedJSONResponse: apigen.ActionsCreatedJSONResponse{Actions: apiActions(as)}}, nil
}

func (s *server) GetFirewallActions(ctx context.Context, req apigen.GetFirewallActionsRequestObject) (apigen.GetFirewallActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.firewalls.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	return apigen.GetFirewallActions200JSONResponse{Actions: apiActions(as), Meta: meta(page, perPage, total)}, nil
}

func (s *server) SetFirewallRules(ctx context.Context, req apigen.SetFirewallRulesRequestObject) (apigen.SetFirewallRulesResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := ruleInputs(req.Body.Rules)
	if err != nil {
		return nil, err
	}
	as, err := s.firewalls.SetRules(ctx, sc.ProjectID, req.Id, rules)
	if err != nil {
		return nil, err
	}
	return apigen.SetFirewallRules201JSONResponse{ActionsCreatedJSONResponse: apigen.ActionsCreatedJSONResponse{Actions: apiActions(as)}}, nil
}

func (s *server) ApplyFirewallToResources(ctx context.Context, req apigen.ApplyFirewallToResourcesRequestObject) (apigen.ApplyFirewallToResourcesResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	as, err := s.firewalls.ApplyTo(ctx, sc.ProjectID, req.Id, resourceInputs(req.Body.ApplyTo))
	if err != nil {
		return nil, err
	}
	return apigen.ApplyFirewallToResources201JSONResponse{ActionsCreatedJSONResponse: apigen.ActionsCreatedJSONResponse{Actions: apiActions(as)}}, nil
}

func (s *server) RemoveFirewallFromResources(ctx context.Context, req apigen.RemoveFirewallFromResourcesRequestObject) (apigen.RemoveFirewallFromResourcesResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	as, err := s.firewalls.RemoveFrom(ctx, sc.ProjectID, req.Id, resourceInputs(req.Body.RemoveFrom))
	if err != nil {
		return nil, err
	}
	return apigen.RemoveFirewallFromResources201JSONResponse{ActionsCreatedJSONResponse: apigen.ActionsCreatedJSONResponse{Actions: apiActions(as)}}, nil
}
