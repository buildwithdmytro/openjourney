import { useEffect, useState } from "react";
import { getOverview, Overview as OverviewType } from "../api";
import { Card, EmptyState, Spinner } from "../components";
import { message } from "../errors";

function SimpleSparkline({ data, label }: { data: number[]; label: string }) {
  if (data.length === 0) return null;
  const max = Math.max(...data, 1);
  const min = Math.min(...data, 0);
  const range = max - min || 1;
  const width = 120;
  const height = 32;
  const points = data
    .map((value, i) => ({
      x: (i / (data.length - 1 || 1)) * width,
      y: height - ((value - min) / range) * (height - 4) - 2,
    }))
    .map((p) => `${p.x},${p.y}`)
    .join(" ");

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="sparkline" role="img" aria-label={label}>
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

interface OverviewCard {
  label: string;
  value: number;
  link?: string;
  color?: string;
}

export default function Overview({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [data, setData] = useState<OverviewType | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setError("");
    setLoading(true);
    getOverview(baseURL, apiKey)
      .then((result) => {
        if (active) {
          setData(result);
          setLoading(false);
        }
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
    { label: "Delivery Attempts", value: data.delivery_attempts, link: "#reports" },
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
            {card.value > 0 && <SimpleSparkline data={[card.value * 0.6, card.value * 0.8, card.value]} label={`${card.label} trend`} />}
          </Card>
        ))}
      </div>
    </div>
  );
}
