-- Reads the old Plane database and prints ONE JSON document with everything
-- MostlyDone's importer needs. Read-only. Soft-deleted rows, drafts and
-- archived items are left out. Times are UTC in MostlyDone's format.
\pset tuples_only on
\pset format unaligned
select json_build_object(
 'users', (select coalesce(json_agg(u),'[]') from (
    select id, email, first_name, last_name, display_name, is_active from users where not is_bot order by date_joined) u),
 'workspace_members', (select coalesce(json_agg(m),'[]') from (
    select member_id as user_id, role from workspace_members where deleted_at is null) m),
 'projects', (select coalesce(json_agg(p),'[]') from (
    select id, name, identifier, description, network, emoji, cycle_view, module_view,
           project_lead_id, default_assignee_id, created_by_id,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at,
           to_char(updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') updated_at
    from projects where deleted_at is null and archived_at is null order by created_at) p),
 'states', (select coalesce(json_agg(s),'[]') from (
    select id, project_id, name, color, "group", "default" as is_default, sequence, description,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from states where deleted_at is null and "group" <> 'triage' order by project_id, sequence) s),
 'labels', (select coalesce(json_agg(l),'[]') from (
    select id, project_id, name, color, description, parent_id, sort_order,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from labels where deleted_at is null order by project_id, created_at) l),
 'issues', (select coalesce(json_agg(i),'[]') from (
    select i.id, i.project_id, i.sequence_id, i.name, i.description_html, i.state_id, i.priority, i.parent_id,
           to_char(i.start_date,'YYYY-MM-DD') start_date, to_char(i.target_date,'YYYY-MM-DD') target_date,
           i.sort_order, i.created_by_id, i.updated_by_id,
           to_char(i.completed_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') completed_at,
           to_char(i.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at,
           to_char(i.updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') updated_at,
           (select coalesce(json_agg(il.label_id),'[]') from issue_labels il where il.issue_id=i.id and il.deleted_at is null) label_ids,
           (select coalesce(json_agg(ia.assignee_id),'[]') from issue_assignees ia where ia.issue_id=i.id and ia.deleted_at is null) assignee_ids
    from issues i where i.deleted_at is null and not i.is_draft and i.archived_at is null
    order by i.project_id, i.sequence_id) i),
 'cycles', (select coalesce(json_agg(c),'[]') from (
    select id, project_id, name, description,
           to_char(start_date at time zone 'UTC','YYYY-MM-DD') start_date, to_char(end_date at time zone 'UTC','YYYY-MM-DD') end_date,
           owned_by_id, sort_order, timezone,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from cycles where deleted_at is null and archived_at is null order by project_id, start_date) c),
 'cycle_issues', (select coalesce(json_agg(x),'[]') from (
    select cycle_id, issue_id, to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from cycle_issues where deleted_at is null order by created_at) x),
 'modules', (select coalesce(json_agg(m),'[]') from (
    select id, project_id, name, coalesce(description,'') description, to_char(start_date,'YYYY-MM-DD') start_date,
           to_char(target_date,'YYYY-MM-DD') target_date, status, lead_id, sort_order,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from modules where deleted_at is null and archived_at is null order by project_id, created_at) m),
 'module_issues', (select coalesce(json_agg(x),'[]') from (
    select module_id, issue_id, to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at
    from module_issues where deleted_at is null order by created_at) x),
 'comments', (select coalesce(json_agg(c),'[]') from (
    select id, issue_id, actor_id, comment_html, access,
           to_char(edited_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') edited_at,
           to_char(created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') created_at,
           to_char(updated_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') updated_at
    from issue_comments where deleted_at is null order by created_at) c)
);
