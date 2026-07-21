import { useEffect, useMemo, useState } from "react";
import {
  Campaign,
  Experiment,
  ExperimentReport,
  getCampaignReport,
  getExperimentReport,
  getJourneyReport,
  Journey,
  listCampaigns,
  listExperiments,
  listJourneys,
  ReportDeliverability,
  ReportFunnel,
} from "../api";
import { FunnelChart } from "../components";
import { useTheme } from "../useTheme";

type ReportType = "campaign" | "journey" | "experiment";
type FunnelReport = { funnel: ReportFunnel; deliverability: ReportDeliverability };
type Theme = "light" | "dark";

const funnelStages: Array<{ key: keyof ReportFunnel; label: string }> = [
  { key: "targeted", label: "Targeted" },
  { key: "sent", label: "Sent" },
  { key: "delivered", label: "Delivered" },
  { key: "opened", label: "Opened" },
  { key: "clicked", label: "Clicked" },
  { key: "converted", label: "Converted" },
];

function initialSelection(): { type: ReportType; id: string } {
  const query = window.location.hash.includes("?") ? window.location.hash.split("?")[1] : "";
  const params = new URLSearchParams(query);
  const type = params.get("type");
  return {
    type: type === "journey" || type === "experiment" ? type : "campaign",
    id: params.get("id") || "",
  };
}

function percent(value: number) {
  return `${(value * 100).toFixed(1)}%`;
}

export function FunnelBars({ funnel }: { funnel: ReportFunnel }) {
  const stages = funnelStages.map(({ key, label }) => ({
    label,
    total: funnel[key].total,
    unique: funnel[key].unique,
  }));
  return <FunnelChart stages={stages} />;
}

export function VariantComparison({ report }: { report: ExperimentReport }) {
  return <article className="report-card comparison-card">
    <div className="report-card-heading">
      <div><span className="eyebrow">Experiment results</span><h2>Variant comparison</h2></div>
      {report.winner_variant && <span className="winner-pill">Advisory winner: {report.winner_variant}</span>}
    </div>
    <div className="report-table-wrap"><table className="variant-comparison"><thead><tr>
      <th>Variant</th><th>Sent</th><th>Conversions</th><th>Rate</th><th>Uplift</th><th>p-value</th><th>95% CI</th><th>Result</th>
    </tr></thead><tbody>{report.variants.map((variant) => {
      const significant = !variant.is_control && variant.p_value < 0.05;
      return <tr key={variant.label}>
        <td><strong>{variant.label}</strong>{variant.is_control && <small>Control</small>}</td>
        <td>{variant.sent}</td><td>{variant.conversions}</td><td>{percent(variant.rate)}</td>
        <td>{variant.is_control ? "—" : percent(variant.uplift)}</td>
        <td>{variant.is_control ? "—" : variant.p_value.toFixed(4)}</td>
        <td>{variant.is_control ? "—" : `${percent(variant.ci_low)} to ${percent(variant.ci_high)}`}</td>
        <td><span className={`result-pill ${significant ? "significant" : "pending"}`}>
          {variant.is_control ? "Baseline" : significant ? "Significant" : "Not yet significant"}
        </span></td>
      </tr>;
    })}</tbody></table></div>
    <p className="report-note">The winner is advisory. Rollout remains a separate, approved action.</p>
  </article>;
}

export default function Reports({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const initial = useMemo(initialSelection, []);
  const [type, setType] = useState<ReportType>(initial.type);
  const [selectedID, setSelectedID] = useState(initial.id);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [experiments, setExperiments] = useState<Experiment[]>([]);
  const [funnelReport, setFunnelReport] = useState<FunnelReport | null>(null);
  const [experimentReport, setExperimentReport] = useState<ExperimentReport | null>(null);
  const { theme, toggle: toggleTheme } = useTheme();
  const [subjectsLoaded, setSubjectsLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setError("");
    Promise.all([listCampaigns(baseURL, apiKey), listJourneys(baseURL, apiKey), listExperiments(baseURL, apiKey)])
      .then(([campaignItems, journeyItems, experimentItems]) => {
        if (!active) return;
        setCampaigns(campaignItems);
        setJourneys(journeyItems);
        setExperiments(experimentItems);
        setSubjectsLoaded(true);
      })
      .catch((cause: unknown) => {
        if (active) {
          setSubjectsLoaded(true);
          setLoading(false);
          setError(cause instanceof Error ? cause.message : "Could not load report subjects.");
        }
      });
    return () => { active = false; };
  }, [apiKey, baseURL]);

  const subjects = useMemo(() => type === "campaign" ? campaigns : type === "journey" ? journeys : experiments, [campaigns, experiments, journeys, type]);

  useEffect(() => {
    if (!subjectsLoaded) return;
    if (subjects.length === 0) {
      setSelectedID("");
      setLoading(false);
      return;
    }
    if (!subjects.some((subject) => subject.id === selectedID)) setSelectedID(subjects[0].id);
  }, [selectedID, subjects, subjectsLoaded]);

  useEffect(() => {
    if (!selectedID) return;
    let active = true;
    setLoading(true);
    setError("");
    setFunnelReport(null);
    setExperimentReport(null);
    const request = type === "campaign"
      ? getCampaignReport(baseURL, apiKey, selectedID)
      : type === "journey"
        ? getJourneyReport(baseURL, apiKey, selectedID)
        : getExperimentReport(baseURL, apiKey, selectedID);
    request.then((report) => {
      if (!active) return;
      if (type === "experiment") setExperimentReport(report as ExperimentReport);
      else setFunnelReport(report as FunnelReport);
    }).catch((cause: unknown) => {
      if (active) setError(cause instanceof Error ? cause.message : "Could not load this report.");
    }).finally(() => active && setLoading(false));
    window.history.replaceState(null, "", `#reports?type=${type}&id=${encodeURIComponent(selectedID)}`);
    return () => { active = false; };
  }, [apiKey, baseURL, selectedID, type]);

  function changeType(nextType: ReportType) {
    setType(nextType);
    setSelectedID("");
  }

  return <section className="reports-view" data-theme={theme}>
    <article className="report-controls">
      <div className="report-selectors">
        <label>Report type<select value={type} onChange={(event) => changeType(event.target.value as ReportType)}>
          <option value="campaign">Campaign</option><option value="journey">Journey</option><option value="experiment">Experiment</option>
        </select></label>
        <label>Report subject<select value={selectedID} onChange={(event) => setSelectedID(event.target.value)} disabled={subjects.length === 0}>
          {subjects.length === 0 && <option value="">No subjects available</option>}
          {subjects.map((subject) => <option value={subject.id} key={subject.id}>{subject.name}</option>)}
        </select></label>
      </div>
      <button type="button" className="report-theme-toggle" onClick={() => toggleTheme()}>
        Use {theme === "light" ? "dark" : "light"} theme
      </button>
    </article>

    {loading && <p role="status" className="report-status">Loading report…</p>}
    {error && <p role="alert" className="report-error">{error}</p>}
    {!loading && !error && subjects.length === 0 && <article className="report-card"><p>No {type}s are available for reporting yet.</p></article>}

    {!loading && funnelReport && <>
      <article className="report-card">
        <div className="report-card-heading"><div><span className="eyebrow">Audience progression</span><h2>Performance funnel</h2></div></div>
        <FunnelBars funnel={funnelReport.funnel} />
      </article>
      <div className="deliverability-grid" aria-label="Deliverability metrics">
        <article className="metric-tile"><span>Bounce rate</span><strong>{percent(funnelReport.deliverability.bounce_rate)}</strong><small>{funnelReport.deliverability.bounced.total} bounced · {funnelReport.deliverability.bounced.unique} unique</small></article>
        <article className="metric-tile"><span>Complaint rate</span><strong>{percent(funnelReport.deliverability.complaint_rate)}</strong><small>{funnelReport.deliverability.complained.total} complained · {funnelReport.deliverability.complained.unique} unique</small></article>
      </div>
    </>}
    {!loading && experimentReport && <VariantComparison report={experimentReport} />}
  </section>;
}
