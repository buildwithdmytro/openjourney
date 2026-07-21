import { useEffect, useState } from "react";
import { getOverview, Overview as OverviewType, listCampaigns, getCampaignFunnelOverTimeReport } from "../api";
import { Card, EmptyState, Spinner, Sparkline } from "../components";
import { message } from "../errors";

interface OverviewCard {
  label: string;
  value: number;
  link?: string;
  color?: string;
  sparklineData?: number[];
}

export default function Overview({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [data, setData] = useState<OverviewType | null>(null);
  const [sparklineMap, setSparklineMap] = useState<Record<string, number[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setError("");
    setLoading(true);

    Promise.all([getOverview(baseURL, apiKey), listCampaigns(baseURL, apiKey)])
      .then(async ([overview, campaigns]) => {
        if (!active) return;
        setData(overview);

        if (campaigns.length > 0) {
          const query = { granularity: "day", start: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(), end: new Date().toISOString() };
          try {
            const report = await getCampaignFunnelOverTimeReport(baseURL, apiKey, campaigns[0].id, query);
            if (active) {
              const delivered = report.buckets.map(b => b.funnel.delivered.total);
              const opened = report.buckets.map(b => b.funnel.opened.total);
              const clicked = report.buckets.map(b => b.funnel.clicked.total);
              setSparklineMap({ delivered, opened, clicked });
            }
          } catch (cause) {
            if (active) {
              console.error("Could not load sparkline data:", cause);
            }
          }
        }
        if (active) setLoading(false);
      })
      .catch((cause: unknown) => {
        if (active) {
          setLoading(false);
          setError(message(cause));
        }
      });
    return () => { active = false; };
  }, [apiKey, baseURL]);

  if (loading) {
    return <div role="status"><Spinner /></div>;
  }

  if (error) {
    return <section className="card ui-crash" role="alert"><h2>Could not load overview</h2><p>{error}</p></section>;
  }

  if (!data) {
    return <EmptyState icon="info" title="No data" description="Overview data could not be loaded." />;
  }

  const isEmpty = Object.values(data).every((v) => v === 0);

  if (isEmpty) {
    return (
      <EmptyState
        icon="info"
        title="Welcome to OpenJourney"
        description="Your workspace is ready. Start by creating profiles, building segments, or designing campaigns."
        cta={{ label: "Explore Profiles", onClick: () => (window.location.hash = "#profiles") }}
      />
    );
  }

  const cards: OverviewCard[] = [
    { label: "Profiles", value: data.profiles, link: "#profiles", color: "accent" },
    { label: "Journeys", value: data.journeys, link: "#journeys" },
    { label: "Campaigns", value: data.campaigns, link: "#campaigns" },
    { label: "Delivery Attempts", value: data.delivery_attempts, link: "#reports", sparklineData: sparklineMap.delivered },
    { label: "In-App Messages", value: data.inapp_messages, link: "#messaging" },
    { label: "Connector Runs", value: data.connector_runs, link: "#connectors" },
  ];

  return (
    <div>
      <h1>Overview</h1>
      <p className="page-description">At a glance view of your workspace activity and resources.</p>
      <div className="overview-grid">
        {cards.map((card) => (
          <Card key={card.label} className="overview-card">
            <div className="card-header">
              <h3>{card.label}</h3>
              {card.link && (
                <a href={card.link} className="card-link" aria-label={`Go to ${card.label}`}>
                  →
                </a>
              )}
            </div>
            <div className="card-value">{card.value.toLocaleString()}</div>
            {card.value > 0 && card.sparklineData && card.sparklineData.length > 0 && <Sparkline data={card.sparklineData} label={`${card.label} trend`} />}
          </Card>
        ))}
      </div>
    </div>
  );
}
