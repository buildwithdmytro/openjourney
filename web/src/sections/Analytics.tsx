import { useEffect, useState, useMemo } from "react";
import {
  Campaign,
  Journey,
  listCampaigns,
  listJourneys,
  getCampaignFunnelOverTimeReport,
  getJourneyFunnelOverTimeReport,
  getCampaignRetentionReport,
  getJourneyRetentionReport,
  getCampaignGrowthReport,
  getJourneyGrowthReport,
  getCampaignCostReport,
  getJourneyCostReport,
  FunnelOverTimeReport,
  RetentionReport,
  GrowthReport,
  CostReport,
  SavedReport,
  listSavedReports,
  createSavedReport,
  deleteSavedReport,
} from "../api";
import { Card, ConfirmDialog, EmptyState, ErrorState, Field, LineChart, BarChart, Spinner, Select } from "../components";
import { message } from "../errors";

type ReportType = "campaign" | "journey";

interface TimeSeries {
  label: string;
  data: number[];
  color?: string;
}

export default function Analytics({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [type, setType] = useState<ReportType>("campaign");
  const [selectedID, setSelectedID] = useState("");
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [funnelReport, setFunnelReport] = useState<FunnelOverTimeReport | null>(null);
  const [retentionReport, setRetentionReport] = useState<RetentionReport | null>(null);
  const [growthReport, setGrowthReport] = useState<GrowthReport | null>(null);
  const [costReport, setCostReport] = useState<CostReport | null>(null);
  const [savedReports, setSavedReports] = useState<SavedReport[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [subjectsLoaded, setSubjectsLoaded] = useState(false);
  const [confirmDeleteReport, setConfirmDeleteReport] = useState<string | null>(null);

  const subjects = useMemo(
    () => (type === "campaign" ? campaigns : journeys),
    [campaigns, journeys, type]
  );

  useEffect(() => {
    let active = true;
    setError("");
    Promise.all([listCampaigns(baseURL, apiKey), listJourneys(baseURL, apiKey), listSavedReports(baseURL, apiKey)])
      .then(([campaignItems, journeyItems, reports]) => {
        if (!active) return;
        setCampaigns(campaignItems);
        setJourneys(journeyItems);
        setSavedReports(reports);
        setSubjectsLoaded(true);
      })
      .catch((cause: unknown) => {
        if (active) {
          setSubjectsLoaded(true);
          setLoading(false);
          setError(message(cause));
        }
      });
    return () => { active = false; };
  }, [apiKey, baseURL]);

  useEffect(() => {
    if (!subjectsLoaded) return;
    if (subjects.length === 0) {
      setSelectedID("");
      setLoading(false);
      return;
    }
    if (!subjects.some((subject) => subject.id === selectedID)) {
      setSelectedID(subjects[0].id);
    }
  }, [subjects, subjectsLoaded, selectedID]);

  useEffect(() => {
    if (!selectedID) return;
    let active = true;
    setLoading(true);
    setError("");
    setFunnelReport(null);
    setRetentionReport(null);
    setGrowthReport(null);
    setCostReport(null);

    const query = { granularity: "day", start: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(), end: new Date().toISOString() };

    Promise.all([
      type === "campaign"
        ? getCampaignFunnelOverTimeReport(baseURL, apiKey, selectedID, query)
        : getJourneyFunnelOverTimeReport(baseURL, apiKey, selectedID, query),
      type === "campaign"
        ? getCampaignRetentionReport(baseURL, apiKey, selectedID, query)
        : getJourneyRetentionReport(baseURL, apiKey, selectedID, query),
      type === "campaign"
        ? getCampaignGrowthReport(baseURL, apiKey, selectedID, query)
        : getJourneyGrowthReport(baseURL, apiKey, selectedID, query),
      type === "campaign"
        ? getCampaignCostReport(baseURL, apiKey, selectedID, query)
        : getJourneyCostReport(baseURL, apiKey, selectedID, query),
    ])
      .then(([funnel, retention, growth, cost]) => {
        if (!active) return;
        setFunnelReport(funnel);
        setRetentionReport(retention);
        setGrowthReport(growth);
        setCostReport(cost);
        setLoading(false);
      })
      .catch((cause: unknown) => {
        if (active) {
          setError(message(cause));
          setLoading(false);
        }
      });

    return () => { active = false; };
  }, [apiKey, baseURL, selectedID, type]);

  function changeType(nextType: ReportType) {
    setType(nextType);
    setSelectedID("");
  }

  const typeOptions = [
    { value: "campaign", label: "Campaign" },
    { value: "journey", label: "Journey" },
  ];

  const subjectOptions = useMemo(
    () => subjects.map((subject) => ({ value: subject.id, label: subject.name })),
    [subjects]
  );

  const funnelSeries = useMemo(() => {
    if (!funnelReport?.buckets) return [];
    return [
      { label: "Sent", data: funnelReport.buckets.map(b => b.funnel?.sent?.total ?? 0) },
      { label: "Delivered", data: funnelReport.buckets.map(b => b.funnel?.delivered?.total ?? 0) },
      { label: "Opened", data: funnelReport.buckets.map(b => b.funnel?.opened?.total ?? 0) },
      { label: "Clicked", data: funnelReport.buckets.map(b => b.funnel?.clicked?.total ?? 0) },
      { label: "Converted", data: funnelReport.buckets.map(b => b.funnel?.converted?.total ?? 0) },
    ];
  }, [funnelReport]);

  const funnelLabels = useMemo(() => {
    if (!funnelReport?.buckets) return [];
    return funnelReport.buckets.map(b => new Date(b.time).toLocaleDateString());
  }, [funnelReport]);

  const growthSeries = useMemo(() => {
    if (!growthReport?.buckets) return [];
    return [
      { label: "New Profiles", data: growthReport.buckets.map(b => b.new_profiles) },
      { label: "Net Growth", data: growthReport.buckets.map(b => b.net_growth) },
      { label: "Segment Memberships", data: growthReport.buckets.map(b => b.segment_memberships) },
    ];
  }, [growthReport]);

  const growthLabels = useMemo(() => {
    if (!growthReport?.buckets) return [];
    return growthReport.buckets.map(b => new Date(b.time).toLocaleDateString());
  }, [growthReport]);

  const costSeries = useMemo(() => {
    if (!costReport?.buckets) return [];
    return [
      { label: "Send Count", data: costReport.buckets.map(b => b.send_count) },
    ];
  }, [costReport]);

  const costLabels = useMemo(() => {
    if (!costReport?.buckets) return [];
    return costReport.buckets.map(b => new Date(b.time).toLocaleDateString());
  }, [costReport]);

  async function handleSaveReport() {
    if (!selectedID) return;
    const name = prompt("Enter report name:");
    if (!name) return;
    try {
      await createSavedReport(baseURL, apiKey, {
        workspace_id: campaigns[0]?.workspace_id || journeys[0]?.workspace_id || "",
        name,
        report_type: "funnel",
        query: { granularity: "day" },
      });
      const reports = await listSavedReports(baseURL, apiKey);
      setSavedReports(reports);
    } catch (cause) {
      setError(message(cause));
    }
  }

  async function handleDeleteReport(reportID: string) {
    try {
      await deleteSavedReport(baseURL, apiKey, reportID);
      const reports = await listSavedReports(baseURL, apiKey);
      setSavedReports(reports);
    } catch (cause) {
      setError(message(cause));
    }
  }

  if (loading && !funnelReport) {
    return <div role="status"><Spinner /></div>;
  }

  if (error && !funnelReport) {
    return <ErrorState title="Could not load analytics" description={error} role="alert" />;
  }

  if (!subjectsLoaded || subjects.length === 0) {
    return (
      <EmptyState
        icon="info"
        title={`No ${type}s available`}
        description={`There are no ${type}s created yet. Create one to view analytics.`}
        cta={{ label: `Create ${type}`, onClick: () => (window.location.hash = type === "campaign" ? "#campaigns" : "#journeys") }}
      />
    );
  }

  return (
    <div>
      <ConfirmDialog isOpen={confirmDeleteReport !== null} onClose={() => setConfirmDeleteReport(null)} onConfirm={() => handleDeleteReport(confirmDeleteReport!)} title="Delete this saved report?" message="This saved report will be permanently deleted." confirmText="Delete" isDangerous={true} />
      <h1>Analytics</h1>
      <p className="page-description">View real-time analytics across your campaigns and journeys.</p>

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
        <button type="button" onClick={() => void handleSaveReport()}>
          Save Report
        </button>
      </Card>

      {error && <div role="alert" className="report-error"><strong>Error:</strong> {error}</div>}

      {funnelReport && funnelLabels.length > 0 && (
        <Card>
          <div className="report-card-heading">
            <div><span className="eyebrow">Performance</span><h2>Funnel over time</h2></div>
          </div>
          <LineChart series={funnelSeries} xLabels={funnelLabels} />
        </Card>
      )}

      {growthReport && growthLabels.length > 0 && (
        <Card>
          <div className="report-card-heading">
            <div><span className="eyebrow">Audience</span><h2>Growth trends</h2></div>
          </div>
          <BarChart series={growthSeries} xLabels={growthLabels} />
        </Card>
      )}

      {costReport && costLabels.length > 0 && (
        <Card>
          <div className="report-card-heading">
            <div><span className="eyebrow">Spending</span><h2>Cost per period</h2></div>
          </div>
          <BarChart series={costSeries} xLabels={costLabels} />
        </Card>
      )}

      {retentionReport && retentionReport.cohorts.length > 0 && (
        <Card>
          <div className="report-card-heading">
            <div><span className="eyebrow">Engagement</span><h2>Retention by cohort</h2></div>
          </div>
          <div className="retention-matrix">
            <table>
              <thead>
                <tr>
                  <th>Cohort</th>
                  {Array.from({ length: Math.max(...retentionReport.cohorts.map(c => c.sizes.length)) }).map((_, i) => (
                    <th key={i}>+{i}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {retentionReport.cohorts.map((cohort) => (
                  <tr key={cohort.cohort_time}>
                    <td>{new Date(cohort.cohort_time).toLocaleDateString()}</td>
                    {cohort.sizes.map((size, i) => (
                      <td key={i}>{size}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {savedReports.length > 0 && (
        <Card>
          <div className="report-card-heading">
            <div><span className="eyebrow">Saved</span><h2>Saved reports</h2></div>
          </div>
          <div className="saved-reports-list">
            {savedReports.map((report) => (
              <div key={report.id} className="saved-report-item">
                <span>{report.name}</span>
                <button type="button" onClick={() => setConfirmDeleteReport(report.id)}>
                  Delete
                </button>
              </div>
            ))}
          </div>
        </Card>
      )}
    </div>
  );
}
