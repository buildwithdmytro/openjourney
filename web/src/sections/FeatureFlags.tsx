import { FormEvent, useEffect, useState } from "react";
import {
  listFeatureFlags, getFeatureFlag, createFeatureFlag, updateFeatureFlag, publishFeatureFlag, setFeatureFlagStatus,
  FeatureFlag, FlagVariant, FlagTargetingRule,
} from "../api";
import { EmptyState, Modal, Input, Select, Textarea, JsonField, ConfirmDialog, Badge } from "../components";
import { useToast } from "../components";

function errorMessage(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

const ENVIRONMENTS = ["development", "staging", "production"];
const FLAG_TYPES = ["boolean", "string", "number", "json"] as const;

function newBlankVariant(): FlagVariant {
  return { label: "", value: null, weight: 0 };
}

function newBlankRule(): FlagTargetingRule {
  return { dsl: {}, variant: "" };
}

export default function FeatureFlags({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const { push: toast } = useToast();
  const [flags, setFlags] = useState<FeatureFlag[]>([]);
  const [environment, setEnvironment] = useState<string>("production");
  const [editingID, setEditingID] = useState("");
  const [showModal, setShowModal] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmPublishID, setConfirmPublishID] = useState("");
  const [confirmDisableID, setConfirmDisableID] = useState("");

  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [flagType, setFlagType] = useState<FeatureFlag["flag_type"]>("boolean");
  const [defaultValue, setDefaultValue] = useState<string>("false");
  const [variants, setVariants] = useState<FlagVariant[]>([]);
  const [targetingRules, setTargetingRules] = useState<FlagTargetingRule[]>([]);
  const [rolloutPct, setRolloutPct] = useState(0);
  const [enabled, setEnabled] = useState(false);

  const filteredFlags = flags.filter((f) => f.environment === environment);
  const sortedFlags = [...filteredFlags].sort((a, b) => (a.key || "").localeCompare(b.key || ""));

  async function loadFlags() {
    setLoading(true);
    try {
      const list = await listFeatureFlags(baseURL, apiKey);
      setFlags(list || []);
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void loadFlags(); }, [apiKey, baseURL]);

  function resetForm() {
    setEditingID("");
    setKey("");
    setName("");
    setDescription("");
    setFlagType("boolean");
    setDefaultValue("false");
    setVariants([]);
    setTargetingRules([]);
    setRolloutPct(0);
    setEnabled(false);
  }

  async function editFlag(flag: FeatureFlag) {
    try {
      const full = await getFeatureFlag(baseURL, apiKey, flag.id);
      setEditingID(full.id);
      setKey(full.key);
      setName(full.name || "");
      setDescription(full.description || "");
      setFlagType(full.flag_type);
      setDefaultValue(JSON.stringify(full.default_value));
      setVariants(full.variants || []);
      setTargetingRules(full.targeting_rules || []);
      setRolloutPct(full.rollout_pct);
      setEnabled(full.enabled);
      setShowModal(true);
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    }
  }

  function parseDefaultValue(): unknown {
    try {
      return JSON.parse(defaultValue);
    } catch {
      return defaultValue;
    }
  }

  async function saveFlag(event: FormEvent) {
    event.preventDefault();
    if (!key.trim()) {
      toast({ kind: "error", message: "Flag key is required" });
      return;
    }
    if (variants.some((v) => !v.label.trim())) {
      toast({ kind: "error", message: "All variant labels must be filled" });
      return;
    }

    setSaving(true);
    try {
      const flagData: Partial<FeatureFlag> = {
        key: key.trim(),
        name: name.trim() || undefined,
        description: description.trim() || undefined,
        flag_type: flagType,
        default_value: parseDefaultValue(),
        variants: variants.length > 0 ? variants : undefined,
        targeting_rules: targetingRules.length > 0 ? targetingRules : undefined,
        rollout_pct: rolloutPct,
        enabled,
        environment,
      };

      if (editingID) {
        await updateFeatureFlag(baseURL, apiKey, editingID, flagData);
        toast({ kind: "success", message: "Flag updated successfully" });
      } else {
        await createFeatureFlag(baseURL, apiKey, flagData);
        toast({ kind: "success", message: "Flag created successfully" });
      }
      setShowModal(false);
      resetForm();
      await loadFlags();
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setSaving(false);
    }
  }

  async function handlePublish(flagID: string) {
    try {
      await publishFeatureFlag(baseURL, apiKey, flagID);
      toast({ kind: "success", message: "Flag published successfully" });
      setConfirmPublishID("");
      await loadFlags();
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    }
  }

  async function handleToggleStatus(flagID: string, currentStatus: string) {
    const newStatus = currentStatus === "disabled" ? "draft" : "disabled";
    try {
      await setFeatureFlagStatus(baseURL, apiKey, flagID, newStatus);
      toast({ kind: "success", message: newStatus === "disabled" ? "Flag disabled" : "Flag enabled" });
      setConfirmDisableID("");
      await loadFlags();
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    }
  }

  function updateVariant(index: number, patch: Partial<FlagVariant>) {
    setVariants((current) => current.map((v, i) => (i === index ? { ...v, ...patch } : v)));
  }

  function updateRule(index: number, patch: Partial<FlagTargetingRule>) {
    setTargetingRules((current) => current.map((r, i) => (i === index ? { ...r, ...patch } : r)));
  }

  return (
    <section className="stack feature-flags-view">
      <article className="card">
        <div className="section-title">
          <div>
            <div className="eyebrow">Feature flags</div>
            <h2>Environment-scoped flags with targeting</h2>
          </div>
          <button onClick={() => { resetForm(); setShowModal(true); }}>New flag</button>
        </div>
        <p className="muted">
          Flags are published as immutable versions and evaluated server-side. Kill-switch (disabled status) returns the default value for all subjects.
        </p>
      </article>

      <article className="card">
        <label>Environment
          <select value={environment} onChange={(e) => setEnvironment(e.target.value)}>
            {ENVIRONMENTS.map((env) => (
              <option key={env} value={env}>{env}</option>
            ))}
          </select>
        </label>
      </article>

      {loading ? (
        <p className="muted">Loading flags…</p>
      ) : sortedFlags.length === 0 ? (
        <EmptyState title="No flags yet" description={`Create a flag for ${environment} environment`} icon="plus" cta={{ label: "Create flag", onClick: () => { resetForm(); setShowModal(true); } }} />
      ) : (
        <article className="card">
          <table className="resource-table">
            <thead>
              <tr>
                <th>Key</th>
                <th>Type</th>
                <th>Status</th>
                <th>Rollout %</th>
                <th>Enabled</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {sortedFlags.map((flag) => (
                <tr key={flag.id}>
                  <td><strong>{flag.key}</strong></td>
                  <td><Badge>{flag.flag_type}</Badge></td>
                  <td><Badge className={flag.status}>{flag.status}</Badge></td>
                  <td>{flag.rollout_pct}%</td>
                  <td>{flag.enabled ? "✓" : "–"}</td>
                  <td className="actions">
                    <button className="text-button" onClick={() => void editFlag(flag)}>Edit</button>
                    {flag.status === "draft" && (
                      <button className="text-button" onClick={() => setConfirmPublishID(flag.id)}>Publish</button>
                    )}
                    {flag.status === "published" && (
                      <button className="text-button" onClick={() => setConfirmDisableID(flag.id)}>
                        Kill switch
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </article>
      )}

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} aria-labelledby="flag-modal-title">
        <div className="modal-header">
          <h2 id="flag-modal-title">{editingID ? "Edit flag" : "Create flag"}</h2>
        </div>
        <form onSubmit={(e) => void saveFlag(e)} className="modal-form">
          <div className="form-group">
            <label>Key
              <Input
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder="feature_flag_key"
                disabled={!!editingID}
                required
              />
            </label>
          </div>

          <div className="form-group">
            <label>Name
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="User-friendly name"
              />
            </label>
          </div>

          <div className="form-group">
            <label>Description
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What does this flag do?"
                rows={3}
              />
            </label>
          </div>

          <div className="form-group">
            <label>Flag type
              <Select value={flagType} onChange={(e) => setFlagType(e.target.value as FeatureFlag["flag_type"])}>
                {FLAG_TYPES.map((type) => (
                  <option key={type} value={type}>{type}</option>
                ))}
              </Select>
            </label>
          </div>

          <div className="form-group">
            <label>Default value
              <JsonField
                value={defaultValue}
                onChange={(e) => setDefaultValue(e.target.value)}
                placeholder={flagType === "boolean" ? "true" : "null"}
              />
            </label>
          </div>

          <div className="form-group">
            <label>Environment
              <Select value={environment} onChange={(e) => setEnvironment(e.target.value)} disabled={!!editingID}>
                {ENVIRONMENTS.map((env) => (
                  <option key={env} value={env}>{env}</option>
                ))}
              </Select>
            </label>
          </div>

          <fieldset>
            <legend>Variants</legend>
            {variants.map((variant, index) => (
              <div key={index} className="variant-row">
                <Input
                  aria-label={`Variant ${index + 1} label`}
                  placeholder="Label"
                  value={variant.label}
                  onChange={(e) => updateVariant(index, { label: e.target.value })}
                />
                <JsonField
                  value={JSON.stringify(variant.value ?? null)}
                  onChange={(e) => {
                    try {
                      updateVariant(index, { value: JSON.parse(e.target.value) });
                    } catch {
                      // Allow invalid JSON while typing
                    }
                  }}
                  placeholder="Value"
                  validateOnBlur={true}
                />
                <Input
                  aria-label={`Variant ${index + 1} weight`}
                  type="number"
                  min="0"
                  value={variant.weight}
                  onChange={(e) => updateVariant(index, { weight: Number(e.target.value) })}
                  placeholder="Weight"
                />
                <button
                  type="button"
                  className="danger"
                  onClick={() => setVariants((v) => v.filter((_, i) => i !== index))}
                >
                  Remove
                </button>
              </div>
            ))}
            <button
              type="button"
              className="secondary"
              onClick={() => setVariants((v) => [...v, newBlankVariant()])}
            >
              Add variant
            </button>
          </fieldset>

          <fieldset>
            <legend>Targeting rules (evaluated in order)</legend>
            {targetingRules.map((rule, index) => (
              <div key={index} className="rule-row">
                <JsonField
                  value={JSON.stringify(rule.dsl ?? {})}
                  onChange={(e) => {
                    try {
                      updateRule(index, { dsl: JSON.parse(e.target.value) });
                    } catch {
                      // Allow invalid JSON while typing
                    }
                  }}
                  placeholder="DSL condition"
                  validateOnBlur={true}
                />
                <Input
                  aria-label={`Rule ${index + 1} variant`}
                  placeholder="Variant label"
                  value={rule.variant}
                  onChange={(e) => updateRule(index, { variant: e.target.value })}
                />
                <button
                  type="button"
                  className="danger"
                  onClick={() => setTargetingRules((r) => r.filter((_, i) => i !== index))}
                >
                  Remove
                </button>
              </div>
            ))}
            <button
              type="button"
              className="secondary"
              onClick={() => setTargetingRules((r) => [...r, newBlankRule()])}
            >
              Add rule
            </button>
          </fieldset>

          <div className="form-group">
            <label>Rollout percentage (0-100)
              <Input
                type="number"
                min="0"
                max="100"
                value={rolloutPct}
                onChange={(e) => setRolloutPct(Math.max(0, Math.min(100, Number(e.target.value))))}
              />
            </label>
          </div>

          <div className="form-group">
            <label className="checkbox">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              Enabled
            </label>
          </div>

          <div className="modal-actions">
            <button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Save"}
            </button>
            <button type="button" className="secondary" onClick={() => setShowModal(false)}>
              Cancel
            </button>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        isOpen={!!confirmPublishID}
        title="Publish flag?"
        message="Publishing creates an immutable version. This cannot be undone."
        confirmText="Publish"
        onConfirm={() => void handlePublish(confirmPublishID)}
        onClose={() => setConfirmPublishID("")}
      />

      {confirmDisableID && (
        <ConfirmDialog
          isOpen={true}
          title="Toggle kill switch?"
          message={flags.find((f) => f.id === confirmDisableID)?.status === "disabled"
            ? "This will enable the flag and restore rollout evaluation."
            : "This will disable the flag and return the default value for all subjects immediately."
          }
          confirmText="Toggle"
          onConfirm={() => {
            const flag = flags.find((f) => f.id === confirmDisableID);
            if (flag) void handleToggleStatus(confirmDisableID, flag.status);
          }}
          onClose={() => setConfirmDisableID("")}
        />
      )}
    </section>
  );
}
