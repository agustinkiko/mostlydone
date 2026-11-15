// Wire shapes, as produced by internal/api/shapes.go and auth.go.

export const ROLE_GUEST = 5;
export const ROLE_MEMBER = 15;
export const ROLE_ADMIN = 20;

export interface User {
  id: string;
  first_name: string;
  last_name: string;
  email: string;
  avatar: string;
  avatar_url: string | null;
  display_name: string;
}

export interface Member extends User {
  role: number;
}

// Row of GET /auth/users (admin only), includes deactivated users.
export interface AccountUser {
  id: string;
  email: string;
  display_name: string;
  role: number;
  is_active: boolean;
}

export interface Prefs {
  theme: "system" | "light" | "dark";
  density: "comfortable" | "compact";
}

export interface Me {
  user: User | null;
  role: number | null;
  prefs?: Prefs | null;
  workspace_slug: string;
  setup_required: boolean;
  setup_token_required?: boolean;
}

export interface Project {
  id: string;
  workspace: string;
  name: string;
  identifier: string;
  description: string;
  default_state: string | null;
  module_view: boolean;
  cycle_view: boolean;
  member_role: number;
  total_members: number;
  total_cycles: number;
  total_modules: number;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
}

/** A project in the trash (admin only). purge_at is when it is deleted for good. */
export interface TrashedProject extends Project {
  deleted_at: string;
  purge_at: string;
}

export type StateGroup = "backlog" | "unstarted" | "started" | "completed" | "cancelled";
export const STATE_GROUPS: StateGroup[] = ["backlog", "unstarted", "started", "completed", "cancelled"];

export interface State {
  id: string;
  name: string;
  color: string;
  group: StateGroup;
  description: string;
  sequence: number;
  default: boolean;
  project: string;
}

export interface Label {
  id: string;
  name: string;
  color: string;
  description: string;
  parent: string | null;
  sort_order: number;
  project: string;
}

export type Priority = "urgent" | "high" | "medium" | "low" | "none";
export const PRIORITIES: Priority[] = ["urgent", "high", "medium", "low", "none"];

export interface Issue {
  id: string;
  name: string;
  description_html: string;
  priority: Priority;
  state: string;
  parent: string | null;
  start_date: string | null;
  target_date: string | null;
  sort_order: number;
  sequence_id: number;
  completed_at: string | null;
  assignees: string[] | null;
  labels: string[] | null;
  project: string;
  workspace: string;
  created_by: string | null;
  updated_by: string | null;
  created_at: string;
  updated_at: string;
  cycle_id: string | null;
  module_ids: string[] | null;
  sub_issues_count: number;
}

/** Writable work item fields (PATCH is partial). */
export interface IssueInput {
  name?: string;
  description_html?: string;
  state?: string;
  priority?: Priority;
  parent?: string | null;
  start_date?: string | null;
  target_date?: string | null;
  sort_order?: number;
  assignees?: string[];
  labels?: string[];
  cycle_id?: string | null;
  module_ids?: string[];
}

interface GroupCounts {
  total_issues: number;
  completed_issues: number;
  cancelled_issues: number;
  started_issues: number;
  unstarted_issues: number;
  backlog_issues: number;
}

export interface Cycle extends GroupCounts {
  id: string;
  name: string;
  description: string;
  start_date: string | null; // "YYYY-MM-DDT00:00:00.000000Z"
  end_date: string | null; // "YYYY-MM-DDT23:59:59.000000Z"
  owned_by: string | null;
  project: string;
  created_by: string | null;
  created_at: string;
}

export const MODULE_STATUSES = ["backlog", "planned", "in-progress", "paused", "completed", "cancelled"] as const;
export type ModuleStatus = (typeof MODULE_STATUSES)[number];

export interface Module extends GroupCounts {
  id: string;
  name: string;
  description: string;
  start_date: string | null;
  target_date: string | null;
  status: ModuleStatus;
  lead: string | null;
  members: string[] | null;
  project: string;
  created_by: string | null;
  created_at: string;
}

export interface Comment {
  id: string;
  comment_html: string;
  actor: string | null;
  edited_at: string | null;
  issue: string;
  created_at: string;
  updated_at: string;
}

export interface IssueLink {
  id: string;
  title: string | null;
  url: string;
  issue: string;
  created_at: string;
}

export interface Activity {
  id: string;
  verb: string;
  field: string | null;
  old_value: string | null;
  new_value: string | null;
  comment: string;
  actor: string | null;
  created_at: string;
}

export interface Token {
  id: string;
  label: string;
  created_at: string;
  last_used_at: string | null;
  token?: string; // only on create
}

export interface Page<T> {
  total_count: number;
  total_results: number;
  total_pages: number;
  count: number;
  next_cursor: string;
  prev_cursor: string;
  next_page_results: boolean;
  prev_page_results: boolean;
  results: T[];
}

export interface SearchHit {
  name: string;
  id: string;
  sequence_id: number;
  project__identifier: string;
  project_id: string;
  workspace__slug: string;
}
