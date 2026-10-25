package service

import (
	"errors"
	"slices"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
)

// eflow: 访问令牌权限目录。JSON 形状与 scope 字符串尽量与上游 QuantumNous/new-api
// service/access_token_scope.go 一致（personal / admin / system 三组，resource:action），
// 但 eflow 没有 Casbin：每个动作按角色写死最低要求（minRole），并覆盖 eflow 的客服(5)、
// 财务(7)角色与工单、风控、监控、批量返还等独有资源。

const (
	AccessTokenGroupPersonal = "personal"
	AccessTokenGroupAdmin    = "admin"
	AccessTokenGroupSystem   = "system"

	AccessTokenMaxPerUser        = 20
	AccessTokenDefaultExpiryDays = 30
	accessTokenScopeSeparator    = ":"
)

var (
	ErrAccessTokenScopeInvalid   = errors.New("access token scope is invalid")
	ErrAccessTokenScopeForbidden = errors.New("access token scope cannot be granted")
)

// ActionDefinition is one grantable action of a resource (same JSON as upstream authz.ActionDefinition).
type ActionDefinition struct {
	Action         string `json:"action"`
	LabelKey       string `json:"label_key"`
	DescriptionKey string `json:"description_key,omitempty"`
	minRole        int
}

// AccessTokenResource shares one JSON shape with upstream so the frontend
// permission matrix can render both.
type AccessTokenResource struct {
	Resource string             `json:"resource"`
	LabelKey string             `json:"label_key"`
	Actions  []ActionDefinition `json:"actions"`
	group    string
}

type AccessTokenCatalogGroup struct {
	Group     string                `json:"group"`
	Resources []AccessTokenResource `json:"resources"`
}

func readAction(minRole int, description string) ActionDefinition {
	return ActionDefinition{Action: "read", LabelKey: "View", DescriptionKey: description, minRole: minRole}
}

func writeAction(minRole int, description string) ActionDefinition {
	return ActionDefinition{Action: "write", LabelKey: "Edit", DescriptionKey: description, minRole: minRole}
}

const (
	roleUser    = common.RoleCommonUser
	roleSupport = common.RoleCustomerServiceUser
	roleAdmin   = common.RoleAdminUser
	roleRoot    = common.RoleRootUser
)

var accessTokenResources = []AccessTokenResource{
	// personal
	{Resource: "profile", LabelKey: "Profile", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View your profile, groups, and available models."),
		writeAction(roleUser, "Change your display name and personal settings."),
	}},
	{Resource: "api_key", LabelKey: "API keys", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View API keys and their settings without the full key."),
		writeAction(roleUser, "Create, edit, and delete API keys."),
		{Action: "reveal", LabelKey: "Reveal full keys", DescriptionKey: "View complete API keys.", minRole: roleUser},
	}},
	{Resource: "usage", LabelKey: "Usage", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View your usage logs, statistics, tasks, and audit logs."),
	}},
	{Resource: "wallet", LabelKey: "Wallet", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View balance, top-up records, invitation codes, and referral information."),
		writeAction(roleUser, "Top up, check in, create invitation codes, and transfer referral rewards."),
	}},
	{Resource: "account_security", LabelKey: "Account security", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View Passkey, two-factor, and sign-in binding status."),
		writeAction(roleUser, "Includes changes that require security verification, such as password, two-factor settings, and account deletion."),
	}},
	{Resource: "ticket", LabelKey: "Support tickets", group: AccessTokenGroupPersonal, Actions: []ActionDefinition{
		readAction(roleUser, "View your support, invoice, and refund tickets."),
		writeAction(roleUser, "Create tickets, reply, upload attachments, and close tickets."),
	}},
	// admin
	{Resource: "user", LabelKey: "Users", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleSupport, "View users, their sign-in bindings, and profile summaries."),
		writeAction(roleAdmin, "Create, edit, enable, disable, and delete users, adjust quota, and reset their security settings."),
	}},
	{Resource: "billing", LabelKey: "Billing management", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleSupport, "View all top-up records."),
		writeAction(roleAdmin, "Complete top-up orders."),
	}},
	{Resource: "ticket_admin", LabelKey: "Ticket management", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleSupport, "View all tickets, invoices, refunds, and staff."),
		writeAction(roleSupport, "Reply to, assign, and change the status of tickets, invoices, and refunds."),
	}},
	{Resource: "channel", LabelKey: "Channels", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View channels, their models, and scheduler state."),
		{Action: "operate", LabelKey: "Operate", DescriptionKey: "Test channels, refresh balances, fetch models, and enable or disable channels.", minRole: roleAdmin},
		writeAction(roleAdmin, "Edit channel settings and models."),
		{Action: "sensitive_write", LabelKey: "Edit sensitive settings", DescriptionKey: "Create, copy, and delete channels and change keys, addresses, and types.", minRole: roleAdmin},
		{Action: "secret_view", LabelKey: "View keys", DescriptionKey: "View complete channel keys after security verification.", minRole: roleRoot},
	}},
	{Resource: "model", LabelKey: "Models", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View models and vendors, and preview upstream sync."),
		writeAction(roleAdmin, "Edit models and vendors, and sync them from upstream."),
	}},
	{Resource: "deployment", LabelKey: "Deployments", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View deployments and price estimates."),
		writeAction(roleAdmin, "Create, edit, and delete deployments, and test connections."),
	}},
	{Resource: "log", LabelKey: "Logs", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View all users' usage logs, statistics, and tasks."),
	}},
	{Resource: "redemption", LabelKey: "Redemption codes", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View redemption codes."),
		writeAction(roleAdmin, "Create, edit, and delete redemption codes."),
	}},
	{Resource: "invitation_code", LabelKey: "Invitation codes", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View invitation codes and their usage."),
		writeAction(roleAdmin, "Create, edit, and delete invitation codes."),
	}},
	{Resource: "group", LabelKey: "Groups", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View groups and prefill groups."),
		writeAction(roleAdmin, "Create, edit, and delete prefill groups."),
	}},
	{Resource: "risk", LabelKey: "Risk center", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View risk rules, incidents, moderation, and enforcement."),
		writeAction(roleAdmin, "Change risk and moderation rules, unblock subjects, and lift enforcement."),
	}},
	{Resource: "monitoring", LabelKey: "Group monitoring", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View group monitoring data and history."),
		writeAction(roleAdmin, "Refresh monitoring data and delete monitoring records."),
	}},
	{Resource: "audit", LabelKey: "Audit logs", group: AccessTokenGroupAdmin, Actions: []ActionDefinition{
		readAction(roleAdmin, "View all users' audit logs."),
	}},
	// system
	{Resource: "option", LabelKey: "System settings", group: AccessTokenGroupSystem, Actions: []ActionDefinition{
		readAction(roleRoot, "View system settings, custom OAuth providers, and price sync sources."),
		writeAction(roleRoot, "Change system settings and custom OAuth providers, and sync prices."),
	}},
	{Resource: "plugin", LabelKey: "Task plugins", group: AccessTokenGroupSystem, Actions: []ActionDefinition{
		readAction(roleRoot, "View installed task plugins, bindings, and reviews."),
		writeAction(roleRoot, "Install, configure, bind, test, and remove task plugins."),
	}},
	{Resource: "ops", LabelKey: "Operations", group: AccessTokenGroupSystem, Actions: []ActionDefinition{
		readAction(roleRoot, "View performance statistics and log files."),
		writeAction(roleRoot, "Run maintenance operations such as cache cleanup and GC."),
	}},
	{Resource: "usage_refund", LabelKey: "Usage refunds", group: AccessTokenGroupSystem, Actions: []ActionDefinition{
		readAction(roleRoot, "Preview usage refunds and view refund batches."),
		writeAction(roleRoot, "Execute usage refund batches."),
	}},
}

// AccessTokenCatalog lists the scopes a user with role may grant, grouped for
// display. Resources without a grantable action and empty groups are omitted.
func AccessTokenCatalog(role int) []AccessTokenCatalogGroup {
	resources := map[string][]AccessTokenResource{}
	for _, resource := range accessTokenResources {
		actions := make([]ActionDefinition, 0, len(resource.Actions))
		for _, action := range resource.Actions {
			if role >= action.minRole {
				actions = append(actions, action)
			}
		}
		if len(actions) == 0 {
			continue
		}
		resource.Actions = actions
		resources[resource.group] = append(resources[resource.group], resource)
	}
	groups := make([]AccessTokenCatalogGroup, 0, 3)
	for _, group := range []string{AccessTokenGroupPersonal, AccessTokenGroupAdmin, AccessTokenGroupSystem} {
		if len(resources[group]) > 0 {
			groups = append(groups, AccessTokenCatalogGroup{Group: group, Resources: resources[group]})
		}
	}
	return groups
}

// AccessTokenScopeDictionary lists every resource a token scope can name,
// whatever the viewer may grant, so stored grants keep their labels.
func AccessTokenScopeDictionary() []AccessTokenResource {
	resources := make([]AccessTokenResource, 0, len(accessTokenResources))
	for _, resource := range accessTokenResources {
		resource.Actions = slices.Clone(resource.Actions)
		resources = append(resources, resource)
	}
	return resources
}

// NormalizeAccessTokenScopes validates a requested grant against the catalog
// for role and returns it trimmed, de-duplicated and sorted.
func NormalizeAccessTokenScopes(role int, scopes []string) ([]string, error) {
	normalized, ok := NormalizeAccessTokenScopeList(scopes)
	if !ok {
		return nil, ErrAccessTokenScopeInvalid
	}
	grantable := make(map[string]bool)
	for _, group := range AccessTokenCatalog(role) {
		for _, resource := range group.Resources {
			for _, action := range resource.Actions {
				grantable[resource.Resource+accessTokenScopeSeparator+action.Action] = true
			}
		}
	}
	for _, scope := range normalized {
		if grantable[scope] {
			continue
		}
		if !KnownAccessTokenScope(scope) {
			return nil, ErrAccessTokenScopeInvalid
		}
		return nil, ErrAccessTokenScopeForbidden
	}
	return normalized, nil
}

// KnownAccessTokenScope reports whether scope names an action in the catalog.
func KnownAccessTokenScope(scope string) bool {
	resourceName, action, ok := strings.Cut(scope, accessTokenScopeSeparator)
	if !ok {
		return false
	}
	for _, resource := range accessTokenResources {
		if resource.Resource == resourceName && slices.ContainsFunc(resource.Actions, func(definition ActionDefinition) bool { return definition.Action == action }) {
			return true
		}
	}
	return false
}

// AccessTokenScopeGranted reports whether a grant contains scope. A scope the
// owner's current role no longer allows is not granted, so demoting a user
// immediately narrows their tokens.
func AccessTokenScopeGranted(scopes []string, scope string, role int) bool {
	if scope == "" || !slices.Contains(scopes, scope) {
		return false
	}
	resourceName, action, _ := strings.Cut(scope, accessTokenScopeSeparator)
	for _, resource := range accessTokenResources {
		if resource.Resource != resourceName {
			continue
		}
		for _, definition := range resource.Actions {
			if definition.Action == action {
				return role >= definition.minRole
			}
		}
	}
	return false
}

func init() {
	lookupAccessTokenScopes = func(identity AuthIdentity) ([]string, bool) {
		tokenID, ok := model.ParseAccessTokenSessionID(identity.SessionID)
		if !ok {
			return nil, false
		}
		token, err := model.GetUserAccessToken(identity.UserID, tokenID)
		if err != nil {
			return nil, false
		}
		user, err := model.GetUserCache(identity.UserID)
		if err != nil {
			return nil, false
		}
		granted := make([]string, 0, len(token.GetScopes()))
		for _, scope := range token.GetScopes() {
			if AccessTokenScopeGranted(token.GetScopes(), scope, user.Role) {
				granted = append(granted, scope)
			}
		}
		return granted, true
	}
}
