import { FormEvent, useEffect, useState } from "react";
import {
  Prompt,
  PromptVersion,
  createPrompt,
  createPromptVersion,
  listPromptVersions,
  listPrompts,
  publishPromptVersion,
  setPromptVersionEvalStatus,
} from "../api";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  JsonField,
  Modal,
  Select,
  Textarea,
  useToast,
} from "../components";

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed";
}

const TASK_TYPES = [
  "content_draft",
  "audience_dsl",
  "journey_draft",
  "performance_summary",
  "analytics_insight",
  "moderation",
];

export default function Prompts({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const { push: toast } = useToast();
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [selectedPrompt, setSelectedPrompt] = useState<Prompt | null>(null);
  const [versions, setVersions] = useState<PromptVersion[]>([]);
  const [loadingPrompts, setLoadingPrompts] = useState(false);
  const [loadingVersions, setLoadingVersions] = useState(false);
  const [saving, setSaving] = useState(false);

  // Modal states
  const [showPromptModal, setShowPromptModal] = useState(false);
  const [showVersionModal, setShowVersionModal] = useState(false);
  const [publishTarget, setPublishTarget] = useState<PromptVersion | null>(null);

  // New Prompt Form
  const [promptName, setPromptName] = useState("");
  const [taskType, setTaskType] = useState("content_draft");

  // New Version Form
  const [template, setTemplate] = useState("");
  const [provider, setProvider] = useState("mock");
  const [model, setModel] = useState("mock-model");
  const [inputSchemaJson, setInputSchemaJson] = useState("{}");
  const [outputSchemaJson, setOutputSchemaJson] = useState("{}");
  const [paramsJson, setParamsJson] = useState("{}");
  const [safetyPolicyJson, setSafetyPolicyJson] = useState("{}");

  async function loadPrompts() {
    setLoadingPrompts(true);
    try {
      const list = await listPrompts(baseURL, apiKey);
      setPrompts(list || []);
      if (selectedPrompt) {
        const updated = list.find((p) => p.id === selectedPrompt.id);
        if (updated) {
          setSelectedPrompt(updated);
        }
      }
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setLoadingPrompts(false);
    }
  }

  async function loadVersions(promptID: string) {
    setLoadingVersions(true);
    try {
      const list = await listPromptVersions(baseURL, apiKey, promptID);
      setVersions(list || []);
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setLoadingVersions(false);
    }
  }

  useEffect(() => {
    void loadPrompts();
  }, [apiKey, baseURL]);

  function handleSelectPrompt(p: Prompt) {
    setSelectedPrompt(p);
    void loadVersions(p.id);
  }

  async function handleCreatePrompt(e: FormEvent) {
    e.preventDefault();
    if (saving) return;
    if (!promptName.trim()) {
      toast({ kind: "error", message: "Prompt name is required" });
      return;
    }
    setSaving(true);
    try {
      const newPrompt = await createPrompt(baseURL, apiKey, {
        name: promptName.trim(),
        task_type: taskType,
      });
      toast({ kind: "success", message: `Created prompt "${newPrompt.name}"` });
      setShowPromptModal(false);
      setPromptName("");
      setTaskType("content_draft");
      await loadPrompts();
      handleSelectPrompt(newPrompt);
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setSaving(false);
    }
  }

  async function handleCreateVersion(e: FormEvent) {
    e.preventDefault();
    if (!selectedPrompt || saving) return;
    if (!template.trim()) {
      toast({ kind: "error", message: "Template is required" });
      return;
    }

    let inputSchema = {};
    let outputSchema = {};
    let params = {};
    let safetyPolicy = {};

    setSaving(true);
    try {
      if (inputSchemaJson.trim()) inputSchema = JSON.parse(inputSchemaJson);
      if (outputSchemaJson.trim()) outputSchema = JSON.parse(outputSchemaJson);
      if (paramsJson.trim()) params = JSON.parse(paramsJson);
      if (safetyPolicyJson.trim()) safetyPolicy = JSON.parse(safetyPolicyJson);
    } catch (err) {
      toast({ kind: "error", message: `Invalid JSON in schema/params: ${errorMessage(err)}` });
      return;
    }

    try {
      const newVersion = await createPromptVersion(baseURL, apiKey, selectedPrompt.id, {
        template: template.trim(),
        provider: provider.trim(),
        model: model.trim(),
        input_schema: inputSchema,
        output_schema: outputSchema,
        params,
        safety_policy: safetyPolicy,
      });
      toast({ kind: "success", message: `Created version v${newVersion.version}` });
      setShowVersionModal(false);
      setTemplate("");
      setInputSchemaJson("{}");
      setOutputSchemaJson("{}");
      setParamsJson("{}");
      setSafetyPolicyJson("{}");
      await loadVersions(selectedPrompt.id);
      await loadPrompts();
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setSaving(false);
    }
  }

  async function handleRunEval(version: PromptVersion, evalStatus: string = "passed") {
    if (!selectedPrompt || saving) return;
    setSaving(true);
    try {
      const updated = await setPromptVersionEvalStatus(
        baseURL,
        apiKey,
        selectedPrompt.id,
        version.version,
        evalStatus
      );
      toast({
        kind: "success",
        message: `Updated eval status to "${updated.eval_status}" for v${updated.version}`,
      });
      await loadVersions(selectedPrompt.id);
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setSaving(false);
    }
  }

  async function handlePublishConfirm() {
    if (!selectedPrompt || !publishTarget || saving) return;
    setSaving(true);
    try {
      const published = await publishPromptVersion(
        baseURL,
        apiKey,
        selectedPrompt.id,
        publishTarget.version
      );
      toast({
        kind: "success",
        message: `Published prompt version v${published.version}`,
      });
      setPublishTarget(null);
      await loadVersions(selectedPrompt.id);
      await loadPrompts();
    } catch (err) {
      toast({ kind: "error", message: errorMessage(err) });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="prompts-section" style={{ display: "flex", flexDirection: "column", gap: "24px" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: "24px", fontWeight: "bold" }}>Prompt Management</h1>
          <p style={{ margin: "4px 0 0 0", color: "var(--color-ink-muted)" }}>
            Author, version, evaluate, and human-publish governed prompt templates.
          </p>
        </div>
        <Button variant="primary" disabled={saving} onClick={() => setShowPromptModal(true)}>
          {saving ? "Saving…" : "New Prompt"}
        </Button>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "320px 1fr", gap: "24px" }}>
        {/* Left Column: Prompts List */}
        <Card>
          <h2 style={{ margin: "0 0 16px 0", fontSize: "18px" }}>Prompts</h2>
          {loadingPrompts ? (
            <p>Loading prompts...</p>
          ) : prompts.length === 0 ? (
            <EmptyState
              title="No prompts yet"
              description="Create a prompt to start versioning and publishing templates."
              cta={{ label: "New prompt", onClick: () => setShowPromptModal(true) }}
            />
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
              {prompts.map((p) => {
                const isSelected = selectedPrompt?.id === p.id;
                return (
                  <button
                    key={p.id}
                    onClick={() => handleSelectPrompt(p)}
                    style={{
                      display: "flex",
                      flexDirection: "column",
                      alignItems: "flex-start",
                      padding: "12px",
                      borderRadius: "8px",
                      border: isSelected ? "1px solid var(--color-primary, #3b82f6)" : "1px solid rgba(255,255,255,0.1)",
                      background: isSelected ? "rgba(59, 130, 246, 0.1)" : "transparent",
                      cursor: "pointer",
                      textAlign: "left",
                      width: "100%",
                    }}
                  >
                    <div style={{ fontWeight: "bold", color: "var(--color-ink)" }}>{p.name}</div>
                    <div style={{ fontSize: "12px", color: "var(--color-ink-muted)", marginTop: "4px" }}>
                      Task: {p.task_type} • v{p.latest_version}
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </Card>

        {/* Right Column: Selected Prompt & Versions */}
        <div>
          {selectedPrompt ? (
            <Card>
              <h2 style={{ margin: 0, fontSize: "20px" }}>{selectedPrompt.name}</h2>
              <p style={{ margin: "4px 0 16px 0", color: "var(--color-ink-muted)", fontSize: "13px" }}>
                Task Type: {selectedPrompt.task_type} | ID: {selectedPrompt.id}
              </p>

              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "16px" }}>
                <h3 style={{ margin: 0, fontSize: "16px" }}>Versions</h3>
                <Button variant="secondary" onClick={() => setShowVersionModal(true)}>
                  Author New Version
                </Button>
              </div>

              {loadingVersions ? (
                <p>Loading prompt versions...</p>
              ) : versions.length === 0 ? (
                <EmptyState
                  title="No versions created"
                  description="Author a version to define prompt templates and schema bounds."
                  cta={{ label: "Create version", onClick: () => setShowVersionModal(true) }}
                />
              ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
                  {versions.map((v) => (
                    <div
                      key={v.id}
                      style={{
                        padding: "16px",
                        borderRadius: "8px",
                        border: "1px solid rgba(255,255,255,0.1)",
                        background: "rgba(0,0,0,0.2)",
                      }}
                    >
                      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                          <span style={{ fontWeight: "bold", fontSize: "16px" }}>v{v.version}</span>
                          <Badge kind={v.status === "active" ? "published" : "draft"}>
                            {v.status}
                          </Badge>
                          <Badge
                            kind={
                              v.eval_status === "passed"
                                ? "success"
                                : v.eval_status === "failed"
                                ? "danger"
                                : "warn"
                            }
                          >
                            eval: {v.eval_status}
                          </Badge>
                        </div>
                        <div style={{ display: "flex", gap: "8px" }}>
                          <Button
                            variant="secondary"
                            onClick={() => handleRunEval(v, "passed")}
                            disabled={saving || v.eval_status === "passed"}
                          >
                            {v.eval_status === "passed" ? "Eval Passed" : "Run Eval"}
                          </Button>
                          <Button
                            variant="primary"
                            onClick={() => setPublishTarget(v)}
                            disabled={saving || v.status === "active"}
                          >
                            {v.status === "active" ? "Published" : "Publish"}
                          </Button>
                        </div>
                      </div>

                      <div style={{ marginTop: "12px", fontSize: "13px" }}>
                        <div><strong>Provider/Model:</strong> {v.provider} / {v.model}</div>
                        <div style={{ marginTop: "8px" }}>
                          <strong>Template:</strong>
                          <pre
                            style={{
                              margin: "4px 0 0 0",
                              padding: "8px",
                              background: "rgba(0,0,0,0.3)",
                              borderRadius: "4px",
                              whiteSpace: "pre-wrap",
                              fontSize: "12px",
                            }}
                          >
                            {v.template}
                          </pre>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          ) : (
            <Card>
              <h2 style={{ margin: "0 0 16px 0", fontSize: "18px" }}>Prompt Details</h2>
              <EmptyState
                title="No prompt selected"
                description="Select a prompt from the list to view and author prompt versions."
                cta={{ label: "New prompt", onClick: () => setShowPromptModal(true) }}
              />
            </Card>
          )}
        </div>
      </div>

      {/* Modal: New Prompt */}
      <Modal isOpen={showPromptModal} onClose={() => setShowPromptModal(false)}>
        <form onSubmit={handleCreatePrompt} style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
          <h2>Create New Prompt</h2>
          <Field label="Prompt Name">
            <Input
              value={promptName}
              onChange={(e) => setPromptName(e.target.value)}
              placeholder="e.g. Content Draft Prompt"
              required
            />
          </Field>
          <Field label="Task Type">
            <Select value={taskType} onChange={(e) => setTaskType(e.target.value)}>
              {TASK_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </Select>
          </Field>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px", marginTop: "16px" }}>
            <Button variant="secondary" onClick={() => setShowPromptModal(false)} type="button">
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={saving}>
              {saving ? "Creating…" : "Create Prompt"}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Modal: Author New Version */}
      <Modal isOpen={showVersionModal} onClose={() => setShowVersionModal(false)}>
        <form onSubmit={handleCreateVersion} style={{ display: "flex", flexDirection: "column", gap: "16px", maxWidth: "600px" }}>
          <h2>Author New Version for {selectedPrompt?.name}</h2>
          <Field label="Prompt Template">
            <Textarea
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder="Enter the prompt template text..."
              rows={5}
              required
            />
          </Field>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "16px" }}>
            <Field label="Provider">
              <Input value={provider} onChange={(e) => setProvider(e.target.value)} placeholder="mock" />
            </Field>
            <Field label="Model">
              <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="mock-model" />
            </Field>
          </div>
          <Field label="Input Schema (JSON)">
            <JsonField
              value={inputSchemaJson}
              onChange={(e) => setInputSchemaJson(e.target.value)}
              placeholder="{}"
            />
          </Field>
          <Field label="Output Schema (JSON)">
            <JsonField
              value={outputSchemaJson}
              onChange={(e) => setOutputSchemaJson(e.target.value)}
              placeholder="{}"
            />
          </Field>
          <Field label="Params (JSON)">
            <JsonField
              value={paramsJson}
              onChange={(e) => setParamsJson(e.target.value)}
              placeholder="{}"
            />
          </Field>
          <Field label="Safety Policy (JSON)">
            <JsonField
              value={safetyPolicyJson}
              onChange={(e) => setSafetyPolicyJson(e.target.value)}
              placeholder="{}"
            />
          </Field>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px", marginTop: "16px" }}>
            <Button variant="secondary" onClick={() => setShowVersionModal(false)} type="button">
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={saving}>
              {saving ? "Creating…" : "Create Version"}
            </Button>
          </div>
        </form>
      </Modal>

      {/* ConfirmDialog: Human-Gated Publish */}
      <ConfirmDialog
        isOpen={Boolean(publishTarget)}
        onClose={() => setPublishTarget(null)}
        onConfirm={handlePublishConfirm}
        title="Publish Prompt Version"
        message={`Confirm publishing version v${publishTarget?.version} of "${selectedPrompt?.name}"? Prompt publishing is human-gated and promotes this version to active status.`}
        confirmText="Publish Version"
      />
    </div>
  );
}
