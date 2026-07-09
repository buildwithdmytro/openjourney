import React, { FormEvent, useEffect, useState, useCallback, useMemo } from "react";
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  addEdge,
  Connection,
  Edge as FlowEdge,
  Node as FlowNode,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  listJourneys,
  createJourney,
  updateJourney,
  publishJourney,
  listSegments,
  listTemplates,
  listUsers,
  Journey,
  Segment,
  Template,
  User,
  JourneyVersion,
} from "../api";

const apiBase = import.meta.env.VITE_API_BASE_URL || "/api";

function message(cause: unknown): string {
  if (cause instanceof Error) return cause.message;
  return String(cause);
}

const getNodeStyle = (type: string, selected: boolean) => {
  const base = {
    padding: "10px",
    borderRadius: "8px",
    fontSize: "12px",
    fontWeight: "bold" as const,
    border: "2px solid #ccc",
    background: "#fff",
    color: "#222",
    boxShadow: selected ? "0 0 8px rgba(26, 115, 232, 0.6)" : "none",
    width: 150,
    textAlign: "center" as const,
    cursor: "pointer",
  };
  switch (type) {
    case "entry":
      return { ...base, borderColor: "#187d56", background: "#e9f8f1", color: "#187d56" };
    case "exit":
      return { ...base, borderColor: "#d93025", background: "#fce8e6", color: "#c5221f" };
    case "message":
      return { ...base, borderColor: "#1a73e8", background: "#e8f0fe", color: "#1a73e8" };
    case "wait_event":
      return { ...base, borderColor: "#af52de", background: "#f5ecfc", color: "#af52de" };
    case "condition":
    case "split":
      return { ...base, borderColor: "#e27220", background: "#fef3eb", color: "#e27220" };
    default:
      return { ...base, borderColor: "#5f6368", background: "#f1f3f4", color: "#5f6368" };
  }
};

export default function Journeys({ apiKey }: { apiKey: string }) {
  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [segments, setSegments] = useState<Segment[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [users, setUsers] = useState<User[]>([]);

  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [successMsg, setSuccessMsg] = useState("");

  // List view state
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [graph, setGraph] = useState("{}");

  // Editor state
  const [editingJourney, setEditingJourney] = useState<Journey | null>(null);
  const [editorMode, setEditorMode] = useState<"visual" | "json">("visual");
  const [rawJSON, setRawJSON] = useState("");
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);
  const [approverId, setApproverId] = useState("");

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [jRes, sRes, tRes, uRes] = await Promise.all([
        listJourneys(apiBase, apiKey),
        listSegments(apiBase, apiKey),
        listTemplates(apiBase, apiKey),
        listUsers(apiBase, apiKey),
      ]);
      setJourneys(jRes);
      setSegments(sRes);
      setTemplates(tRes);
      setUsers(uRes);
      if (uRes.length > 0) {
        setApproverId(uRes[0].id);
      }
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (apiKey) void load();
  }, [apiKey]);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      let parsedGraph: Record<string, any>;
      try {
        parsedGraph = JSON.parse(graph || "{}");
      } catch (err) {
        throw new Error("Invalid graph JSON: " + message(err));
      }
      if (Object.keys(parsedGraph).length === 0) {
        parsedGraph = {
          entry_node_id: "n1",
          nodes: [
            { id: "n1", type: "entry", config: { trigger: "event", event_type: "signup" }, position: { x: 100, y: 100 } },
            { id: "n2", type: "exit", config: { reason: "completed" }, position: { x: 100, y: 300 } },
          ],
          edges: [
            { from: "n1", to: "n2", branch: "" }
          ],
        };
      }
      await createJourney(apiBase, apiKey, {
        name,
        description: description || undefined,
        graph: parsedGraph,
      });
      setGraph("{}");
      setName("");
      setDescription("");
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  };

  // Convert DB graph to React Flow state
  const openEditor = (journey: Journey) => {
    setEditingJourney(journey);
    setError("");
    setSuccessMsg("");
    setSelectedNodeId(null);
    setValidationErrors([]);

    const graph = (journey.graph || {}) as Record<string, any>;
    const rawNodes = (graph.nodes || []) as any[];
    const rawEdges = (graph.edges || []) as any[];

    const flowNodes: FlowNode[] = rawNodes.map((n, idx) => ({
      id: n.id,
      type: "default",
      position: n.position || { x: 100 + idx * 100, y: 100 + idx * 100 },
      data: { label: `${n.type}: ${n.id}`, config: n.config || {}, type: n.type },
      style: getNodeStyle(n.type, false),
    }));

    const flowEdges: FlowEdge[] = rawEdges.map((e, idx) => ({
      id: `${e.from}-${e.to}-${idx}`,
      source: e.from,
      target: e.to,
      label: e.branch || undefined,
    }));

    setNodes(flowNodes);
    setEdges(flowEdges);
    setRawJSON(JSON.stringify(journey.graph, null, 2));
  };

  const closeEditor = () => {
    setEditingJourney(null);
    setSelectedNodeId(null);
  };

  const onConnect = useCallback((params: Connection) => {
    setEdges((eds) => addEdge({ ...params, label: "" }, eds));
  }, [setEdges]);

  // Sync node style selection
  const onNodeClick = useCallback((_: any, node: FlowNode) => {
    setSelectedNodeId(node.id);
    setNodes((nds) =>
      nds.map((n) => ({
        ...n,
        style: getNodeStyle(n.data.type as string, n.id === node.id),
      }))
    );
  }, [setNodes]);

  // Convert React Flow nodes/edges back to DB graph
  const getGraphJSON = (): Record<string, any> => {
    const entryNode = nodes.find((n) => n.data.type === "entry");
    return {
      entry_node_id: entryNode ? entryNode.id : "",
      nodes: nodes.map((n) => ({
        id: n.id,
        type: n.data.type,
        config: n.data.config,
        position: n.position,
      })),
      edges: edges.map((e) => ({
        from: e.source,
        to: e.target,
        branch: e.label ? String(e.label) : "",
      })),
    };
  };

  const validateGraph = (): boolean => {
    const errs: string[] = [];
    const entryNode = nodes.find((n) => n.data.type === "entry");
    if (!entryNode) errs.push("Graph must contain exactly one 'entry' node.");
    const exits = nodes.filter((n) => n.data.type === "exit");
    if (exits.length === 0) errs.push("Graph must contain at least one 'exit' node.");

    // Check outgoing edge contracts
    nodes.forEach((n) => {
      const type = n.data.type as string;
      const outgoing = edges.filter((e) => e.source === n.id);

      if (type === "exit") {
        if (outgoing.length > 0) errs.push(`Exit node '${n.id}' cannot have outgoing edges.`);
      } else if (type === "condition") {
        if (outgoing.length !== 2) {
          errs.push(`Condition node '${n.id}' must have exactly 2 outgoing branches.`);
        }
        const labels = outgoing.map((e) => e.label);
        if (!labels.includes("true") || !labels.includes("false")) {
          errs.push(`Condition node '${n.id}' outgoing branches must be labeled 'true' and 'false'.`);
        }
      } else if (type === "split") {
        const config = n.data.config as any;
        const branches = config.branches || [];
        if (outgoing.length !== branches.length) {
          errs.push(`Split node '${n.id}' has ${outgoing.length} edges but config has ${branches.length} branches.`);
        }
        const edgeLabels = outgoing.map((e) => e.label || "");
        branches.forEach((br: any) => {
          if (!edgeLabels.includes(br.label)) {
            errs.push(`Split node '${n.id}' is missing outgoing edge for branch '${br.label}'.`);
          }
        });
      } else if (type === "wait_event") {
        if (outgoing.length !== 2) {
          errs.push(`Wait Event node '${n.id}' must have exactly 2 outgoing branches.`);
        }
        const labels = outgoing.map((e) => e.label);
        if (!labels.includes("success") || !labels.includes("timeout")) {
          errs.push(`Wait Event node '${n.id}' branches must be labeled 'success' and 'timeout'.`);
        }
      } else {
        // entry, delay, action, message, goal
        if (outgoing.length !== 1) {
          errs.push(`Node '${n.id}' (${type}) must have exactly one outgoing connection.`);
        }
        if (outgoing.length === 1 && outgoing[0].label) {
          errs.push(`Node '${n.id}' (${type}) outgoing edge must not have a branch label.`);
        }
      }
    });

    setValidationErrors(errs);
    return errs.length === 0;
  };

  const handleSave = async () => {
    if (!editingJourney) return;
    setSaving(true);
    setError("");
    setSuccessMsg("");
    try {
      let graphData: Record<string, any>;
      if (editorMode === "json") {
        try {
          graphData = JSON.parse(rawJSON);
        } catch (e) {
          throw new Error("Invalid Raw JSON: " + message(e));
        }
      } else {
        graphData = getGraphJSON();
      }

      await updateJourney(apiBase, apiKey, editingJourney.id, {
        graph: graphData,
      });
      setSuccessMsg("Journey saved successfully.");
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  };

  const handlePublish = async () => {
    if (!editingJourney) return;
    setError("");
    setSuccessMsg("");
    if (!validateGraph()) {
      setError("Cannot publish: graph validation failed.");
      return;
    }
    setSaving(true);
    try {
      await publishJourney(apiBase, apiKey, editingJourney.id, approverId);
      setSuccessMsg("Journey published successfully!");
      await load();
      closeEditor();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  };

  // Node editing actions
  const selectedNode = useMemo(() => {
    return nodes.find((n) => n.id === selectedNodeId) || null;
  }, [nodes, selectedNodeId]);

  const updateSelectedNodeConfig = (key: string, value: any) => {
    if (!selectedNodeId) return;
    setNodes((nds) =>
      nds.map((n) => {
        if (n.id === selectedNodeId) {
          const config = { ...(n.data.config as Record<string, any>), [key]: value };
          return {
            ...n,
            data: { ...n.data, config },
          };
        }
        return n;
      })
    );
  };

  const addNode = (type: string) => {
    const id = `n_${Date.now().toString().slice(-4)}`;
    let defaultConfig: Record<string, any> = {};
    if (type === "delay") defaultConfig = { duration: "1h" };
    if (type === "condition") defaultConfig = { dsl: { field: "country", operator: "equals", value: "US" } };
    if (type === "split") defaultConfig = { mode: "random", branches: [{ label: "a", weight: 50 }, { label: "b", weight: 50 }] };
    if (type === "message") defaultConfig = { template_id: "", channel: "email", transactional: false };
    if (type === "wait_event") defaultConfig = { event_type: "email.opened", timeout: "24h" };
    if (type === "action") defaultConfig = { action: "profile_update", set: {} };
    if (type === "goal") defaultConfig = { name: "signup" };
    if (type === "exit") defaultConfig = { reason: "completed" };

    const newNode: FlowNode = {
      id,
      type: "default",
      position: { x: 200, y: 200 },
      data: { label: `${type}: ${id}`, config: defaultConfig, type },
      style: getNodeStyle(type, false),
    };
    setNodes((nds) => [...nds, newNode]);
    setSelectedNodeId(id);
  };

  const deleteSelectedNode = () => {
    if (!selectedNodeId) return;
    if (nodes.find((n) => n.id === selectedNodeId)?.data.type === "entry") {
      setError("Cannot delete the entry node.");
      return;
    }
    setNodes((nds) => nds.filter((n) => n.id !== selectedNodeId));
    setEdges((eds) => eds.filter((e) => e.source !== selectedNodeId && e.target !== selectedNodeId));
    setSelectedNodeId(null);
  };

  // Rendering configs in sidebar
  const renderNodeConfigPanel = () => {
    if (!selectedNode) {
      return <p className="muted">Select a node in the canvas to edit its properties.</p>;
    }
    const type = selectedNode.data.type as string;
    const config = selectedNode.data.config as any;

    return (
      <div className="stack" style={{ gap: "1rem" }}>
        <h3>Edit Node: {selectedNode.id} ({type})</h3>
        <label>Node ID
          <input
            value={selectedNode.id}
            readOnly
            disabled
            style={{ background: "#f1f3f4", cursor: "not-allowed" }}
          />
        </label>

        {type === "entry" && (
          <>
            <label>Trigger Type
              <select
                value={config.trigger || "event"}
                onChange={(e) => updateSelectedNodeConfig("trigger", e.target.value)}
              >
                <option value="event">Event Trigger</option>
                <option value="scheduled">Scheduled/Segment</option>
              </select>
            </label>
            {config.trigger === "event" ? (
              <label>Event Type
                <input
                  value={config.event_type || ""}
                  onChange={(e) => updateSelectedNodeConfig("event_type", e.target.value)}
                  placeholder="signup.completed"
                />
              </label>
            ) : (
              <>
                <label>Segment
                  <select
                    value={config.segment_id || ""}
                    onChange={(e) => updateSelectedNodeConfig("segment_id", e.target.value)}
                  >
                    <option value="">-- Select Segment --</option>
                    {segments.map((s) => (
                      <option key={s.id} value={s.id}>{s.name}</option>
                    ))}
                  </select>
                </label>
                <label>Schedule (Cron/Interval)
                  <input
                    value={config.schedule || ""}
                    onChange={(e) => updateSelectedNodeConfig("schedule", e.target.value)}
                    placeholder="*/5 * * * *"
                  />
                </label>
                <label>Reentry Policy
                  <select
                    value={config.reentry_policy || "once"}
                    onChange={(e) => updateSelectedNodeConfig("reentry_policy", e.target.value)}
                  >
                    <option value="once">Once</option>
                    <option value="always">Always</option>
                    <option value="after_exit">After Exit</option>
                  </select>
                </label>
              </>
            )}
          </>
        )}

        {type === "delay" && (
          <label>Duration
            <input
              value={config.duration || ""}
              onChange={(e) => updateSelectedNodeConfig("duration", e.target.value)}
              placeholder="2h"
            />
          </label>
        )}

        {type === "condition" && (
          <label>Audience Condition (DSL JSON)
            <textarea
              value={JSON.stringify(config.dsl || {}, null, 2)}
              onChange={(e) => {
                try {
                  const parsed = JSON.parse(e.target.value);
                  updateSelectedNodeConfig("dsl", parsed);
                } catch {
                  // Allow typing invalid json momentarily
                }
              }}
              rows={6}
              style={{ fontFamily: "monospace" }}
            />
          </label>
        )}

        {type === "split" && (
          <>
            <label>Mode
              <select
                value={config.mode || "random"}
                onChange={(e) => updateSelectedNodeConfig("mode", e.target.value)}
              >
                <option value="random">Random Weight %</option>
                <option value="audience">Audience/Segment Membership</option>
              </select>
            </label>
            <h4>Branches</h4>
            {(config.branches || []).map((br: any, idx: number) => (
              <div key={idx} style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.5rem", borderBottom: "1px solid #eee", paddingBottom: "0.5rem" }}>
                <label>Label
                  <input
                    value={br.label}
                    onChange={(e) => {
                      const copy = [...config.branches];
                      copy[idx] = { ...br, label: e.target.value };
                      updateSelectedNodeConfig("branches", copy);
                    }}
                  />
                </label>
                {config.mode === "random" ? (
                  <label>Weight %
                    <input
                      type="number"
                      value={br.weight || 0}
                      onChange={(e) => {
                        const copy = [...config.branches];
                        copy[idx] = { ...br, weight: parseInt(e.target.value) || 0 };
                        updateSelectedNodeConfig("branches", copy);
                      }}
                    />
                  </label>
                ) : (
                  <label>Segment
                    <select
                      value={br.segment_id || ""}
                      onChange={(e) => {
                        const copy = [...config.branches];
                        copy[idx] = { ...br, segment_id: e.target.value };
                        updateSelectedNodeConfig("branches", copy);
                      }}
                    >
                      <option value="">-- Select Segment --</option>
                      {segments.map((s) => (
                        <option key={s.id} value={s.id}>{s.name}</option>
                      ))}
                    </select>
                  </label>
                )}
              </div>
            ))}
            <button
              type="button"
              onClick={() => {
                const branches = config.branches || [];
                const label = String.fromCharCode(97 + branches.length); // a, b, c...
                const nextBranch = config.mode === "random" ? { label, weight: 0 } : { label, segment_id: "" };
                updateSelectedNodeConfig("branches", [...branches, nextBranch]);
              }}
            >
              Add Branch
            </button>
          </>
        )}

        {type === "message" && (
          <>
            <label>Template
              <select
                value={config.template_id || ""}
                onChange={(e) => {
                  const tmpl = templates.find((t) => t.id === e.target.value);
                  updateSelectedNodeConfig("template_id", e.target.value);
                  if (tmpl) {
                    updateSelectedNodeConfig("channel", tmpl.channel);
                  }
                }}
              >
                <option value="">-- Select Template --</option>
                {templates.map((t) => (
                  <option key={t.id} value={t.id}>{t.name} ({t.channel})</option>
                ))}
              </select>
            </label>
            <label style={{ flexDirection: "row", alignItems: "center", gap: "0.5rem" }}>
              <input
                type="checkbox"
                checked={config.transactional || false}
                onChange={(e) => updateSelectedNodeConfig("transactional", e.target.checked)}
              />
              Bypass quiet hours / fatigue (Transactional)
            </label>
          </>
        )}

        {type === "wait_event" && (
          <>
            <label>Wait Event Type
              <input
                value={config.event_type || ""}
                onChange={(e) => updateSelectedNodeConfig("event_type", e.target.value)}
                placeholder="email.opened"
              />
            </label>
            <label>Timeout Duration
              <input
                value={config.timeout || ""}
                onChange={(e) => updateSelectedNodeConfig("timeout", e.target.value)}
                placeholder="48h"
              />
            </label>
          </>
        )}

        {type === "action" && (
          <>
            <label>Action Type
              <select
                value={config.action || "profile_update"}
                onChange={(e) => updateSelectedNodeConfig("action", e.target.value)}
              >
                <option value="profile_update">Profile Attribute Update</option>
              </select>
            </label>
            <label>Attributes to Set (JSON)
              <textarea
                value={JSON.stringify(config.set || {}, null, 2)}
                onChange={(e) => {
                  try {
                    const parsed = JSON.parse(e.target.value);
                    updateSelectedNodeConfig("set", parsed);
                  } catch {
                    // Momentary invalid JSON
                  }
                }}
                rows={6}
                style={{ fontFamily: "monospace" }}
              />
            </label>
          </>
        )}

        {type === "goal" && (
          <label>Goal Name
            <input
              value={config.name || ""}
              onChange={(e) => updateSelectedNodeConfig("name", e.target.value)}
              placeholder="signup"
            />
          </label>
        )}

        {type === "exit" && (
          <label>Exit Reason
            <input
              value={config.reason || ""}
              onChange={(e) => updateSelectedNodeConfig("reason", e.target.value)}
              placeholder="completed"
            />
          </label>
        )}

        <button
          type="button"
          className="danger"
          style={{ marginTop: "1rem" }}
          onClick={deleteSelectedNode}
        >
          Delete Node
        </button>
      </div>
    );
  };

  if (editingJourney) {
    return (
      <section className="stack" style={{ height: "100%" }}>
        <div className="section-title">
          <div>
            <div className="eyebrow">Durable Journey Graph Builder</div>
            <h2>{editingJourney.name} <span className={`pill ${editingJourney.status}`}>{editingJourney.status}</span></h2>
            {editingJourney.description && <p className="muted">{editingJourney.description}</p>}
          </div>
          <div style={{ display: "flex", gap: "1rem", alignItems: "center" }}>
            <select value={editorMode} onChange={(e) => setEditorMode(e.target.value as any)}>
              <option value="visual">Visual Builder</option>
              <option value="json">Raw JSON Fallback</option>
            </select>
            <button onClick={validateGraph} disabled={editorMode === "json"}>Validate</button>
            <button onClick={handleSave} disabled={saving} className="primary">
              {saving ? "Saving..." : "Save Draft"}
            </button>
            {editingJourney.status === "draft" && (
              <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", borderLeft: "1px solid #ccc", paddingLeft: "1rem" }}>
                <select value={approverId} onChange={(e) => setApproverId(e.target.value)}>
                  {users.map((u) => (
                    <option key={u.id} value={u.id}>{u.email || u.display_name}</option>
                  ))}
                </select>
                <button onClick={handlePublish} disabled={saving} style={{ background: "#187d56", color: "#fff" }}>
                  Publish
                </button>
              </div>
            )}
            <button onClick={closeEditor} className="secondary">Back</button>
          </div>
        </div>

        <ErrorMessage value={error} />
        {successMsg && <div style={{ color: "#187d56", background: "#e9f8f1", padding: "10px", borderRadius: "5px", fontWeight: "bold" }}>{successMsg}</div>}

        {validationErrors.length > 0 && (
          <div style={{ color: "#c5221f", background: "#fce8e6", padding: "10px", borderRadius: "5px" }}>
            <h4>Graph Validation Errors:</h4>
            <ul>
              {validationErrors.map((err, idx) => (
                <li key={idx}>{err}</li>
              ))}
            </ul>
          </div>
        )}

        <div style={{ display: "grid", gridTemplateColumns: editorMode === "visual" ? "3fr 1fr" : "1fr", gap: "1.5rem", height: "600px" }}>
          {editorMode === "visual" ? (
            <>
              <div style={{ border: "1px solid #dadce0", borderRadius: "8px", position: "relative", height: "100%", background: "#f8f9fa" }}>
                <ReactFlow
                  nodes={nodes}
                  edges={edges}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onConnect={onConnect}
                  onNodeClick={onNodeClick}
                  fitView
                >
                  <Background color="#ccc" gap={16} />
                  <Controls />
                  <MiniMap />
                </ReactFlow>

                <div style={{ position: "absolute", top: 10, left: 10, zIndex: 4, display: "flex", flexWrap: "wrap", gap: "0.5rem", background: "rgba(255,255,255,0.9)", padding: "5px", borderRadius: "5px", border: "1px solid #ccc" }}>
                  <button onClick={() => addNode("delay")}>+ Delay</button>
                  <button onClick={() => addNode("condition")}>+ Condition</button>
                  <button onClick={() => addNode("split")}>+ Split</button>
                  <button onClick={() => addNode("message")}>+ Message</button>
                  <button onClick={() => addNode("wait_event")}>+ Wait Event</button>
                  <button onClick={() => addNode("action")}>+ Action</button>
                  <button onClick={() => addNode("goal")}>+ Goal</button>
                  <button onClick={() => addNode("exit")}>+ Exit</button>
                </div>
              </div>

              <article className="card" style={{ height: "100%", overflowY: "auto" }}>
                {renderNodeConfigPanel()}

                {selectedNode && (
                  <div style={{ marginTop: "2rem", borderTop: "1px solid #eee", paddingTop: "1rem" }}>
                    <h4>Edit Outgoing Edge Labels</h4>
                    <p className="muted" style={{ fontSize: "11px" }}>If selected node is a Condition, Split, or Wait Event, outgoing edges must carry a branch label.</p>
                    {edges.filter(e => e.source === selectedNode.id).map((e) => (
                      <label key={e.id} style={{ display: "block", marginBottom: "0.5rem" }}>
                        To Node {e.target} Branch Label
                        <input
                          value={e.label ? String(e.label) : ""}
                          onChange={(evt) => {
                            setEdges((eds) =>
                              eds.map((edge) =>
                                edge.id === e.id ? { ...edge, label: evt.target.value } : edge
                              )
                            );
                          }}
                        />
                      </label>
                    ))}
                  </div>
                )}
              </article>
            </>
          ) : (
            <textarea
              value={rawJSON}
              onChange={(e) => setRawJSON(e.target.value)}
              style={{ width: "100%", height: "100%", fontFamily: "monospace", fontSize: "12px", border: "1px solid #ccc", padding: "10px", borderRadius: "5px" }}
            />
          )}
        </div>
      </section>
    );
  }

  return (
    <section className="stack">
      <div style={{ display: "grid", gridTemplateColumns: "1fr 2fr", gap: "2rem" }}>
        <article className="card" style={{ height: "fit-content" }}>
          <h2>Create journey</h2>
          <form onSubmit={handleCreate} className="schema-form" style={{ gridTemplateColumns: "1fr" }}>
            <label>Name
              <input value={name} onChange={event => setName(event.target.value)} required placeholder="Welcome Series" />
            </label>
            <label>Description
              <input value={description} onChange={event => setDescription(event.target.value)} placeholder="Activation flow" />
            </label>
            <label>Graph
              <textarea value={graph} onChange={event => setGraph(event.target.value)} rows={6} />
            </label>
            <button type="submit" disabled={saving || !apiKey || !name.trim()}>
              {saving ? "Saving..." : "Create journey"}
            </button>
          </form>
          <ErrorMessage value={error} />
        </article>

        <article className="card">
          <div className="section-title">
            <div>
              <div className="eyebrow">Durable workflows</div>
              <h2>Journeys ({journeys.length})</h2>
            </div>
            <button onClick={() => void load()} disabled={!apiKey || loading}>
              {loading ? "Loading..." : "Refresh"}
            </button>
          </div>
          {loading && <p>Loading journeys...</p>}
          {!loading && journeys.length === 0 && <p className="muted">No journeys configured.</p>}
          {!loading && journeys.length > 0 && (
            <div style={{ overflowX: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Status</th>
                    <th>Latest version</th>
                    <th>Updated</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {journeys.map(journey => (
                    <tr key={journey.id}>
                      <td>
                        <strong>{journey.name}</strong>
                        {journey.description && <div style={{ fontSize: "11px", color: "var(--muted)" }}>{journey.description}</div>}
                      </td>
                      <td><span className={`pill ${journey.status}`}>{journey.status}</span></td>
                      <td>{journey.latest_version}</td>
                      <td>{new Date(journey.updated_at).toLocaleString()}</td>
                      <td>
                        <button onClick={() => openEditor(journey)} className="small secondary">
                          Edit Graph
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </article>
      </div>
    </section>
  );
}

function ErrorMessage({ value }: { value: string }) {
  if (!value) return null;
  return (
    <div style={{ color: "#a93838", background: "#fff0f0", padding: "10px", borderRadius: "5px", border: "1px solid #dadce0" }}>
      {value}
    </div>
  );
}
