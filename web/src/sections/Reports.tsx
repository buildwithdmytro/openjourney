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
import { Card, EmptyState, Field, FunnelChart, Select, Spinner } from "../components";
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
    total: funnel?.[key]?.total ?? 0,
    unique: funnel?.[key]?.unique ?? 0,
  }));
  return <FunnelChart stages={stages} />;
}

export function VariantComparison({ report }: { report: ExperimentReport }) {
  return <Card className="comparison-card">
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
  </Card>;
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

  const typeOptions = [
    { value: "campaign", label: "Campaign" },
    { value: "journey", label: "Journey" },
    { value: "experiment", label: "Experiment" },
  ];

  const subjectOptions = useMemo(() =>
    subjects.map((subject) => ({ value: subject.id, label: subject.name })),
    [subjects]
  );

  return <section className="reports-view" data-theme={theme}>
    <Card className="report-controls">
      <div className="report-control-fields">
        <Field label="Report type">
          <Select
            value={type}
            onChange={(event) => changeType(event.target.value as ReportType)}
            options={typeOptions}
          />
        </Field>
        <Field label="Report subject">
          <Select
            value={selectedID}
            onChange={(event) => setSelectedID(event.target.value)}
            disabled={subjects.length === 0}
            options={subjectOptions}
          />
        </Field>
      </div>
      <button type="button" className="report-theme-toggle" onClick={() => toggleTheme()}>
        Use {theme === "light" ? "dark" : "light"} theme
      </button>
    </Card>

    {loading && <div role="status" className="report-status"><Spinner /></div>}
    {error && <div role="alert" className="report-error"><strong>Error:</strong> {error}</div>}
    {!loading && !error && subjects.length === 0 && (
      <EmptyState
        icon="info"
        title={`No ${type}s available`}
        description={`There are no ${type}s created yet. Create one to generate a report.`}
      />
    )}

    {!loading && funnelReport && <>
      <Card>
        <div className="report-card-heading"><div><span className="eyebrow">Audience progression</span><h2>Performance funnel</h2></div></div>
        <FunnelBars funnel={funnelReport.funnel} />
      </Card>
      <div className="deliverability-grid" aria-label="Deliverability metrics">
        <article className="metric-tile"><span>Bounce rate</span><strong>{percent(funnelReport.deliverability?.bounce_rate ?? 0)}</strong><small>{funnelReport.deliverability?.bounced?.total ?? 0} bounced · {funnelReport.deliverability?.bounced?.unique ?? 0} unique</small></article>
        <article className="metric-tile"><span>Complaint rate</span><strong>{percent(funnelReport.deliverability?.complaint_rate ?? 0)}</strong><small>{funnelReport.deliverability?.complained?.total ?? 0} complained · {funnelReport.deliverability?.complained?.unique ?? 0} unique</small></article>
      </div>
    </>}
    {!loading && experimentReport && <VariantComparison report={experimentReport} />}
  </section>;
}
