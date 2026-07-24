import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  Campaign,
  createExperiment,
  Experiment,
  ExperimentVariant,
  getExperiment,
  Journey,
  listCampaigns,
  listExperiments,
  listJourneys,
  listTemplates,
  Template,
  updateCampaign,
  updateExperiment,
  updateJourney,
  approveExperimentOptimization,
  proposeExperimentOptimization,
  OptimizationProposal,
} from "../api";
import { Card, DataTable, EmptyState, ErrorState, Spinner } from "../components";

type JourneyNode = {
  id: string;
  type: string;
  config?: Record<string, unknown>;
};

const defaultVariants = (): ExperimentVariant[] => [
  { label: "control", weight: 50, is_control: true },
  { label: "treatment", weight: 50, is_control: false },
];

function journeyNodes(journey: Journey): JourneyNode[] {
  const nodes = journey.graph.nodes;
  return Array.isArray(nodes) ? nodes as JourneyNode[] : [];
}

function message(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback;
}

export default function Experiments({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [items, setItems] = useState<Experiment[]>([]);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [editingID, setEditingID] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [subjectType, setSubjectType] = useState<Experiment["subject_type"]>("campaign");
  const [status, setStatus] = useState<Experiment["status"]>("draft");
  const [seed, setSeed] = useState<string>(() => crypto.randomUUID());
  const [holdoutPct, setHoldoutPct] = useState(0);
  const [variants, setVariants] = useState<ExperimentVariant[]>(defaultVariants);
  const [bindingID, setBindingID] = useState("");
  const [journeyNodeID, setJourneyNodeID] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [proposals, setProposals] = useState<Record<string, OptimizationProposal>>({});
  const [optimizingID, setOptimizingID] = useState("");

  const draftCampaigns = campaigns.filter((campaign) => campaign.status === "draft");
  const draftJourneys = journeys.filter((journey) => journey.status === "draft");
  const selectedJourney = journeys.find((journey) => journey.id === bindingID);
  const bindableNodes = useMemo(
    () => selectedJourney ? journeyNodes(selectedJourney).filter((node) => node.type === "split" || node.type === "message") : [],
    [selectedJourney],
  );
  const totalWeight = variants.reduce((sum, variant) => sum + Number(variant.weight || 0), 0);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [experimentList, campaignList, journeyList, templateList] = await Promise.all([
        listExperiments(baseURL, apiKey),
        listCampaigns(baseURL, apiKey),
        listJourneys(baseURL, apiKey),
        listTemplates(baseURL, apiKey),
      ]);
      setItems(experimentList);
      setCampaigns(campaignList);
      setJourneys(journeyList);
      setTemplates(templateList);
    } catch (cause) {
      setError(message(cause, "Unable to load experiments"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, [apiKey, baseURL]);

  function resetForm() {
    setEditingID("");
    setName("");
    setDescription("");
    setSubjectType("campaign");
    setStatus("draft");
    setSeed(crypto.randomUUID());
    setHoldoutPct(0);
    setVariants(defaultVariants());
    setBindingID("");
    setJourneyNodeID("");
  }

  async function edit(item: Experiment) {
    setError("");
    setSuccess("");
    try {
      const full = await getExperiment(baseURL, apiKey, item.id);
      setEditingID(full.id);
      setName(full.name);
      setDescription(full.description || "");
      setSubjectType(full.subject_type);
      setStatus(full.status);
      setSeed(full.seed);
      setHoldoutPct(full.holdout_pct);
      setVariants(full.variants?.length ? full.variants.map(({ label, weight, is_control, template_id }) => ({ label, weight, is_control, template_id })) : defaultVariants());
      const campaign = draftCampaigns.find((candidate) => candidate.experiment_id === full.id);
      const journey = draftJourneys.find((candidate) => journeyNodes(candidate).some((node) => node.config?.experiment_id === full.id));
      setBindingID(campaign?.id || journey?.id || "");
      setJourneyNodeID(journey ? journeyNodes(journey).find((node) => node.config?.experiment_id === full.id)?.id || "" : "");
    } catch (cause) {
      setError(message(cause, "Unable to load experiment"));
    }
  }

  function updateVariant(index: number, patch: Partial<ExperimentVariant>) {
    setVariants((current) => current.map((variant, candidate) => {
      if ("is_control" in patch && patch.is_control) {
        return candidate === index ? { ...variant, ...patch } : { ...variant, is_control: false };
      }
      return candidate === index ? { ...variant, ...patch } : variant;
    }));
  }

  function validate() {
    const labels = variants.map((variant) => variant.label.trim());
    if (variants.length < 2) throw new Error("Add at least two variants");
    if (labels.some((label) => !label)) throw new Error("Every variant needs a label");
    if (new Set(labels).size !== labels.length) throw new Error("Variant labels must be unique");
    if (variants.filter((variant) => variant.is_control).length !== 1) throw new Error("Choose exactly one control variant");
    if (variants.some((variant) => variant.weight < 0) || totalWeight <= 0) throw new Error("Variant weights must total more than zero");
    if (holdoutPct < 0 || holdoutPct > 100) throw new Error("Holdout must be between 0 and 100 percent");
    if (subjectType === "journey" && bindingID && !journeyNodeID) throw new Error("Choose a journey split or message node");
    if (subjectType === "journey" && bindingID && journeyNodeID) {
      const node = bindableNodes.find((candidate) => candidate.id === journeyNodeID);
      if (node?.type === "split") {
        const branches = Array.isArray(node.config?.branches) ? node.config.branches as Array<{ label?: string }> : [];
        const branchLabels = branches.map((branch) => branch.label).filter(Boolean).sort();
        if (branchLabels.join("|") !== [...labels].sort().join("|")) {
          throw new Error("Split branch labels must match the experiment variant labels");
        }
      }
    }
  }

  async function syncBinding(experimentID: string) {
    const selectedCampaignID = subjectType === "campaign" ? bindingID : "";
    const campaignUpdates = draftCampaigns.filter((campaign) =>
      campaign.experiment_id === experimentID || campaign.id === selectedCampaignID,
    ).map((campaign) => updateCampaign(baseURL, apiKey, campaign.id, {
      ...campaign,
      experiment_id: campaign.id === selectedCampaignID ? experimentID : null,
    }));

    const selectedJourneyID = subjectType === "journey" ? bindingID : "";
    const journeyUpdates = draftJourneys.flatMap((journey) => {
      let changed = false;
      const graph = JSON.parse(JSON.stringify(journey.graph)) as Record<string, unknown>;
      const nodes = Array.isArray(graph.nodes) ? graph.nodes as JourneyNode[] : [];
      for (const node of nodes) {
        const config = { ...(node.config || {}) };
        if (config.experiment_id === experimentID) {
          delete config.experiment_id;
          node.config = config;
          changed = true;
        }
        if (journey.id === selectedJourneyID && node.id === journeyNodeID) {
          node.config = { ...config, experiment_id: experimentID };
          changed = true;
        }
      }
      return changed ? [updateJourney(baseURL, apiKey, journey.id, { ...journey, graph })] : [];
    });
    await Promise.all([...campaignUpdates, ...journeyUpdates]);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      validate();
      const input: Partial<Experiment> = {
        name: name.trim(),
        description: description.trim() || undefined,
        subject_type: subjectType,
        status,
        method: "frequentist",
        seed,
        holdout_pct: holdoutPct,
        variants: variants.map((variant) => ({
          label: variant.label.trim(),
          weight: Number(variant.weight),
          is_control: variant.is_control,
          template_id: variant.template_id || undefined,
        })),
      };
      const saved = editingID
        ? await updateExperiment(baseURL, apiKey, editingID, input)
        : await createExperiment(baseURL, apiKey, input);
      await syncBinding(saved.id);
      setSuccess(editingID ? "Experiment updated." : "Experiment created.");
      resetForm();
      await load();
    } catch (cause) {
      setError(message(cause, "Unable to save experiment"));
    } finally {
      setSaving(false);
    }
  }

  async function propose(item: Experiment) {
    setOptimizingID(item.id); setError(""); setSuccess("");
    try {
      const proposal = await proposeExperimentOptimization(baseURL, apiKey, item.id);
      setProposals((current) => ({ ...current, [item.id]: proposal }));
      setSuccess(`Proposal ready for ${item.name}. It is advisory until a human approves it.`);
    } catch (cause) { setError(message(cause, "Unable to create optimization proposal")); }
    finally { setOptimizingID(""); }
  }

  async function approve(item: Experiment, proposal: OptimizationProposal) {
    setOptimizingID(item.id); setError(""); setSuccess("");
    try {
      const version = await approveExperimentOptimization(baseURL, apiKey, item.id, proposal.id);
      setProposals((current) => ({ ...current, [item.id]: { ...proposal, status: "approved", approved_at: version.created_at } }));
      setSuccess(`Approved version ${version.version} created. Seed and ${version.holdout_pct}% holdout preserved.`);
    } catch (cause) { setError(message(cause, "Unable to approve optimization")); }
    finally { setOptimizingID(""); }
  }

  return <section className="experiments-layout">
    <Card variant="article" className="experiment-editor">
      <div className="section-title">
        <div><span className="eyebrow">Controlled test</span><h2>{editingID ? "Edit experiment" : "Create experiment"}</h2></div>
        {editingID && <button type="button" className="secondary" onClick={resetForm}>New</button>}
      </div>
      <form onSubmit={submit} className="experiment-form">
        <label>Name<input aria-label="Experiment name" value={name} onChange={(event) => setName(event.target.value)} required /></label>
        <label>Description<input value={description} onChange={(event) => setDescription(event.target.value)} /></label>
        <div className="experiment-inline-fields">
          <label>Subject type<select value={subjectType} onChange={(event) => { setSubjectType(event.target.value as Experiment["subject_type"]); setBindingID(""); setJourneyNodeID(""); }}>
            <option value="campaign">Campaign</option><option value="journey">Journey</option>
          </select></label>
          <label>Holdout %<input type="number" min="0" max="100" value={holdoutPct} onChange={(event) => setHoldoutPct(Number(event.target.value))} /></label>
        </div>
        <div className="experiment-inline-fields">
          <label>Status<select value={status} onChange={(event) => setStatus(event.target.value as Experiment["status"])}>
            <option value="draft">Draft</option><option value="running">Running</option><option value="completed">Completed</option><option value="archived">Archived</option>
          </select></label>
          <label>Assignment seed<input value={seed} onChange={(event) => setSeed(event.target.value)} disabled={status === "running" || status === "completed" || status === "archived"} required /></label>
        </div>

        <fieldset className="variant-editor">
          <legend>Variants</legend>
          <p className="field-help">One control is required. Weights are relative after the {holdoutPct}% holdout.</p>
          {variants.map((variant, index) => <div className="variant-row" key={index}>
            <label>Label<input aria-label={`Variant ${index + 1} label`} value={variant.label} onChange={(event) => updateVariant(index, { label: event.target.value })} /></label>
            <label>Weight<input aria-label={`Variant ${index + 1} weight`} type="number" min="0" value={variant.weight} onChange={(event) => updateVariant(index, { weight: Number(event.target.value) })} /></label>
            <label>Template<select aria-label={`Variant ${index + 1} template`} value={variant.template_id || ""} onChange={(event) => updateVariant(index, { template_id: event.target.value || undefined })}>
              <option value="">Use base template</option>{templates.map((template) => <option value={template.id} key={template.id}>{template.name}</option>)}
            </select></label>
            <label className="checkbox-row"><input aria-label={`Variant ${index + 1} control`} type="radio" name="control-variant" checked={variant.is_control} onChange={() => updateVariant(index, { is_control: true })} />Control</label>
            <button type="button" className="danger" aria-label={`Remove variant ${index + 1}`} disabled={variants.length <= 2} onClick={() => setVariants((current) => current.filter((_, candidate) => candidate !== index))}>Remove</button>
          </div>)}
          <div className="variant-summary"><button type="button" className="secondary" onClick={() => setVariants((current) => [...current, { label: `variant-${current.length + 1}`, weight: 0, is_control: false }])}>Add variant</button><span>Total weight: <strong>{totalWeight}</strong></span></div>
        </fieldset>

        <fieldset className="experiment-binding">
          <legend>Binding</legend>
          <label>{subjectType === "campaign" ? "Bind to campaign" : "Bind to journey"}<select value={bindingID} onChange={(event) => { setBindingID(event.target.value); setJourneyNodeID(""); }}>
            <option value="">No binding</option>
            {(subjectType === "campaign" ? draftCampaigns : draftJourneys).map((subject) => <option value={subject.id} key={subject.id}>{subject.name}</option>)}
          </select></label>
          {subjectType === "journey" && bindingID && <label>Journey node<select value={journeyNodeID} onChange={(event) => setJourneyNodeID(event.target.value)} required>
            <option value="">Select split or message node</option>{bindableNodes.map((node) => <option value={node.id} key={node.id}>{node.id} ({node.type})</option>)}
          </select></label>}
          <p className="field-help">Only editable draft campaigns and journeys are listed. Published versions remain immutable.</p>
        </fieldset>

        <div className="form-actions"><button type="submit" disabled={saving || !name.trim() || !seed.trim()}>{saving ? "Saving…" : editingID ? "Update experiment" : "Create experiment"}</button>{editingID && <button type="button" className="secondary" onClick={resetForm}>Cancel</button>}</div>
      </form>
      {error && <ErrorState description={error} role="alert" />}
      {success && <p className="replay" role="status">{success}</p>}
    </Card>

    <Card variant="article" className="experiment-list">
      <div className="section-title"><div><span className="eyebrow">Workspace</span><h2>Experiments ({items.length})</h2></div></div>
      {loading && <Spinner label="Loading experiments…" />}
      {!loading && items.length === 0 ? <EmptyState title="No experiments yet" description="Create an experiment to test variations and optimize performance" icon="plus" cta={{ label: "New experiment", onClick: resetForm }} /> : <DataTable headers={["Name", "Subject", "Holdout", "Status", "Actions"]} rows={items.map(item => [
        <><strong>{item.name}</strong>{item.description && <small>{item.description}</small>}</>, item.subject_type, `${item.holdout_pct}%`, <span className={`pill ${item.status}`}>{item.status}</span>, <div className="report-row-actions"><button type="button" className="secondary" onClick={() => void edit(item)}>Edit</button><a className="report-link" href={`#reports?type=experiment&id=${encodeURIComponent(item.id)}`}>Report</a></div>,
      ])} />}
      <div className="optimization-panel">
        <div><span className="eyebrow">Governed optimization</span><h3>Proposals review</h3></div>
        <p className="field-help">Generate a report-gated recommendation first. Approval creates a new immutable version; live assignment, seed, and holdout are not changed automatically.</p>
        {items.map((item) => {
          const proposal = proposals[item.id];
          return <div className="optimization-row" key={item.id}>
            <strong>Proposal for {item.name}</strong>
            {!proposal ? <button type="button" className="secondary" disabled={optimizingID === item.id} onClick={() => void propose(item)}>{optimizingID === item.id ? "Evaluating…" : "Propose optimization"}</button> : <>
              <span className={`pill ${proposal.status}`}>{proposal.status}</span>
              <span>{proposal.winner_variant ? `Winner: ${proposal.winner_variant}` : proposal.rationale}</span>
              {proposal.status === "proposed" && <button type="button" className="publish-button" disabled={optimizingID === item.id} onClick={() => void approve(item, proposal)}>Approve new version</button>}
            </>}
          </div>;
        })}
      </div>
    </Card>
  </section>;
}
