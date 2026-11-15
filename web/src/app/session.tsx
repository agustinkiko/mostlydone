import { createContext, useContext } from "react";
import { ROLE_ADMIN, ROLE_MEMBER, type Project, type User } from "../api/types";

export interface Session {
  user: User;
  role: number;
  slug: string;
  projects: Project[] | undefined;
  projectsError: string;
  reloadProjects: () => Promise<Project[] | undefined>;
  logout: () => void;
  /** Opens the new-work-item dialog (keyboard `c`, buttons). */
  openNewItem: () => void;
  openPalette: () => void;
}

export const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(SessionContext);
  if (!s) throw new Error("useSession outside SessionContext");
  return s;
}

/** Members and admins write; guests read and comment only. */
export function useCanWrite(): boolean {
  return useSession().role >= ROLE_MEMBER;
}

export function useIsAdmin(): boolean {
  return useSession().role >= ROLE_ADMIN;
}

export function useProjectByIdent(ident: string | undefined): Project | undefined {
  const { projects } = useSession();
  if (!ident) return undefined;
  const up = ident.toUpperCase();
  return projects?.find((p) => p.identifier.toUpperCase() === up);
}

export function issueKey(p: { identifier: string } | undefined, seq: number): string {
  return `${p?.identifier ?? "?"}-${seq}`;
}
