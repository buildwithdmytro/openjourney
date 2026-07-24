import { FormEvent, useEffect, useState } from "react";
import {
  Catalog, CatalogItem, ConnectedContentSource, createCatalog, getCatalog, listCatalogs, updateCatalog, deleteCatalog,
  listCatalogItems, bulkUploadCatalogItems, createConnectedContentSource, listConnectedContentSources,
  getConnectedContentSource, updateConnectedContentSource, enableConnectedContentSource, deleteConnectedContentSource,
} from "../api";
import { Card, DataTable, ConfirmDialog, EmptyState, ErrorState, Field, JsonField, useToast } from "../components";

function errorMessage(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

export default function Catalogs({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const { push: toast } = useToast();
  const [tab, setTab] = useState<"catalogs" | "sources">("catalogs");
  const [catalogs, setCatalogs] = useState<Catalog[]>([]);
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [items, setItems] = useState<CatalogItem[]>([]);
  const [sources, setSources] = useState<ConnectedContentSource[]>([]);
  const [source, setSource] = useState<ConnectedContentSource | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<{ kind: "catalog" | "source"; id: string } | null>(null);

  async function refresh() {
    try {
      const [nextCatalogs, nextSources] = await Promise.all([
        listCatalogs(baseURL, apiKey),
        listConnectedContentSources(baseURL, apiKey).catch(() => []),
      ]);
      setCatalogs(nextCatalogs);
      setSources(nextSources);
      setError("");
      if (catalog && nextCatalogs.find(c => c.id === catalog.id)) {
        const updated = await getCatalog(baseURL, apiKey, catalog.id);
        setCatalog(updated);
        if (updated.id) {
          const nextItems = await listCatalogItems(baseURL, apiKey, updated.id);
          setItems(nextItems);
        }
      }
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => { if (apiKey) void refresh(); }, [apiKey, baseURL]);

  async function saveCatalog(event: FormEvent) {
    event.preventDefault();
    if (!catalog || saving) return;
    setSaving(true);
    try {
      const saved = catalog.id
        ? await updateCatalog(baseURL, apiKey, catalog.id, { name: catalog.name, description: catalog.description, status: catalog.status })
        : await createCatalog(baseURL, apiKey, { key: catalog.key, name: catalog.name, description: catalog.description, item_key_field: catalog.item_key_field, status: catalog.status, app_id: "" });
      setCatalog(saved);
      setNotice("Catalog saved.");
      toast({ kind: "success", message: catalog.id ? "Catalog updated successfully" : "Catalog created successfully" });
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function uploadItems(file: File) {
    if (!catalog || saving) return;
    setSaving(true);
    try {
      await bulkUploadCatalogItems(baseURL, apiKey, catalog.id, file);
      setNotice("Items uploaded.");
      toast({ kind: "success", message: "Catalog items uploaded successfully" });
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function saveSource(event: FormEvent) {
    event.preventDefault();
    if (!source || saving) return;
    setSaving(true);
    try {
      const saved = source.id
        ? await updateConnectedContentSource(baseURL, apiKey, source.id, {
          name: source.name,
          allowed_host: source.allowed_host,
          auth_header_name: source.auth_header_name,
          auth_secret_ref: source.auth_secret_ref,
          default_ttl_seconds: source.default_ttl_seconds,
          timeout_ms: source.timeout_ms,
          status: source.status,
        })
        : await createConnectedContentSource(baseURL, apiKey, {
          name: source.name,
          allowed_host: source.allowed_host,
          auth_header_name: source.auth_header_name,
          auth_secret_ref: source.auth_secret_ref,
          default_ttl_seconds: source.default_ttl_seconds,
          timeout_ms: source.timeout_ms,
          enabled: false,
          status: "draft",
        });
      setSource(saved);
      setNotice("Source saved.");
      toast({ kind: "success", message: source.id ? "Connected content source updated successfully" : "Connected content source created successfully" });
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function enableSource(id: string) {
    if (saving) return;
    setSaving(true);
    try {
      await enableConnectedContentSource(baseURL, apiKey, id);
      setNotice("Source enabled.");
      toast({ kind: "success", message: "Connected content source enabled" });
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function deleteSource(id: string) {
    if (saving) return;
    setSaving(true);
    try {
      await deleteConnectedContentSource(baseURL, apiKey, id);
      setNotice("Source deleted.");
      toast({ kind: "success", message: "Connected content source deleted" });
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function deleteCatalogAfterConfirm(id: string) {
    try { await deleteCatalog(baseURL, apiKey, id); setCatalog(null); setItems([]); await refresh(); }
    catch (e) { setError(errorMessage(e)); }
  }

  async function confirmDeletion() {
    if (!confirmDelete) return;
    const pending = confirmDelete;
    setConfirmDelete(null);
    if (pending.kind === "source") await deleteSource(pending.id);
    else await deleteCatalogAfterConfirm(pending.id);
  }

  const tabs = [["catalogs", "Catalogs"], ["sources", "Connected Content"]] as const;

  return <section className="stack acquisition-view">
    <ConfirmDialog isOpen={confirmDelete !== null} onClose={() => setConfirmDelete(null)} onConfirm={confirmDeletion} title={confirmDelete?.kind === "source" ? "Delete this source?" : "Delete this catalog?"} message="This action cannot be undone." confirmText="Delete" isDangerous={true} />
    <Card variant="article">
      <div className="section-title">
        <div>
          <div className="eyebrow">Reference data</div>
          <h2>Catalogs & connected content</h2>
        </div>
        <div className="tab-buttons">
          {tabs.map(([value, label]) => (
            <button key={value} className={tab === value ? "active" : ""} onClick={() => setTab(value)}>
              {label}
            </button>
          ))}
        </div>
      </div>
      <p className="muted">Catalogs provide reference data for personalization; connected-content sources are allowlisted, authed external data fetchers.</p>
      {error && <ErrorState description={error} role="alert" />}
      {notice && <p className="success" role="status">{notice}</p>}
    </Card>

    {tab === "catalogs" ? (
      <div className="acquisition-grid">
        <Card variant="article">
          <div className="section-title">
            <h2>Catalog registry</h2>
            <button onClick={() => setCatalog({ id: "", key: "", name: "New catalog", description: "", item_key_field: "id", status: "active", item_count: 0, tenant_id: "", workspace_id: "", app_id: "", created_at: "", updated_at: "" } as Catalog)}>
              New catalog
            </button>
          </div>
          {catalogs.map(item => (
            <button className="resource-row" key={item.id} onClick={() => { setCatalog(item); setTab("catalogs"); void listCatalogItems(baseURL, apiKey, item.id).then(setItems).catch(() => setItems([])); }}>
              <strong>{item.name}</strong>
              <span className={`pill ${item.status}`}>{item.status}</span>
              <small>{item.item_count} items</small>
            </button>
          ))}
          {catalogs.length === 0 && <EmptyState title="No catalogs yet" description="Create a catalog to store reference data" icon="plus" cta={{ label: "New catalog", onClick: () => setCatalog({ id: "", key: "", name: "New catalog", description: "", item_key_field: "id", status: "active", item_count: 0, tenant_id: "", workspace_id: "", app_id: "", created_at: "", updated_at: "" } as Catalog) }} />}
        </Card>

        <Card variant="article">
          {catalog ? (
            <form className="acquisition-form" onSubmit={saveCatalog}>
              <Field id="catalog-key" label="Key" required>
                <input
                  value={catalog.key}
                  onChange={e => setCatalog({ ...catalog, key: e.target.value })}
                  required
                  disabled={!!catalog.id}
                  placeholder="product_catalog"
                />
              </Field>
              <Field id="catalog-name" label="Name" required>
                <input
                  value={catalog.name}
                  onChange={e => setCatalog({ ...catalog, name: e.target.value })}
                  required
                  placeholder="Product Catalog"
                />
              </Field>
              <label>
                Description
                <textarea
                  value={catalog.description || ""}
                  onChange={e => setCatalog({ ...catalog, description: e.target.value })}
                  placeholder="What this catalog contains"
                />
              </label>
              <label>
                Item key field
                <input
                  value={catalog.item_key_field}
                  onChange={e => setCatalog({ ...catalog, item_key_field: e.target.value })}
                  placeholder="id"
                />
              </label>
              <label>
                Status
                <select
                  value={catalog.status}
                  onChange={e => setCatalog({ ...catalog, status: e.target.value as "active" | "archived" })}
                >
                  <option value="active">Active</option>
                  <option value="archived">Archived</option>
                </select>
              </label>
              <div className="form-actions">
                <button type="submit">Save catalog</button>
                {catalog.id && <button type="button" className="danger" onClick={() => setConfirmDelete({ kind: "catalog", id: catalog.id })}>Delete</button>}
              </div>

              {catalog.id && (
                <div style={{ marginTop: "24px", paddingTop: "24px", borderTop: "1px solid var(--color-border)" }}>
                  <h3>Items ({items.length})</h3>
                  <label style={{ marginBottom: "16px" }}>
                    Bulk upload (CSV or JSON)
                    <input
                      type="file"
                      accept=".csv,.json,.jsonl"
                      onChange={e => {
                        const file = e.target.files?.[0];
                        if (file) void uploadItems(file);
                      }}
                    />
                  </label>
                  <div style={{ maxHeight: "400px", overflowY: "auto", border: "1px solid var(--color-border)", borderRadius: "6px" }}>
                    {items.length > 0 ? (
                      <DataTable headers={["Item key", "Payload"]} rows={items.map(item => [<code>{item.item_key}</code>, <code>{JSON.stringify(item.payload).slice(0, 100)}...</code>])} />
                    ) : (
                      <EmptyState title="No items yet" description="Upload a CSV or JSON file to add items." icon="plus" cta={{ label: "Upload items", onClick: () => document.getElementById("catalog-items-file")?.click() }} />
                    )}
                  </div>
                </div>
              )}
            </form>
          ) : (
            <p className="muted">Choose a catalog or create one to begin.</p>
          )}
        </Card>
      </div>
    ) : (
      <Card variant="article">
        <div className="section-title">
          <h2>Connected content sources</h2>
          <button onClick={() => setSource({ id: "", name: "New source", allowed_host: "", auth_header_name: "", auth_secret_ref: "", default_ttl_seconds: 300, timeout_ms: 2000, enabled: false, status: "draft", tenant_id: "", workspace_id: "", created_at: "", updated_at: "" } as ConnectedContentSource)}>
            New source
          </button>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "24px" }}>
          <div>
            {sources.map(item => (
              <button className="resource-row" key={item.id} onClick={() => setSource(item)} style={{ marginBottom: "8px" }}>
                <strong>{item.name}</strong>
                <span className={`pill ${item.status}`}>{item.status}</span>
                {item.enabled && <span className="pill" style={{ background: "var(--color-success)" }}>enabled</span>}
              </button>
            ))}
            {sources.length === 0 && <EmptyState title="No sources yet" description="Create a connected-content source to enable external data fetch" icon="plus" cta={{ label: "Create source", onClick: () => setSource({ id: "", name: "New source", allowed_host: "", auth_header_name: "", auth_secret_ref: "", default_ttl_seconds: 300, timeout_ms: 2000, enabled: false, status: "draft", tenant_id: "", workspace_id: "", created_at: "", updated_at: "" } as ConnectedContentSource) }} />}
          </div>

          <div>
            {source ? (
              <form className="acquisition-form" onSubmit={saveSource}>
                <label>
                  Name
                  <input
                    value={source.name}
                    onChange={e => setSource({ ...source, name: e.target.value })}
                    required
                    placeholder="API source name"
                  />
                </label>
                <label>
                  Allowed host
                  <input
                    value={source.allowed_host}
                    onChange={e => setSource({ ...source, allowed_host: e.target.value })}
                    required
                    placeholder="api.example.com"
                  />
                </label>
                <label>
                  Auth header name
                  <input
                    value={source.auth_header_name || ""}
                    onChange={e => setSource({ ...source, auth_header_name: e.target.value })}
                    placeholder="Authorization (optional)"
                  />
                </label>
                <label>
                  Auth secret ref (env var)
                  <input
                    value={source.auth_secret_ref || ""}
                    onChange={e => setSource({ ...source, auth_secret_ref: e.target.value })}
                    placeholder="API_SECRET (optional)"
                  />
                </label>
                <label>
                  Default TTL (seconds)
                  <input
                    type="number"
                    min="0"
                    max="86400"
                    value={source.default_ttl_seconds}
                    onChange={e => setSource({ ...source, default_ttl_seconds: Number(e.target.value) })}
                  />
                </label>
                <label>
                  Timeout (ms)
                  <input
                    type="number"
                    min="100"
                    max="10000"
                    value={source.timeout_ms}
                    onChange={e => setSource({ ...source, timeout_ms: Number(e.target.value) })}
                  />
                </label>
                <label>
                  Status
                  <select
                    value={source.status}
                    onChange={e => setSource({ ...source, status: e.target.value as "draft" | "active" | "disabled" })}
                  >
                    <option value="draft">Draft</option>
                    <option value="active">Active</option>
                    <option value="disabled">Disabled</option>
                  </select>
                </label>
                <div className="form-actions">
                  <button type="submit">Save source</button>
                  {source.id && !source.enabled && (
                    <button type="button" className="secondary" onClick={() => void enableSource(source.id)}>
                      Enable source
                    </button>
                  )}
                  {source.id && (
                    <button type="button" className="danger" onClick={() => setConfirmDelete({ kind: "source", id: source.id })}>
                      Delete
                    </button>
                  )}
                </div>
              </form>
            ) : (
              <p className="muted">Choose a source or create one to begin.</p>
            )}
          </div>
        </div>
      </Card>
    )}
  </section>;
}
