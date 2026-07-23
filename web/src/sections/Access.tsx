import { FormEvent, useEffect, useState } from "react";
import { createRole, createTeam, createUser, listPermissions, listRoles, listTeams, listUsers, Permission, Role, Team, User } from "../api";
import { Badge, Button, Card, DataTable, ErrorState, Field, Input, ScopeSelector } from "../components";

function errorMessage(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

export default function Access({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [roles, setRoles] = useState<Role[]>([]); const [users, setUsers] = useState<User[]>([]);
  const [teams, setTeams] = useState<Team[]>([]); const [permissions, setPermissions] = useState<Permission[]>([]);
  const [roleName, setRoleName] = useState(""); const [selectedPermissions, setSelectedPermissions] = useState<string[]>([]);
  const [teamName, setTeamName] = useState(""); const [teamRoles, setTeamRoles] = useState("");
  const [email, setEmail] = useState(""); const [password, setPassword] = useState(""); const [roleIDs, setRoleIDs] = useState("");
  const [error, setError] = useState("");
  async function refresh() {
    try { const [r, u, t, p] = await Promise.all([listRoles(baseURL, apiKey), listUsers(baseURL, apiKey), listTeams(baseURL, apiKey), listPermissions(baseURL, apiKey)]); setRoles(r); setUsers(u); setTeams(t); setPermissions(p); setError(""); }
    catch (cause) { setError(errorMessage(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey, baseURL]);
  async function addRole(event: FormEvent) { event.preventDefault(); try { await createRole(baseURL, apiKey, roleName, selectedPermissions); setRoleName(""); await refresh(); } catch (cause) { setError(errorMessage(cause)); } }
  async function addTeam(event: FormEvent) { event.preventDefault(); try { await createTeam(baseURL, apiKey, { name: teamName, description: "", member_ids: [], role_ids: teamRoles.split(",").map(v => v.trim()).filter(Boolean) }); setTeamName(""); setTeamRoles(""); await refresh(); } catch (cause) { setError(errorMessage(cause)); } }
  async function addUser(event: FormEvent) { event.preventDefault(); try { await createUser(baseURL, apiKey, { email, display_name: email, password, role_ids: roleIDs.split(",").map(v => v.trim()).filter(Boolean) }); setEmail(""); setPassword(""); setRoleIDs(""); await refresh(); } catch (cause) { setError(errorMessage(cause)); } }
  return <section className="stack access-console">
    {error && <ErrorState description={error} />}
    <div className="section-title"><div><div className="eyebrow">Enterprise access</div><h2>Roles, teams, and users</h2></div><Button variant="secondary" onClick={() => void refresh()}>Refresh</Button></div>
    <div className="acquisition-grid">
      <Card variant="article"><h3>Create role</h3><form className="governance-form" onSubmit={addRole}><Field id="role-name" label="Role name"><Input value={roleName} onChange={e => setRoleName(e.target.value)} required /></Field><Field id="role-permissions" label="Permissions"><ScopeSelector selected={selectedPermissions} onChange={setSelectedPermissions} availableScopes={permissions.map(p => p.key)} /></Field><Button type="submit" disabled={!apiKey || !selectedPermissions.length}>Create role</Button></form></Card>
      <Card variant="article"><h3>Create team</h3><form className="governance-form" onSubmit={addTeam}><Field id="team-name" label="Team name"><Input value={teamName} onChange={e => setTeamName(e.target.value)} required /></Field><Field id="team-roles" label="Role IDs"><Input value={teamRoles} onChange={e => setTeamRoles(e.target.value)} placeholder="comma-separated role IDs" /></Field><Button type="submit">Create team</Button></form></Card>
      <Card variant="article"><h3>Provision user</h3><form className="governance-form" onSubmit={addUser}><Field id="user-email" label="Email"><Input type="email" value={email} onChange={e => setEmail(e.target.value)} required /></Field><Field id="user-password" label="Password"><Input type="password" value={password} onChange={e => setPassword(e.target.value)} required /></Field><Field id="user-roles" label="Role IDs"><Input value={roleIDs} onChange={e => setRoleIDs(e.target.value)} placeholder="comma-separated role IDs" /></Field><Button type="submit">Provision user</Button></form></Card>
    </div>
    <Card variant="article"><h3>Permission catalog</h3><DataTable headers={["Key", "Resource", "Verb", "Description"]} rows={permissions.map(p => [p.key, p.resource, p.verb, p.description])} /></Card>
    <Card variant="article"><h3>Roles</h3><DataTable headers={["Name", "Permissions", "Type"]} rows={roles.map(r => [r.name, r.permissions.join(", "), <Badge key={r.id} kind={r.system ? "default" : "success"}>{r.system ? "System" : "Custom"}</Badge>])} /></Card>
    <Card variant="article"><h3>Teams</h3><DataTable headers={["Name", "Members", "Roles"]} rows={teams.map(t => [t.name, t.member_ids.length, t.role_ids.join(", ") || "—"])} /></Card>
    <Card variant="article"><h3>Users</h3><DataTable headers={["Email", "Identity", "Roles"]} rows={users.map(u => [u.email || "—", u.local ? "Local" : "OIDC", u.role_ids.join(", ") || "—"])} /></Card>
  </section>;
}
