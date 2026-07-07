import { UserManager, WebStorageStateStore } from "oidc-client-ts";

const authority = import.meta.env.VITE_OIDC_AUTHORITY;
const clientID = import.meta.env.VITE_OIDC_CLIENT_ID;
export const oidcConfigured = Boolean(authority && clientID);

const manager = oidcConfigured ? new UserManager({
  authority: authority as string,
  client_id: clientID as string,
  redirect_uri: import.meta.env.VITE_OIDC_REDIRECT_URI || window.location.origin,
  post_logout_redirect_uri: window.location.origin,
  response_type: "code",
  scope: "openid profile email",
  userStore: new WebStorageStateStore({ store: window.sessionStorage }),
}) : null;

export async function restoreOIDCSession(): Promise<string | null> {
  if (!manager) return null;
  if (window.location.search.includes("code=") && window.location.search.includes("state=")) {
    const user = await manager.signinRedirectCallback();
    window.history.replaceState({}, document.title, window.location.pathname);
    return user.id_token ?? null;
  }
  const user = await manager.getUser();
  return user && !user.expired ? user.id_token ?? null : null;
}

export async function signIn(): Promise<void> {
  if (!manager) throw new Error("OIDC is not configured");
  await manager.signinRedirect();
}

export async function signOut(): Promise<void> {
  if (!manager) return;
  await manager.removeUser();
  await manager.signoutRedirect();
}
