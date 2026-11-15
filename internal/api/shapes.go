package api

import (
	"encoding/json"

	"mostlydone/internal/store"
)

// Wire shapes. Ids are kept as plain strings (or nil) so expand can find and
// replace them. Out-of-scope Plane fields carry Plane's default value.

func str(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func rawJSON(p *string, def any) any {
	if p == nil {
		return def
	}
	return json.RawMessage(*p)
}

func userLite(u *store.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{"id": u.ID, "first_name": u.FirstName, "last_name": u.LastName, "email": u.Email,
		"avatar": "", "avatar_url": nil, "display_name": u.DisplayName}
}

func memberJSON(m *store.Member) map[string]any {
	o := userLite(&m.User)
	o["role"] = m.Role
	return o
}

func projectLite(p *store.Project) map[string]any {
	return map[string]any{"id": p.ID, "identifier": p.Identifier, "name": p.Name, "cover_image": str(p.CoverImage),
		"icon_prop": rawJSON(p.IconProp, nil), "emoji": str(p.Emoji), "description": p.Description,
		"cover_image_url": str(p.CoverImage), "archived_at": nil}
}

func projectJSON(p *store.Project, role int) map[string]any {
	return map[string]any{
		"id": p.ID, "workspace": p.WorkspaceID, "name": p.Name, "identifier": p.Identifier, "description": p.Description,
		"description_text": nil, "description_html": nil, "network": p.Network,
		"project_lead": str(p.ProjectLead), "default_assignee": str(p.DefaultAssignee), "default_state": str(p.DefaultState),
		"emoji": str(p.Emoji), "icon_prop": rawJSON(p.IconProp, nil), "logo_props": map[string]any{},
		"cover_image": str(p.CoverImage), "cover_image_url": str(p.CoverImage), "cover_image_asset": nil,
		"module_view": p.ModuleView, "cycle_view": p.CycleView, "issue_views_view": p.IssueViewsView,
		"page_view": p.PageView, "intake_view": p.IntakeView,
		"is_time_tracking_enabled": p.IsTimeTrackingEnabled, "is_issue_type_enabled": p.IsIssueTypeEnabled,
		"guest_view_all_features": p.GuestViewAllFeatures, "archive_in": p.ArchiveIn, "close_in": p.CloseIn,
		"timezone": p.Timezone, "estimate": nil, "archived_at": nil,
		"external_source": str(p.ExternalSource), "external_id": str(p.ExternalID),
		"total_members": p.TotalMembers, "total_cycles": p.TotalCycles, "total_modules": p.TotalModules,
		"is_member": true, "member_role": role, "is_deployed": false, "sort_order": nil,
		"created_by": str(p.CreatedBy), "updated_by": str(p.UpdatedBy),
		"created_at": p.CreatedAt, "updated_at": p.UpdatedAt, "deleted_at": str(p.DeletedAt),
	}
}

func stateLite(s *store.State) map[string]any {
	return map[string]any{"id": s.ID, "name": s.Name, "color": s.Color, "group": s.Group}
}

func stateJSON(s *store.State, wsID string) map[string]any {
	return map[string]any{
		"id": s.ID, "name": s.Name, "color": s.Color, "group": s.Group, "description": s.Description,
		"sequence": s.Sequence, "default": s.Default, "slug": s.Slug, "is_triage": false,
		"external_source": str(s.ExternalSource), "external_id": str(s.ExternalID),
		"project": s.ProjectID, "workspace": wsID,
		"created_by": str(s.CreatedBy), "updated_by": str(s.UpdatedBy),
		"created_at": s.CreatedAt, "updated_at": s.UpdatedAt, "deleted_at": nil,
	}
}

func labelJSON(l *store.Label, wsID string) map[string]any {
	return map[string]any{
		"id": l.ID, "name": l.Name, "color": l.Color, "description": l.Description, "parent": str(l.Parent),
		"sort_order": l.SortOrder, "external_source": str(l.ExternalSource), "external_id": str(l.ExternalID),
		"project": l.ProjectID, "workspace": wsID,
		"created_by": str(l.CreatedBy), "updated_by": str(l.UpdatedBy),
		"created_at": l.CreatedAt, "updated_at": l.UpdatedAt, "deleted_at": nil,
	}
}

func issueJSON(i *store.Issue) map[string]any {
	return map[string]any{
		"id": i.ID, "name": i.Name, "description_html": i.DescriptionHTML, "description_binary": nil,
		"priority": i.Priority, "state": i.StateID, "parent": str(i.ParentID),
		"start_date": str(i.StartDate), "target_date": str(i.TargetDate),
		"point": nil, "estimate_point": nil, "sort_order": i.SortOrder, "sequence_id": i.SequenceID,
		"completed_at": str(i.CompletedAt), "archived_at": nil, "is_draft": false, "type_id": nil,
		"external_source": str(i.ExternalSource), "external_id": str(i.ExternalID),
		"assignees": i.Assignees, "labels": i.Labels,
		"project": i.ProjectID, "workspace": i.WorkspaceID,
		"created_by": str(i.CreatedBy), "updated_by": str(i.UpdatedBy),
		"created_at": i.CreatedAt, "updated_at": i.UpdatedAt, "deleted_at": nil,
		"cycle_id": str(i.CycleID), "module_ids": i.ModuleIDs, "sub_issues_count": i.SubIssuesCount,
	}
}

func issuesJSON(list []*store.Issue) []map[string]any {
	out := make([]map[string]any, len(list))
	for k, i := range list {
		out[k] = issueJSON(i)
	}
	return out
}

func countsJSON(o map[string]any, g store.GroupCounts) map[string]any {
	o["total_issues"], o["completed_issues"], o["cancelled_issues"] = g.Total, g.Completed, g.Cancelled
	o["started_issues"], o["unstarted_issues"], o["backlog_issues"] = g.Started, g.Unstarted, g.Backlog
	return o
}

func cycleDate(d *string, clock string) any {
	if d == nil {
		return nil
	}
	return *d + "T" + clock + ".000000Z"
}

func cycleJSON(c *store.Cycle, wsID string) map[string]any {
	return countsJSON(map[string]any{
		"id": c.ID, "name": c.Name, "description": c.Description,
		"start_date": cycleDate(c.StartDate, "00:00:00"), "end_date": cycleDate(c.EndDate, "23:59:59"),
		"owned_by": str(c.OwnedBy), "timezone": c.Timezone, "sort_order": c.SortOrder,
		"view_props": map[string]any{}, "progress_snapshot": map[string]any{}, "logo_props": map[string]any{},
		"version": 1, "archived_at": nil, "external_source": str(c.ExternalSource), "external_id": str(c.ExternalID),
		"total_estimates": nil, "completed_estimates": nil, "started_estimates": nil,
		"project": c.ProjectID, "workspace": wsID,
		"created_by": str(c.CreatedBy), "updated_by": str(c.UpdatedBy),
		"created_at": c.CreatedAt, "updated_at": c.UpdatedAt, "deleted_at": nil,
	}, c.Counts)
}

func moduleJSON(m *store.Module, wsID string) map[string]any {
	return countsJSON(map[string]any{
		"id": m.ID, "name": m.Name, "description": m.Description, "description_text": nil, "description_html": nil,
		"start_date": str(m.StartDate), "target_date": str(m.TargetDate), "status": m.Status,
		"lead": str(m.Lead), "members": m.Members, "view_props": map[string]any{}, "logo_props": map[string]any{},
		"sort_order": m.SortOrder, "archived_at": nil, "external_source": str(m.ExternalSource), "external_id": str(m.ExternalID),
		"project": m.ProjectID, "workspace": wsID,
		"created_by": str(m.CreatedBy), "updated_by": str(m.UpdatedBy),
		"created_at": m.CreatedAt, "updated_at": m.UpdatedAt, "deleted_at": nil,
	}, m.Counts)
}

func linkRowJSON(r *store.LinkRow, parentKey, wsID string) map[string]any {
	return map[string]any{
		"id": r.ID, parentKey: r.ParentID, "issue": r.IssueID, "project": r.ProjectID, "workspace": wsID,
		"sub_issues_count": r.SubIssuesCount, "created_by": str(r.CreatedBy), "updated_by": str(r.UpdatedBy),
		"created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "deleted_at": nil,
	}
}

func commentJSON(c *store.Comment, wsID string) map[string]any {
	return map[string]any{
		"id": c.ID, "comment_html": c.CommentHTML, "access": c.Access, "actor": str(c.ActorID),
		"is_member": true, "attachments": []string{}, "edited_at": str(c.EditedAt), "parent": nil, "description": nil,
		"external_source": str(c.ExternalSource), "external_id": str(c.ExternalID),
		"issue": c.IssueID, "project": c.ProjectID, "workspace": wsID,
		"created_by": str(c.CreatedBy), "updated_by": str(c.UpdatedBy),
		"created_at": c.CreatedAt, "updated_at": c.UpdatedAt, "deleted_at": nil,
	}
}

func linkJSON(l *store.Link, wsID string) map[string]any {
	return map[string]any{
		"id": l.ID, "title": str(l.Title), "url": l.URL, "metadata": json.RawMessage(l.Metadata),
		"issue": l.IssueID, "project": l.ProjectID, "workspace": wsID,
		"created_by": str(l.CreatedBy), "updated_by": str(l.UpdatedBy),
		"created_at": l.CreatedAt, "updated_at": l.UpdatedAt, "deleted_at": nil,
	}
}

func activityJSON(a *store.Activity, wsID string) map[string]any {
	return map[string]any{
		"id": a.ID, "verb": a.Verb, "field": str(a.Field), "old_value": str(a.OldValue), "new_value": str(a.NewValue),
		"comment": a.Comment, "attachments": []string{}, "old_identifier": str(a.OldIdentifier), "new_identifier": str(a.NewIdentifier),
		"epoch": int64(a.Epoch), "issue": a.IssueID, "issue_comment": str(a.IssueCommentID), "actor": str(a.ActorID),
		"project": a.ProjectID, "workspace": wsID, "created_at": a.CreatedAt, "updated_at": a.UpdatedAt, "deleted_at": nil,
	}
}
