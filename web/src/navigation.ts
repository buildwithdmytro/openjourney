export type View =
  | "overview" | "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit"
  | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports"
  | "analytics" | "copilots" | "assistant" | "governance" | "extensions" | "connectors"
  | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging" | "flags"
  | "catalogs" | "prompts";

export const viewTitles: Record<View, [string, string]> = {
  overview: ["Overview", "At a glance view of your workspace activity and resources."],
  profiles: ["Profiles", "Inspect the current customer and consent projection."],
  schemas: ["Event schemas", "Register typed event contracts and compatibility rules."],
  "api-keys": ["API keys", "Create scoped credentials and revoke access."],
  privacy: ["Privacy", "Submit and inspect DSAR export/delete operations."],
  access: ["Access", "Provision local/OIDC users and tenant roles."],
  operations: ["Operations", "Inspect queues, DLQs, and replay determinism."],
  audit: ["Audit", "Review tenant-scoped security and operations activity."],
  segments: ["Segments", "Manage customer segments and membership rules."],
  scoring: ["Scoring", "Publish governed scoring models and inspect profile scores."],
  templates: ["Templates", "Design email templates with Liquid tags and live preview."],
  campaigns: ["Campaigns", "Schedule and manage sharded marketing campaigns linked to segments and templates."],
  journeys: ["Journeys", "Design, publish, and monitor automated customer experiences."],
  experiments: ["Experiments", "Create controlled tests with stable audience assignment."],
  reports: ["Reports", "Compare delivery, conversion, and experiment performance."],
  analytics: ["Analytics", "Explore time-series trends, retention cohorts, audience growth, and spending."],
  copilots: ["AI Copilots", "Create governed drafts for review and human approval."],
  assistant: ["AI Assistant", "Conversational analytics assistant grounded in report data and audited tools."],
  governance: ["AI Governance", "Manage providers, budgets, redaction, and AI activity."],
  extensions: ["Extensions", "Install signed providers, configure grants, and review extension health."],
  connectors: ["Connectors", "Move data through governed sources, sinks, exports, and identity commands."],
  suppressions: ["Suppressions", "Manage bounces, complaints, and manually suppressed endpoints."],
  "sender-identities": ["Sender Identities", "Manage verified sender emails, SMS, and push channels."],
  "device-tokens": ["Device Tokens", "Inspect and retire push device tokens per profile."],
  acquisition: ["Acquisition", "Build defended forms and immutable landing pages."],
  messaging: ["Messaging", "Create and manage in-app messages, content cards, and web push campaigns."],
  flags: ["Feature Flags", "Create, publish, and toggle environment-scoped feature flags with targeting and exposure analytics."],
  catalogs: ["Catalogs", "Manage reference data catalogs and governed connected content sources."],
  prompts: ["Prompts", "Author, version, evaluate, and publish governed prompt templates."],
};

export interface NavGroup {
  label: string;
  items: View[];
}

export const navGroups: NavGroup[] = [
  { label: "", items: ["overview"] },
  { label: "Audiences", items: ["profiles", "segments", "scoring", "acquisition"] },
  { label: "Messaging", items: ["templates", "campaigns", "journeys", "experiments", "flags", "messaging"] },
  { label: "Delivery", items: ["suppressions", "sender-identities", "device-tokens"] },
  { label: "AI & Insights", items: ["copilots", "assistant", "governance", "prompts", "reports", "analytics"] },
  { label: "Data", items: ["schemas", "catalogs"] },
  { label: "Infrastructure", items: ["connectors", "extensions"] },
  { label: "Admin", items: ["privacy", "access", "audit", "operations"] },
  { label: "Settings", items: ["api-keys"] },
];
