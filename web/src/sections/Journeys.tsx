import React, { FormEvent, useEffect, useState, useCallback, useMemo, useRef } from "react";
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
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  EdgeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  listJourneys,
  createJourney,
  updateJourney,
  publishJourney,
  listSegments,
  listTemplates,
  listSchemas,
  getJourneyVersion,
  updateJourneyVersionStatus,
  cancelJourneyRun,
  listJourneyRuns,
  listJourneyRunTransitions,
  listJourneyDLQ,
  retryJourneyDLQ,
  Journey,
  Segment,
  Template,
  JourneyVersion,
  JourneyRun,
  JourneyTransition,
  JourneyStep,
  JourneyMessageIntent,
  EventSchema,
} from "../api";
import { journeyColors } from "../tokens";
import { ConfirmDialog, EmptyState, Spinner } from "../components";
import { useMediaQuery } from "../hooks/useMediaQuery";

const apiBase = import.meta.env.VITE_API_BASE_URL || "/api";

type InsertableEdgeData = { onInsert?: (edgeID: string) => void };

function InsertableEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd, style, label, data }: EdgeProps) {
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition });
  const edgeData = data as InsertableEdgeData | undefined;
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} style={style} />
      <EdgeLabelRenderer>
        <div className="insert-edge-control" style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}>
          {label && <span>{String(label)}</span>}
          <button
            type="button"
            aria-label="Add step on this connection"
            title="Add a step here"
            onClick={(event) => { event.stopPropagation(); edgeData?.onInsert?.(id); }}
          >+</button>
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

const journeyEdgeTypes = { insertable: InsertableEdge };

const stepCatalog = [
  { type: "message", icon: "✉", title: "Send a message", description: "Send an email or webhook from a template", group: "Engage" },
  { type: "delay", icon: "◷", title: "Wait", description: "Pause before moving to the next step", group: "Timing" },
  { type: "wait_event", icon: "◎", title: "Wait for an event", description: "Continue when a customer takes an action", group: "Timing" },
  { type: "condition", icon: "◇", title: "Decision", description: "Create Yes and No paths using customer data", group: "Paths" },
  { type: "split", icon: "⑂", title: "Split paths", description: "Divide customers by percentage or audience", group: "Paths" },
  { type: "ai_decision", icon: "✦", title: "AI decision", description: "Choose a declared path with a deterministic fallback", group: "Paths" },
  { type: "action", icon: "↻", title: "Update profile", description: "Save a value on the customer profile", group: "Data" },
  { type: "goal", icon: "⚑", title: "Mark a goal", description: "Record a successful journey outcome", group: "Finish" },
  { type: "exit", icon: "✓", title: "Exit journey", description: "End this path", group: "Finish" },
] as const;

const stepMeta = (type: string) => stepCatalog.find((step) => step.type === type) ||
  { type, icon: "•", title: type.replaceAll("_", " "), description: "Journey step", group: "Other" };

function nodeSummary(type: string, config: Record<string, any>): string {
  if (type === "entry") return config.trigger === "scheduled" ? "Scheduled audience entry" : `When ${config.event_type || "an event occurs"}`;
  if (type === "delay") return `Wait ${config.duration || "for a while"}`;
  if (type === "message") return config.template_id ? "Template selected" : "Choose a message template";
  if (type === "wait_event") return `Wait for ${config.event_type || "an event"}`;
  if (type === "condition") return `${config.dsl?.field || "Customer"} ${config.dsl?.operator || "matches"} ${config.dsl?.value ?? "a value"}`;
  if (type === "split") return `${config.branches?.length || 2} customer paths`;
  if (type === "ai_decision") return `${config.branches?.length || 2} paths · fallback: ${config.fallback || "required"}`;
  if (type === "action") return "Update customer attributes";
  if (type === "goal") return config.name || "Journey goal";
  if (type === "exit") return config.reason || "Journey complete";
  return "Configure this step";
}

function nodeLabel(type: string, config: Record<string, any>) {
  const meta = type === "entry" ? { icon: "→", title: "Journey entry" } : stepMeta(type);
  return (
    <div className="journey-node-label">
      <span className={`journey-node-icon ${type}`}>{meta.icon}</span>
      <span><strong>{meta.title}</strong><small>{nodeSummary(type, config)}</small></span>
    </div>
  );
}

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
    border: "2px solid var(--color-border-default)",
    background: journeyColors.white,
    color: journeyColors.black,
    boxShadow: selected ? "0 0 8px var(--color-primary-shadow)" : "none",
    width: 230,
    textAlign: "left" as const,
    cursor: "pointer",
  };
  switch (type) {
    case "entry":
      return { ...base, borderColor: journeyColors.successBorder, background: journeyColors.successBg, color: journeyColors.successText };
    case "exit":
      return { ...base, borderColor: journeyColors.errorBorder, background: journeyColors.errorBg, color: journeyColors.errorText };
    case "message":
      return { ...base, borderColor: journeyColors.infoBorder, background: journeyColors.infoBg, color: journeyColors.infoText };
    case "wait_event":
      return { ...base, borderColor: journeyColors.purpleBorder, background: journeyColors.purpleBg, color: journeyColors.purpleText };
    case "condition":
    case "split":
    case "ai_decision":
      return { ...base, borderColor: journeyColors.orangeBorder, background: journeyColors.orangeBg, color: journeyColors.orangeText };
    default:
      return { ...base, borderColor: journeyColors.neutralBorder, background: journeyColors.neutralBg, color: journeyColors.neutralText };
  }
};

export default function Journeys({ apiKey }: { apiKey: string }) {
  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [segments, setSegments] = useState<Segment[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [schemas, setSchemas] = useState<EventSchema[]>([]);

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
  const [editorMode, setEditorMode] = useState<"visual" | "json" | "operator">("visual");
  const [rawJSON, setRawJSON] = useState("");
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);
  const [isDirty, setIsDirty] = useState(false);
  const [confirmDiscardChanges, setConfirmDiscardChanges] = useState(false);
  const isNarrowViewport = useMediaQuery("(max-width: 760px)");
  const undoHistory = useRef<Array<{ nodes: FlowNode[]; edges: FlowEdge[] }>>([]);
  const redoHistory = useRef<Array<{ nodes: FlowNode[]; edges: FlowEdge[] }>>([]);
  const selectInsertionEdge = useCallback((edgeID: string) => {
    setSelectedEdgeId(edgeID);
    setSelectedNodeId(null);
  }, []);

  // Operator states
  const [runs, setRuns] = useState<JourneyRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<JourneyRun | null>(null);
  const [transitions, setTransitions] = useState<JourneyTransition[]>([]);
  const [dlqSteps, setDlqSteps] = useState<JourneyStep[]>([]);
  const [dlqIntents, setDlqIntents] = useState<JourneyMessageIntent[]>([]);
  const [loadingOps, setLoadingOps] = useState(false);
  const [activeVersion, setActiveVersion] = useState<JourneyVersion | null>(null);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [jRes, sRes, tRes, schemaRes] = await Promise.all([
        listJourneys(apiBase, apiKey),
        listSegments(apiBase, apiKey),
        listTemplates(apiBase, apiKey),
        listSchemas(apiBase, apiKey),
      ]);
      setJourneys(jRes ?? []);
      setSegments(sRes ?? []);
      setTemplates(tRes ?? []);
      setSchemas(schemaRes ?? []);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (apiKey) void load();
  }, [apiKey]);

  const loadOperations = useCallback(async (journeyId: string, latestVersion: number) => {
    setLoadingOps(true);
    setError("");
    try {
      const [runsList, dlqData] = await Promise.all([
        listJourneyRuns(apiBase, apiKey, journeyId),
        listJourneyDLQ(apiBase, apiKey)
      ]);
      setRuns(runsList);
      
      // Filter DLQ steps and intents for this journey
      const journeySteps = (dlqData.steps ?? []).filter(s => (runsList ?? []).some(r => r.id === s.run_id));
      const journeyIntents = (dlqData.intents ?? []).filter(i => i.journey_id === journeyId);
      setDlqSteps(journeySteps);
      setDlqIntents(journeyIntents);

      if (editingJourney?.current_version_id) {
        try {
          const ver = await getJourneyVersion(apiBase, apiKey, journeyId, latestVersion);
          setActiveVersion(ver);
        } catch {
          setActiveVersion(null);
        }
      } else {
        setActiveVersion(null);
      }
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoadingOps(false);
    }
  }, [apiKey, editingJourney]);

  useEffect(() => {
    if (editingJourney && editorMode === "operator") {
      void loadOperations(editingJourney.id, editingJourney.latest_version);
    }
  }, [editingJourney, editorMode, loadOperations]);

  const handlePauseToggle = async () => {
    if (!editingJourney || !activeVersion) return;
    setSaving(true);
    setError("");
    try {
      const nextStatus = activeVersion.status === "paused" ? "active" : "paused";
      await updateJourneyVersionStatus(apiBase, apiKey, editingJourney.id, activeVersion.version, nextStatus);
      setSuccessMsg(`Journey version ${activeVersion.version} status updated to ${nextStatus}`);
      await loadOperations(editingJourney.id, editingJourney.latest_version);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  };

  const handleCancelRun = async (runID: string) => {
    if (!editingJourney) return;
    setError("");
    try {
      await cancelJourneyRun(apiBase, apiKey, editingJourney.id, runID);
      setSuccessMsg("Run cancelled successfully");
      await loadOperations(editingJourney.id, editingJourney.latest_version);
      if (selectedRun?.id === runID) {
        setSelectedRun(null);
      }
    } catch (cause) {
      setError(message(cause));
    }
  };

  const handleRetryDLQ = async (kind: string, id: string) => {
    if (!editingJourney) return;
    setError("");
    try {
      await retryJourneyDLQ(apiBase, apiKey, kind, id);
      setSuccessMsg("Replay request accepted; item status set to pending");
      await loadOperations(editingJourney.id, editingJourney.latest_version);
    } catch (cause) {
      setError(message(cause));
    }
  };

  const selectRun = async (run: JourneyRun) => {
    setSelectedRun(run);
    setError("");
    try {
      const transList = await listJourneyRunTransitions(apiBase, apiKey, editingJourney!.id, run.id);
      setTransitions(transList);
    } catch (cause) {
      setError(message(cause));
    }
  };

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
      const created = await createJourney(apiBase, apiKey, {
        name,
        description: description || undefined,
        graph: parsedGraph,
      });
      setGraph("{}");
      setName("");
      setDescription("");
      await load();
      openEditor(created);
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
    setSelectedEdgeId(null);
    undoHistory.current = [];
    redoHistory.current = [];
    setIsDirty(false);
    setValidationErrors([]);

    const graph = (journey.graph || {}) as Record<string, any>;
    const rawNodes = (graph.nodes || []) as any[];
    const rawEdges = (graph.edges || []) as any[];

    const flowNodes: FlowNode[] = rawNodes.map((n, idx) => ({
      id: n.id,
      type: "default",
      position: n.position || { x: 100 + idx * 100, y: 100 + idx * 100 },
      data: { label: nodeLabel(n.type, n.config || {}), config: n.config || {}, type: n.type },
      style: getNodeStyle(n.type, false),
    }));

    const flowEdges: FlowEdge[] = rawEdges.map((e, idx) => ({
      id: `${e.from}-${e.to}-${idx}`,
      source: e.from,
      target: e.to,
      label: e.branch || undefined,
      type: "insertable",
      data: { onInsert: selectInsertionEdge },
    }));

    setNodes(flowNodes);
    setEdges(flowEdges);
    setRawJSON(JSON.stringify(journey.graph, null, 2));
  };

  const closeEditor = () => {
    if (isDirty) {
      setConfirmDiscardChanges(true);
    } else {
      performCloseEditor();
    }
  };

  const performCloseEditor = () => {
    setEditingJourney(null);
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
    setConfirmDiscardChanges(false);
  };

  const onConnect = useCallback((params: Connection) => {
    undoHistory.current.push({ nodes, edges });
    redoHistory.current = [];
    setIsDirty(true);
    setEdges((eds) => addEdge({ ...params, label: "", type: "insertable", data: { onInsert: selectInsertionEdge } }, eds));
  }, [nodes, edges, setEdges, selectInsertionEdge]);

  // Sync node style selection
  const onNodeClick = useCallback((_: any, node: FlowNode) => {
    setSelectedNodeId(node.id);
    setSelectedEdgeId(null);
    setNodes((nds) =>
      nds.map((n) => ({
        ...n,
        style: getNodeStyle(n.data.type as string, n.id === node.id),
      }))
    );
  }, [setNodes]);

  const onEdgeClick = useCallback((_: any, edge: FlowEdge) => {
    setSelectedEdgeId(edge.id);
    setSelectedNodeId(null);
  }, []);

  const rememberCanvas = useCallback(() => {
    undoHistory.current.push({ nodes, edges });
    if (undoHistory.current.length > 50) undoHistory.current.shift();
    redoHistory.current = [];
    setIsDirty(true);
  }, [nodes, edges]);

  const undoCanvas = useCallback(() => {
    const previous = undoHistory.current.pop();
    if (!previous) return;
    redoHistory.current.push({ nodes, edges });
    setNodes(previous.nodes);
    setEdges(previous.edges);
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
    setIsDirty(true);
  }, [nodes, edges, setNodes, setEdges]);

  const redoCanvas = useCallback(() => {
    const next = redoHistory.current.pop();
    if (!next) return;
    undoHistory.current.push({ nodes, edges });
    setNodes(next.nodes);
    setEdges(next.edges);
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
    setIsDirty(true);
  }, [nodes, edges, setNodes, setEdges]);

  useEffect(() => {
    if (!isDirty) return;
    const warnBeforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [isDirty]);

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
      } else if (type === "ai_decision") {
        const config = n.data.config as any;
        const branches = Array.isArray(config.branches) ? config.branches : [];
        const edgeLabels = outgoing.map((e) => e.label || "");
        if (!config.prompt_version_id) errs.push(`AI decision node '${n.id}' requires a pinned prompt version.`);
        if (!config.timeout_ms || Number(config.timeout_ms) <= 0 || Number(config.timeout_ms) > 5000) errs.push(`AI decision node '${n.id}' timeout must be between 1 and 5000 ms.`);
        if (!config.max_cost_cents || Number(config.max_cost_cents) <= 0) errs.push(`AI decision node '${n.id}' requires a positive cost cap.`);
        if (!config.fallback || !branches.includes(config.fallback)) errs.push(`AI decision node '${n.id}' fallback must be one of its declared branches.`);
        if (outgoing.length !== branches.length || branches.some((branch: string) => !edgeLabels.includes(branch))) errs.push(`AI decision node '${n.id}' needs one outgoing edge for every declared branch.`);
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
      setIsDirty(false);
      undoHistory.current = [];
      redoHistory.current = [];
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
      await publishJourney(apiBase, apiKey, editingJourney.id);
      setIsDirty(false);
      setSuccessMsg("Journey published successfully!");
      await load();
      setEditingJourney(null);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  };

  const handleCreateDraft = async () => {
    if (!editingJourney) return;
    setSaving(true);
    setError("");
    try {
      const draft = await updateJourney(apiBase, apiKey, editingJourney.id, { status: "draft", graph: getGraphJSON() });
      setEditingJourney(draft);
      setIsDirty(false);
      setSuccessMsg("An editable draft was created from the published journey.");
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

  const suggestedCustomerFields = useMemo(() => {
    const fields = new Set(["country", "email", "first_name", "last_name", "language", "timezone"]);
    schemas.forEach((eventSchema) => {
      const root = eventSchema.schema as { properties?: Record<string, unknown> };
      const payload = root.properties?.payload as { properties?: Record<string, unknown> } | undefined;
      Object.keys(payload?.properties || root.properties || {}).forEach((field) => {
        if (field !== "payload") fields.add(field);
      });
    });
    return [...fields].sort();
  }, [schemas]);

  const updateSelectedNodeConfig = (key: string, value: any) => {
    if (!selectedNodeId || editingJourney?.status !== "draft") return;
    rememberCanvas();
    setNodes((nds) =>
      nds.map((n) => {
        if (n.id === selectedNodeId) {
          const config = { ...(n.data.config as Record<string, any>), [key]: value };
          return {
            ...n,
            data: { ...n.data, config, label: nodeLabel(n.data.type as string, config) },
          };
        }
        return n;
      })
    );
  };

  const addNode = (type: string) => {
    if (editingJourney?.status !== "draft") return;
    rememberCanvas();
    const id = `n_${crypto.randomUUID()}`;
    let defaultConfig: Record<string, any> = {};
    if (type === "delay") defaultConfig = { duration: "1h" };
    if (type === "condition") defaultConfig = { dsl: { field: "country", operator: "equals", value: "US" } };
    if (type === "split") defaultConfig = { mode: "random", branches: [{ label: "a", weight: 50 }, { label: "b", weight: 50 }] };
    if (type === "ai_decision") defaultConfig = { prompt_version_id: "", timeout_ms: 1000, max_cost_cents: 10, branches: ["yes", "no"], fallback: "no" };
    if (type === "message") defaultConfig = { template_id: "", channel: "email", transactional: false };
    if (type === "wait_event") defaultConfig = { event_type: "email.opened", timeout: "24h" };
    if (type === "action") defaultConfig = { action: "profile_update", set: {} };
    if (type === "goal") defaultConfig = { name: "signup" };
    if (type === "exit") defaultConfig = { reason: "completed" };

    const selectedEdge = edges.find((edge) => edge.id === selectedEdgeId);
    const anchor = nodes.find((node) => node.id === selectedNodeId) || nodes.find((node) => node.id === selectedEdge?.source);
    const canInsertAfterAnchor = anchor && !["condition", "split", "ai_decision", "wait_event", "exit"].includes(anchor.data.type as string);
    const insertionEdge = selectedEdge || (canInsertAfterAnchor ? edges.find((edge) => edge.source === anchor.id) : undefined);
    const edgeTarget = nodes.find((node) => node.id === insertionEdge?.target);
    const branchLabels = type === "condition" ? ["true", "false"] : type === "wait_event" ? ["success", "timeout"] : type === "ai_decision" ? ["yes", "no"] : type === "split" ? ["a", "b"] : [];
    const fallbackID = branchLabels.length > 0 && insertionEdge ? `n_${crypto.randomUUID()}` : "";
    const downstreamNodeIDs = new Set<string>();
    if (insertionEdge) {
      const queue = [insertionEdge.target];
      while (queue.length > 0) {
        const nodeID = queue.shift()!;
        if (downstreamNodeIDs.has(nodeID)) continue;
        downstreamNodeIDs.add(nodeID);
        edges.filter((edge) => edge.source === nodeID).forEach((edge) => queue.push(edge.target));
      }
    }
    const newNode: FlowNode = {
      id,
      type: "default",
      position: insertionEdge && edgeTarget
        ? { x: edgeTarget.position.x, y: edgeTarget.position.y }
        : anchor ? { x: anchor.position.x, y: anchor.position.y + 170 } : { x: 260, y: 180 + nodes.length * 120 },
      data: { label: nodeLabel(type, defaultConfig), config: defaultConfig, type },
      style: getNodeStyle(type, true),
    };
    const fallbackNode: FlowNode | null = fallbackID && edgeTarget ? {
      id: fallbackID,
      type: "default",
      position: { x: edgeTarget.position.x + 300, y: edgeTarget.position.y + 190 },
      data: { label: nodeLabel("exit", { reason: `${branchLabels[1]} path complete` }), config: { reason: `${branchLabels[1]} path complete` }, type: "exit" },
      style: getNodeStyle("exit", false),
    } : null;
    setNodes((nds) => [
      ...nds.map((node) => downstreamNodeIDs.has(node.id)
        ? { ...node, position: { ...node.position, y: node.position.y + 190 } }
        : node),
      newNode,
      ...(fallbackNode ? [fallbackNode] : []),
    ]);
    if (selectedEdge) {
      setEdges((currentEdges) => [
        ...currentEdges.filter((edge) => edge.id !== selectedEdge.id),
        { ...selectedEdge, id: `${selectedEdge.source}-${id}`, target: id },
        { id: `${id}-${selectedEdge.target}`, source: id, target: selectedEdge.target, label: branchLabels[0], type: "insertable", data: { onInsert: selectInsertionEdge } },
        ...(fallbackID ? [{ id: `${id}-${fallbackID}`, source: id, target: fallbackID, label: branchLabels[1], type: "insertable", data: { onInsert: selectInsertionEdge } }] : []),
      ]);
    } else if (canInsertAfterAnchor) {
      setEdges((currentEdges) => {
        const outgoing = currentEdges.find((edge) => edge.source === anchor.id);
        const withoutOutgoing = outgoing ? currentEdges.filter((edge) => edge.id !== outgoing.id) : currentEdges;
        const inserted = [{ id: `${anchor.id}-${id}`, source: anchor.id, target: id, type: "insertable", data: { onInsert: selectInsertionEdge } } as FlowEdge];
        if (outgoing) inserted.push({ ...outgoing, id: `${id}-${outgoing.target}`, source: id, label: branchLabels[0], data: { onInsert: selectInsertionEdge } });
        if (fallbackID) inserted.push({ id: `${id}-${fallbackID}`, source: id, target: fallbackID, label: branchLabels[1], type: "insertable", data: { onInsert: selectInsertionEdge } });
        return [...withoutOutgoing, ...inserted];
      });
    }
    setSelectedNodeId(id);
    setSelectedEdgeId(null);
  };

  const deleteSelectedNode = () => {
    if (editingJourney?.status !== "draft") return;
    if (selectedEdgeId) {
      rememberCanvas();
      setEdges((current) => current.filter((edge) => edge.id !== selectedEdgeId));
      setSelectedEdgeId(null);
      return;
    }
    if (!selectedNodeId) return;
    const nodeToDelete = nodes.find((n) => n.id === selectedNodeId);
    if (nodeToDelete?.data.type === "entry") {
      setError("Cannot delete the entry node.");
      return;
    }
    rememberCanvas();
    setNodes((nds) => nds.filter((n) => n.id !== selectedNodeId));
    setEdges((currentEdges) => {
      const incoming = currentEdges.filter((edge) => edge.target === selectedNodeId);
      const outgoing = currentEdges.filter((edge) => edge.source === selectedNodeId);
      const remaining = currentEdges.filter((edge) => edge.source !== selectedNodeId && edge.target !== selectedNodeId);
      if (incoming.length !== 1 || outgoing.length !== 1) return remaining;
      const before = incoming[0];
      const after = outgoing[0];
      return [...remaining, {
        id: `${before.source}-${after.target}-${crypto.randomUUID()}`,
        source: before.source,
        target: after.target,
        label: before.label,
        type: "insertable",
        data: { onInsert: selectInsertionEdge },
      }];
    });
    setSelectedNodeId(null);
  };

  useEffect(() => {
    if (!editingJourney || editingJourney.status !== "draft" || editorMode !== "visual") return;
    const handleCanvasShortcut = (event: KeyboardEvent) => {
      const target = event.target;
      const isTyping = target instanceof Element && target.matches("input, textarea, select, [contenteditable='true']");
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z" && event.shiftKey) {
        event.preventDefault();
        redoCanvas();
        return;
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        undoCanvas();
        return;
      }
      if (!isTyping && (event.key === "Delete" || event.key === "Backspace")) {
        event.preventDefault();
        deleteSelectedNode();
      }
    };
    window.addEventListener("keydown", handleCanvasShortcut);
    return () => window.removeEventListener("keydown", handleCanvasShortcut);
  }, [editingJourney, editorMode, selectedNodeId, selectedEdgeId, undoCanvas, redoCanvas]);

  // Rendering configs in sidebar
  const renderNodeConfigPanel = () => {
    if (editingJourney?.status !== "draft") return <div className="journey-inspector-empty"><span>🔒</span><h3>Published journey</h3><p>This version is read-only. Create an editable draft to make changes.</p></div>;
    if (selectedEdgeId) {
      return (
        <div className="journey-inspector-empty edge-selected">
          <span>＋</span><h3>Insert a step here</h3>
          <p>Choose any step from the left. The connection will be rewired automatically.</p>
          <button className="danger" onClick={deleteSelectedNode}>Delete connection</button>
        </div>
      );
    }
    if (!selectedNode) {
      return (
        <div className="journey-inspector-empty">
          <span>↖</span>
          <h3>Select a step</h3>
          <p>Choose a card on the canvas to review or change what happens at that point in the journey.</p>
        </div>
      );
    }
    const type = selectedNode.data.type as string;
    const config = selectedNode.data.config as any;

    return (
      <div className="journey-inspector-form">
        <div className="journey-inspector-heading">
          <span className={`journey-node-icon ${type}`}>{type === "entry" ? "→" : stepMeta(type).icon}</span>
          <div><span>Step settings</span><h3>{type === "entry" ? "Journey entry" : stepMeta(type).title}</h3></div>
        </div>
        <p className="journey-help">{type === "entry" ? "Choose who enters this journey and when." : stepMeta(type).description}</p>

        {type === "entry" && (
          <>
            <label>How should customers enter?
              <select
                value={config.trigger || "event"}
                onChange={(e) => updateSelectedNodeConfig("trigger", e.target.value)}
              >
                <option value="event">When they perform an event</option>
                <option value="scheduled">On a recurring schedule</option>
              </select>
            </label>
            {config.trigger === "event" ? (
              <label>Event name
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
                <label>Schedule
                  <input
                    value={config.schedule || ""}
                    onChange={(e) => updateSelectedNodeConfig("schedule", e.target.value)}
                    placeholder="* * * * *"
                  />
                  <small className="field-help">Use * * * * * for every minute or */15 * * * * for every 15 minutes.</small>
                </label>
                <label>Can customers enter again?
                  <select
                    value={config.reentry_policy || "once"}
                    onChange={(e) => updateSelectedNodeConfig("reentry_policy", e.target.value)}
                  >
                    <option value="once">No, only once</option>
                    <option value="always">Yes, every time</option>
                    <option value="after_exit">Yes, after they finish</option>
                  </select>
                </label>
              </>
            )}
          </>
        )}

        {type === "delay" && (
          <label>How long should customers wait?
            <input
              value={config.duration || ""}
              onChange={(e) => updateSelectedNodeConfig("duration", e.target.value)}
              placeholder="2h"
            />
          </label>
        )}

        {type === "condition" && (
          <div className="condition-builder">
            <span className="field-title">Send customers down “Yes” when</span>
            <label>Customer field
              <input
                className="field-drop-target"
                value={config.dsl?.field || ""}
                placeholder="Drag a field here or type its name"
                list="journey-customer-fields"
                onDragOver={(event) => { event.preventDefault(); event.currentTarget.classList.add("drag-over"); }}
                onDragLeave={(event) => event.currentTarget.classList.remove("drag-over")}
                onDrop={(event) => {
                  event.preventDefault();
                  event.currentTarget.classList.remove("drag-over");
                  const field = event.dataTransfer.getData("application/x-openjourney-field") || event.dataTransfer.getData("text/plain");
                  if (field) updateSelectedNodeConfig("dsl", { ...config.dsl, field });
                }}
                onChange={(e) => updateSelectedNodeConfig("dsl", { ...config.dsl, field: e.target.value })}
              />
              <datalist id="journey-customer-fields">{suggestedCustomerFields.map((field) => <option key={field} value={field} />)}</datalist>
            </label>
            <label>Comparison
              <select value={config.dsl?.operator || "equals"} onChange={(e) => updateSelectedNodeConfig("dsl", { ...config.dsl, operator: e.target.value })}>
                <option value="equals">is equal to</option><option value="not_equals">is not equal to</option>
                <option value="contains">contains</option><option value="exists">has any value</option>
              </select>
            </label>
            {config.dsl?.operator !== "exists" && <label>Value
              <input value={config.dsl?.value ?? ""} placeholder="US" onChange={(e) => updateSelectedNodeConfig("dsl", { ...config.dsl, value: e.target.value })} />
            </label>}
          </div>
        )}

        {type === "ai_decision" && (
          <>
            <label>Pinned prompt version
              <input value={config.prompt_version_id || ""} onChange={(e) => updateSelectedNodeConfig("prompt_version_id", e.target.value)} placeholder="prompt-version-uuid" required />
            </label>
            <label>Decision timeout (ms)
              <input type="number" min="1" max="5000" value={config.timeout_ms || 1000} onChange={(e) => updateSelectedNodeConfig("timeout_ms", Number(e.target.value))} />
            </label>
            <label>Maximum cost (cents)
              <input type="number" min="1" value={config.max_cost_cents || 1} onChange={(e) => updateSelectedNodeConfig("max_cost_cents", Number(e.target.value))} />
            </label>
            <label>Declared branches
              <input value={(config.branches || []).join(", ")} onChange={(e) => updateSelectedNodeConfig("branches", e.target.value.split(",").map((branch: string) => branch.trim()).filter(Boolean))} placeholder="yes, no" />
            </label>
            <label>Deterministic fallback
              <select value={config.fallback || ""} onChange={(e) => updateSelectedNodeConfig("fallback", e.target.value)}>
                <option value="">Select fallback</option>
                {(config.branches || []).map((branch: string) => <option key={branch} value={branch}>{branch}</option>)}
              </select>
            </label>
            <small className="field-help">Provider timeout, budget, schema, and errors advance on this fallback branch. Model failures never retry the journey step.</small>
          </>
        )}

        {type === "split" && (
          <>
            <label>How should customers be divided?
              <select
                value={config.mode || "random"}
                onChange={(e) => updateSelectedNodeConfig("mode", e.target.value)}
              >
                <option value="random">Random percentage</option>
                <option value="audience">By audience membership</option>
              </select>
            </label>
            <h4>Paths</h4>
            {(config.branches || []).map((br: any, idx: number) => (
              <div key={idx} style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.5rem", borderBottom: "1px solid var(--color-border-light)", paddingBottom: "0.5rem" }}>
                <label>Path name
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
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={config.transactional || false}
                onChange={(e) => updateSelectedNodeConfig("transactional", e.target.checked)}
              />
              This is a transactional message
            </label>
            <small className="field-help">Transactional messages can bypass marketing quiet hours and frequency limits.</small>
          </>
        )}

        {type === "wait_event" && (
          <>
            <label>Event to wait for
              <input
                value={config.event_type || ""}
                onChange={(e) => updateSelectedNodeConfig("event_type", e.target.value)}
                placeholder="email.opened"
              />
            </label>
            <label>Stop waiting after
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
          Delete step
        </button>
      </div>
    );
  };

  if (editingJourney) {
    return (
      <>
      <ConfirmDialog
        isOpen={confirmDiscardChanges}
        onClose={() => setConfirmDiscardChanges(false)}
        onConfirm={performCloseEditor}
        title="Discard unsaved changes?"
        message="All unsaved changes to this journey will be lost."
        confirmText="Discard"
        isDangerous={true}
      />
      <section className="journey-workspace">
        {isNarrowViewport && (
          <div style={{ padding: "12px 18px", backgroundColor: "var(--color-warn-bg)", borderBottom: "1px solid var(--color-warn)", color: "var(--color-warn-text)" }} role="alert">
            <strong>This journey builder is best viewed on a larger screen.</strong> You can still view and edit this journey on a desktop or landscape tablet for the optimal experience.
          </div>
        )}
        <div className="journey-topbar">
          <div className="journey-title-block">
            <button onClick={closeEditor} className="icon-button" aria-label="Back to journeys">←</button>
            <div>
              <div className="journey-title-line"><h2>{editingJourney.name}</h2><span className={`pill ${editingJourney.status}`}>{editingJourney.status}</span></div>
              <p>{editingJourney.description || "Build and publish a customer journey"}</p>
            </div>
          </div>
          <div className="journey-actions">
            <button onClick={validateGraph} disabled={editorMode === "json"} className="secondary">Check journey</button>
            {editingJourney.status === "draft" ? <button onClick={handleSave} disabled={saving} className="secondary">
              {saving ? "Saving..." : isDirty ? "Save changes" : "Saved"}
            </button> : <button onClick={handleCreateDraft} disabled={saving} className="secondary">Create editable draft</button>}
            {editingJourney.status === "draft" && (
              <button onClick={handlePublish} disabled={saving} className="publish-button">Publish journey</button>
            )}
          </div>
        </div>

        <nav className="journey-tabs" aria-label="Journey workspace">
          <button className={editorMode === "visual" ? "active" : ""} onClick={() => setEditorMode("visual")}>Journey</button>
          <button className={editorMode === "operator" ? "active" : ""} onClick={() => setEditorMode("operator")}>Activity</button>
          <button className={editorMode === "json" ? "active" : ""} onClick={() => setEditorMode("json")}>Advanced</button>
        </nav>

        <ErrorMessage value={error} />
        {successMsg && <div style={{ color: journeyColors.successText, background: journeyColors.successBg, padding: "10px", borderRadius: "5px", fontWeight: "bold" }}>{successMsg}</div>}

        {validationErrors.length > 0 && (
          <div className="journey-validation" role="alert">
            <div><strong>{validationErrors.length} {validationErrors.length === 1 ? "item needs" : "items need"} attention</strong><span>Fix these before publishing.</span></div>
            <ul>
              {validationErrors.map((err, idx) => (
                <li key={idx}>{err}</li>
              ))}
            </ul>
          </div>
        )}

        <div className={`journey-editor-layout ${editorMode}`}>
          {editorMode === "visual" && (
            <>
              <aside className="journey-step-library">
                <div><span className="eyebrow">Add a step</span><h3>What happens next?</h3><p>Choose a step, then connect it to your journey.</p></div>
                {["Engage", "Timing", "Paths", "Data", "Finish"].map((group) => (
                  <div className="step-group" key={group}>
                    <span>{group}</span>
                    {stepCatalog.filter((step) => step.group === group).map((step) => (
                      <button key={step.type} disabled={editingJourney.status !== "draft"} onClick={() => addNode(step.type)} className="step-library-item">
                        <span className={`journey-node-icon ${step.type}`}>{step.icon}</span>
                        <span><strong>{step.title}</strong><small>{step.description}</small></span>
                        <b>+</b>
                      </button>
                    ))}
                  </div>
                ))}
                <div className="customer-field-library">
                  <div><span>Customer fields</span><small>Suggested from your available event schemas</small></div>
                  <p>Drag a field into a Decision step.</p>
                  <div className="field-chips">
                    {suggestedCustomerFields.map((field) => (
                      <button
                        type="button"
                        draggable
                        key={field}
                        onDragStart={(event) => {
                          event.dataTransfer.setData("application/x-openjourney-field", field);
                          event.dataTransfer.setData("text/plain", field);
                          event.dataTransfer.effectAllowed = "copy";
                        }}
                        onClick={() => {
                          if (selectedNode?.data.type === "condition") {
                            const config = selectedNode.data.config as Record<string, any>;
                            updateSelectedNodeConfig("dsl", { ...(config.dsl || {}), field });
                          }
                        }}
                        title={selectedNode?.data.type === "condition" ? `Use ${field}` : "Select a Decision step, then click or drag"}
                      >
                        ⋮⋮ {field}
                      </button>
                    ))}
                  </div>
                </div>
              </aside>

              <div className="journey-canvas">
                <div className={`canvas-guidance ${selectedEdgeId ? "insert-mode" : ""}`}>
                  {selectedEdgeId ? "Connection selected — choose a step to insert it here" : selectedNode ? "New linear steps are inserted after the selected step" : "Select a step or connection, then choose what happens next"}
                </div>
                <ReactFlow
                  nodes={nodes}
                  edges={edges}
                  edgeTypes={journeyEdgeTypes}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onNodeDragStart={() => rememberCanvas()}
                  nodesDraggable={editingJourney.status === "draft"}
                  nodesConnectable={editingJourney.status === "draft"}
                  onConnect={onConnect}
                  onNodeClick={onNodeClick}
                  onEdgeClick={onEdgeClick}
                  onPaneClick={() => { setSelectedNodeId(null); setSelectedEdgeId(null); }}
                  deleteKeyCode={null}
                  fitView
                >
                  <Background color={journeyColors.canvasGridColor} gap={24} size={1} />
                  <Controls />
                  <MiniMap />
                </ReactFlow>
                <div className="canvas-history-controls"><button onClick={undoCanvas} disabled={undoHistory.current.length === 0} title="Undo (Ctrl/⌘+Z)">↶ Undo</button><button onClick={redoCanvas} disabled={redoHistory.current.length === 0} title="Redo (Ctrl/⌘+Shift+Z)">↷ Redo</button></div>
              </div>

              <aside className="journey-inspector">
                {renderNodeConfigPanel()}

                {selectedNode && (
                  <details className="advanced-settings">
                    <summary>Path connection settings</summary>
                    <p className="field-help">Decision, split, and event steps need a label for every outgoing path.</p>
                    {edges.filter(e => e.source === selectedNode.id).map((e) => (
                      <label key={e.id}>
                        Path to {e.target}
                        <input
                          value={e.label ? String(e.label) : ""}
                          onChange={(evt) => {
                            rememberCanvas();
                            setEdges((eds) =>
                              eds.map((edge) =>
                                edge.id === e.id ? { ...edge, label: evt.target.value } : edge
                              )
                            );
                          }}
                        />
                      </label>
                    ))}
                  </details>
                )}
              </aside>
            </>
          )}

          {editorMode === "json" && (
            <textarea
              value={rawJSON}
              onChange={(e) => { setRawJSON(e.target.value); setIsDirty(true); }}
              className="journey-json-editor"
            />
          )}

          {editorMode === "operator" && (
            <>
              <div style={{ display: "flex", flexDirection: "column", gap: "1.5rem", height: "100%", overflowY: "auto" }}>
                {/* 1. Version Controls */}
                <article className="card">
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <div>
                      <h3>Active Version Control</h3>
                      {activeVersion ? (
                        <p className="muted" style={{ fontSize: "12px", margin: "5px 0 0" }}>
                          Version {activeVersion.version} · Published {new Date(activeVersion.published_at).toLocaleString()}
                        </p>
                      ) : (
                      <EmptyState title="No published versions yet" description="Publish a journey version to start sending customers through this flow." icon="info" cta={{ label: "Publish journey", onClick: () => void handlePublish() }} />
                      )}
                    </div>
                    {activeVersion && (
                      <div style={{ display: "flex", alignItems: "center", gap: "1rem" }}>
                        <span className={`pill ${activeVersion.status}`}>{activeVersion.status}</span>
                        <button
                          onClick={handlePauseToggle}
                          disabled={saving}
                          style={{
                            background: activeVersion.status === "paused" ? journeyColors.successBorder : journeyColors.orangeBorder,
                            color: journeyColors.white,
                            border: "none",
                            padding: "8px 16px",
                            borderRadius: "5px",
                            fontWeight: "bold",
                            cursor: "pointer"
                          }}
                        >
                          {saving ? "Updating..." : activeVersion.status === "paused" ? "Resume version" : "Pause version"}
                        </button>
                      </div>
                    )}
                  </div>
                </article>

                {/* 2. DLQ Section */}
                <article className="card">
                  <h3>Dead Letter Queue (DLQ)</h3>
                  {loadingOps ? (
                    <Spinner label="Loading DLQ…" />
                  ) : dlqSteps.length === 0 && dlqIntents.length === 0 ? (
                      <EmptyState title="No dead-letter items for this journey" icon="check" cta={{ label: "Refresh journey", onClick: () => window.location.reload() }} />
                  ) : (
                    <div style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
                      {dlqSteps.map((s) => (
                        <div key={s.id} className="key-row" style={{ padding: "10px", background: journeyColors.errorMessageBg, border: `1px solid ${journeyColors.errorMessageBorder}`, borderRadius: "5px" }}>
                          <div>
                            <strong>Step DLQ · Node: {s.node_id} ({s.kind})</strong>
                            <div style={{ fontSize: "11px", color: journeyColors.errorMessageText, marginTop: "4px" }}>
                              Run: {s.run_id} · Attempts: {s.attempts} · Error: {s.error_message || "no error info"}
                            </div>
                          </div>
                          <button className="small primary" onClick={() => handleRetryDLQ("step", s.id)}>Retry</button>
                        </div>
                      ))}
                      {dlqIntents.map((i) => (
                        <div key={i.id} className="key-row" style={{ padding: "10px", background: journeyColors.errorMessageBg, border: `1px solid ${journeyColors.errorMessageBorder}`, borderRadius: "5px" }}>
                          <div>
                            <strong>Intent DLQ · Node: {i.node_id} (Message: {i.channel})</strong>
                            <div style={{ fontSize: "11px", color: journeyColors.errorMessageText, marginTop: "4px" }}>
                              Run: {i.run_id} · Attempts: {i.attempts} · Endpoint: {i.endpoint} · Error: {i.error_message || "no error info"}
                            </div>
                          </div>
                          <button className="small primary" onClick={() => handleRetryDLQ("intent", i.id)}>Retry</button>
                        </div>
                      ))}
                    </div>
                  )}
                </article>

                {/* 3. Runs list */}
                <article className="card" style={{ flexGrow: 1 }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "1rem" }}>
                    <h3>Journey Runs ({runs.length})</h3>
                    <button className="small secondary" onClick={() => loadOperations(editingJourney.id, editingJourney.latest_version)} disabled={loadingOps}>
                      Refresh
                    </button>
                  </div>
                  {loadingOps ? (
                    <Spinner label="Loading runs…" />
                  ) : runs.length === 0 ? (
                      <EmptyState title="No runs started for this journey" description="Publish the journey and wait for an eligible profile." icon="info" cta={{ label: "Publish journey", onClick: () => void handlePublish() }} />
                  ) : (
                    <div style={{ overflowX: "auto" }}>
                      <table>
                        <thead>
                          <tr>
                            <th>Run ID / Profile</th>
                            <th>Status</th>
                            <th>Current Node</th>
                            <th>Entered At</th>
                            <th>Actions</th>
                          </tr>
                        </thead>
                        <tbody>
                          {runs.map((r) => (
                            <tr key={r.id} style={{ background: selectedRun?.id === r.id ? journeyColors.selectedRowBg : "transparent" }}>
                              <td>
                                <div style={{ fontWeight: "bold", fontSize: "12px" }}>{r.id.slice(0, 8)}...</div>
                                <div style={{ fontSize: "10px", color: "var(--muted)" }}>Ext: {r.subject_external_id}</div>
                              </td>
                              <td><span className={`pill ${r.status}`}>{r.status}</span></td>
                              <td><code>{r.current_node_id || "-"}</code></td>
                              <td style={{ fontSize: "11px" }}>{new Date(r.entered_at).toLocaleString()}</td>
                              <td>
                                <div style={{ display: "flex", gap: "0.5rem" }}>
                                  <button className="small secondary" onClick={() => selectRun(r)}>Inspect</button>
                                  {r.status === "active" && (
                                    <button className="small danger" onClick={() => handleCancelRun(r.id)}>Cancel</button>
                                  )}
                                </div>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </article>
              </div>

              {/* Inspector panel */}
              <article className="card" style={{ height: "100%", overflowY: "auto" }}>
                {selectedRun ? (
                  <div>
                    <h3>Run Inspector</h3>
                    <div style={{ fontSize: "12px", borderBottom: "1px solid var(--color-border-light)", paddingBottom: "1rem", marginBottom: "1rem" }}>
                      <div><strong>Run ID:</strong> {selectedRun.id}</div>
                      <div><strong>External Subject:</strong> {selectedRun.subject_external_id}</div>
                      <div><strong>Status:</strong> <span className={`pill ${selectedRun.status}`}>{selectedRun.status}</span></div>
                      <div><strong>Entered:</strong> {new Date(selectedRun.entered_at).toLocaleString()}</div>
                      {selectedRun.completed_at && (
                        <div><strong>Completed:</strong> {new Date(selectedRun.completed_at).toLocaleString()}</div>
                      )}
                    </div>

                    <h4>Transitions Timeline</h4>
                    {transitions.length === 0 ? (
                      <EmptyState title="No transitions recorded yet" description="Journey transitions appear after a run starts." icon="info" cta={{ label: "Refresh journey", onClick: () => window.location.reload() }} />
                    ) : (
                      <div style={{ display: "flex", flexDirection: "column", gap: "1rem", position: "relative", paddingLeft: "10px", borderLeft: "2px solid var(--color-border-light)" }}>
                        {transitions.map((t, idx) => (
                          <div key={t.id || idx} style={{ position: "relative", fontSize: "12px" }}>
                            <div style={{ position: "absolute", left: "-15px", top: "4px", width: "8px", height: "8px", borderRadius: "50%", background: journeyColors.dotBg }} />
                            <div style={{ fontWeight: "bold" }}>
                              {t.from_node ? `Node ${t.from_node}` : "Start"} &rarr; {t.to_node ? `Node ${t.to_node}` : "End"}
                            </div>
                            <div style={{ fontSize: "10px", color: "var(--muted)" }}>
                              {t.node_type} · Outcome: <strong>{t.outcome}</strong>
                            </div>
                            {t.detail && Object.keys(t.detail).length > 0 && (
                              <pre style={{ fontSize: "9px", margin: "4px 0 0", padding: "4px", background: journeyColors.codeBg, borderRadius: "3px", overflowX: "auto" }}>
                                {JSON.stringify(t.detail, null, 2)}
                              </pre>
                            )}
                            <div style={{ fontSize: "9px", color: "var(--muted)", marginTop: "2px" }}>
                              {new Date(t.occurred_at).toLocaleString()}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ) : (
                  <div style={{ display: "flex", height: "100%", alignItems: "center", justifyContent: "center", color: "var(--muted)", fontSize: "12px", textAlign: "center" }}>
                    Select a run to inspect its transition timeline.
                  </div>
                )}
              </article>
            </>
          )}
        </div>
      </section>
      </>
    );
  }

  return (
    <section className="journeys-home">
      <div className="journeys-home-hero">
        <div><span className="eyebrow">Customer journeys</span><h2>Turn customer moments into meaningful experiences</h2><p>Build automated, personal journeys without writing code.</p></div>
        <details className="create-journey-panel">
          <summary>+ Create journey</summary>
          <form onSubmit={handleCreate}>
            <label>Journey name<input value={name} onChange={event => setName(event.target.value)} required placeholder="New customer welcome" autoFocus /></label>
            <label>What is this journey for?<input value={description} onChange={event => setDescription(event.target.value)} placeholder="Help new customers reach their first success" /></label>
            <div className="starter-preview"><span>→</span><div><strong>Start with a simple journey</strong><small>We’ll create an entry and exit. Add messages, waits, and decisions in the visual builder.</small></div></div>
            <div className="form-actions"><button type="button" className="secondary" onClick={() => { setName(""); setDescription(""); }}>Clear</button><button type="submit" disabled={saving || !apiKey || !name.trim()}>{saving ? "Creating..." : "Create and design"}</button></div>
          </form>
        </details>
      </div>
      <ErrorMessage value={error} />

      <article className="journey-list-card">
          <div className="journey-list-header">
            <div>
              <h3>Your journeys</h3><span>{journeys.length} total</span>
            </div>
            <button className="secondary" onClick={() => void load()} disabled={!apiKey || loading}>
              {loading ? "Loading..." : "Refresh"}
            </button>
          </div>
          {loading && <Spinner label="Loading journeys…" />}
          {!loading && journeys.length === 0 && <div className="journey-empty"><span>⌁</span><h3>Create your first customer journey</h3><p>Start with a welcome flow, an abandoned-cart reminder, or any customer moment you want to automate.</p></div>}
          {!loading && journeys.length > 0 && (
            <div className="journey-grid">
              {journeys.map(journey => (
                <div className="journey-card-shell" key={journey.id}>
                  <button onClick={() => openEditor(journey)} className="journey-card">
                    <div className="journey-card-top"><span className="journey-card-icon">⌁</span><span className={`pill ${journey.status}`}>{journey.status}</span></div>
                    <div><h3>{journey.name}</h3><p>{journey.description || "No description yet"}</p></div>
                    <footer><span>Version {journey.latest_version || "Draft"}</span><span>Updated {new Date(journey.updated_at).toLocaleDateString()}</span><b>Open →</b></footer>
                  </button>
                  <a className="report-link journey-report-link" href={`#reports?type=journey&id=${encodeURIComponent(journey.id)}`}>View report</a>
                </div>
              ))}
            </div>
          )}
      </article>
    </section>
  );
}

function ErrorMessage({ value }: { value: string }) {
  if (!value) return null;
  return (
    <div style={{ color: journeyColors.danger, background: journeyColors.dangerBg, padding: "10px", borderRadius: "5px", border: `1px solid ${journeyColors.errorAlertBorder}` }}>
      {value}
    </div>
  );
}
