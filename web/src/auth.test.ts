import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const manager = {
  getUser: vi.fn(),
  removeUser: vi.fn(),
  signinRedirect: vi.fn(),
  signinRedirectCallback: vi.fn(),
  signoutRedirect: vi.fn(),
};

const userManagerConstructor = vi.fn(() => manager);
const webStorageStateStoreConstructor = vi.fn();

vi.mock("oidc-client-ts", () => ({
  UserManager: userManagerConstructor,
  WebStorageStateStore: webStorageStateStoreConstructor,
}));

async function loadConfiguredAuth() {
  vi.resetModules();
  vi.stubEnv("VITE_OIDC_AUTHORITY", "https://issuer.example.test");
  vi.stubEnv("VITE_OIDC_CLIENT_ID", "openjourney-web");
  vi.stubEnv("VITE_OIDC_REDIRECT_URI", "https://app.example.test/callback");
  return import("./auth");
}

describe("OIDC auth helper", () => {
  beforeEach(() => {
    manager.getUser.mockReset();
    manager.removeUser.mockReset();
    manager.signinRedirect.mockReset();
    manager.signinRedirectCallback.mockReset();
    manager.signoutRedirect.mockReset();
    userManagerConstructor.mockClear();
    webStorageStateStoreConstructor.mockClear();
    sessionStorage.clear();
    history.replaceState({}, "", "/");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("configures authorization-code OIDC with session storage", async () => {
    const auth = await loadConfiguredAuth();
    expect(auth.oidcConfigured).toBe(true);
    expect(webStorageStateStoreConstructor).toHaveBeenCalledWith({ store: window.sessionStorage });
    expect(userManagerConstructor).toHaveBeenCalledWith(expect.objectContaining({
      authority: "https://issuer.example.test",
      client_id: "openjourney-web",
      redirect_uri: "https://app.example.test/callback",
      response_type: "code",
      scope: "openid profile email",
    }));
  });

  it("handles redirect callback and removes code from the browser URL", async () => {
    history.replaceState({}, "", "/?code=auth-code&state=opaque-state");
    manager.signinRedirectCallback.mockResolvedValue({ id_token: "id-token" });

    const auth = await loadConfiguredAuth();
    await expect(auth.restoreOIDCSession()).resolves.toBe("id-token");

    expect(manager.signinRedirectCallback).toHaveBeenCalledOnce();
    expect(location.pathname + location.search).toBe("/");
  });

  it("restores a non-expired existing OIDC session", async () => {
    manager.getUser.mockResolvedValue({ expired: false, id_token: "existing-token" });

    const auth = await loadConfiguredAuth();
    await expect(auth.restoreOIDCSession()).resolves.toBe("existing-token");

    expect(manager.getUser).toHaveBeenCalledOnce();
  });

  it("does not restore expired sessions", async () => {
    manager.getUser.mockResolvedValue({ expired: true, id_token: "expired-token" });

    const auth = await loadConfiguredAuth();
    await expect(auth.restoreOIDCSession()).resolves.toBeNull();
  });

  it("starts sign-in and clears local user state before sign-out redirect", async () => {
    const auth = await loadConfiguredAuth();

    await auth.signIn();
    expect(manager.signinRedirect).toHaveBeenCalledOnce();

    await auth.signOut();
    expect(manager.removeUser).toHaveBeenCalledOnce();
    expect(manager.signoutRedirect).toHaveBeenCalledOnce();
  });
});
