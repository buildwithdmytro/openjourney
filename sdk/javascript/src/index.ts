export type Classification = "public" | "internal" | "confidential" | "restricted";

export type OpenJourneyEvent = {
  event_type: string;
  schema_version: number;
  external_id?: string;
  anonymous_id?: string;
  idempotency_key: string;
  occurred_at: string;
  source: "web-sdk";
  source_event_id: string;
  correlation_id?: string;
  data_classification: Classification;
  consent_context?: Record<string, unknown>;
  payload: Record<string, unknown>;
};

export type InAppMessage = {
  id: string;
  message_type: "modal" | "banner" | "fullscreen" | "card";
  content: Record<string, unknown>;
  status: string;
  rank: number;
  categories: string[];
  start_at: string;
  expires_at?: string | null;
  displayed_at?: string | null;
  clicked_at?: string | null;
  dismissed_at?: string | null;
};

export type FlagValue = boolean | string | number | Record<string, unknown>;

export type FlagEvaluation = {
  variant: string;
  value: FlagValue;
};

export type FlagsResponse = {
  flags: Record<string, FlagEvaluation>;
};

export type ClientOptions = {
  endpoint: string;
  apiKey: string;
  tenant: string;
  app: string;
  batchSize?: number;
  flushIntervalMs?: number;
  storage?: Storage;
  fetch?: typeof globalThis.fetch;
  now?: () => Date;
  randomUUID?: () => string;
};

const QUEUE_KEY = "openjourney:event-queue:v1";
const ANONYMOUS_KEY = "openjourney:anonymous-id:v1";
const FLAGS_KEY = "openjourney:flags:v1";

export class OpenJourney {
  private readonly endpoint: string;
  private readonly apiKey: string;
  private readonly tenant: string;
  private readonly app: string;
  private readonly batchSize: number;
  private readonly storage?: Storage;
  private readonly request: typeof globalThis.fetch;
  private readonly now: () => Date;
  private readonly uuid: () => string;
  private externalID?: string;
  private anonymousID: string;
  private queue: OpenJourneyEvent[];
  private timer?: ReturnType<typeof setInterval>;
  private flushing?: Promise<void>;
  private flags: Record<string, FlagEvaluation>;
  private currentEnvironment: string;

  constructor(options: ClientOptions) {
    if (!options.endpoint || !options.apiKey || !options.tenant || !options.app) {
      throw new Error("endpoint, apiKey, tenant, and app are required");
    }
    this.endpoint = options.endpoint.replace(/\/$/, "");
    this.apiKey = options.apiKey;
    this.tenant = options.tenant;
    this.app = options.app;
    this.batchSize = Math.max(1, Math.min(options.batchSize ?? 25, 75));
    this.storage = options.storage ?? globalThis.localStorage;
    this.request = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.now = options.now ?? (() => new Date());
    this.uuid = options.randomUUID ?? (() => globalThis.crypto.randomUUID());
    this.anonymousID = this.storage?.getItem(ANONYMOUS_KEY) || this.uuid();
    this.storage?.setItem(ANONYMOUS_KEY, this.anonymousID);
    this.queue = this.loadQueue();
    this.flags = this.loadFlags();
    this.currentEnvironment = "production";
    const interval = options.flushIntervalMs ?? 10_000;
    if (interval > 0) {
      this.timer = setInterval(() => void this.flush(), interval);
    }
  }

  identify(externalID: string, attributes?: Record<string, unknown>): string | undefined {
    if (!externalID.trim()) throw new Error("externalID is required");
    this.externalID = externalID;
    if (attributes && Object.keys(attributes).length > 0) {
      return this.setAttributes(attributes);
    }
    return undefined;
  }

  reset(): void {
    this.externalID = undefined;
    this.anonymousID = this.uuid();
    this.storage?.setItem(ANONYMOUS_KEY, this.anonymousID);
  }

  getAnonymousId(): string {
    return this.anonymousID;
  }

  getExternalId(): string | undefined {
    return this.externalID;
  }

  startSession(properties: Record<string, unknown> = {}): string {
    return this.track("session.started", properties);
  }

  track(
    eventType: string,
    payload: Record<string, unknown> = {},
    options: { schemaVersion?: number; classification?: Classification; consentContext?: Record<string, unknown> } = {},
  ): string {
    if (!eventType.trim()) throw new Error("eventType is required");
    const id = this.uuid();
    const event: OpenJourneyEvent = {
      event_type: eventType,
      schema_version: options.schemaVersion ?? 1,
      idempotency_key: id,
      source_event_id: id,
      occurred_at: this.now().toISOString(),
      source: "web-sdk",
      data_classification: options.classification ?? "internal",
      payload,
      anonymous_id: this.anonymousID,
    };
    if (this.externalID) event.external_id = this.externalID;
    if (options.consentContext) event.consent_context = options.consentContext;
    this.enqueue(event);
    return id;
  }

  setAttributes(attributes: Record<string, unknown>): string {
    return this.track("profile.updated", { attributes }, { classification: "confidential" });
  }

  alias(namespace: string, value: string): string {
    if (!namespace.trim() || !value.trim()) throw new Error("namespace and value are required");
    return this.track("identity.alias", { namespace, value }, { classification: "confidential" });
  }

  merge(sourceExternalID: string): string {
    if (!this.externalID) throw new Error("identify target externalID before merge");
    if (!sourceExternalID.trim()) throw new Error("sourceExternalID is required");
    return this.track(
      "identity.merge",
      { source_external_id: sourceExternalID },
      { classification: "confidential" },
    );
  }

  setConsent(
    channel: string,
    state: "subscribed" | "unsubscribed",
    options: { topic?: string; evidence?: Record<string, unknown> } = {},
  ): string {
    return this.track(
      "consent.changed",
      { channel, state, topic: options.topic ?? "marketing", evidence: options.evidence ?? {} },
      { classification: "restricted", consentContext: options.evidence },
    );
  }

  async fetchInbox(token?: string): Promise<InAppMessage[]> {
    if (this.externalID && !token) {
      throw new Error(
        "identified user requires a token from the server; pass SignInAppToken to fetchInbox(token)",
      );
    }

    const params = new URLSearchParams({
      tenant: this.tenant,
      app: this.app,
    });

    if (token) {
      params.set("token", token);
      if (this.externalID) {
        params.set("external_id", this.externalID);
      }
    } else {
      params.set("anonymous_id", this.anonymousID);
    }

    const response = await this.request(`${this.endpoint}/v1/messages/inbox?${params}`, {
      method: "GET",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
    });

    if (!response.ok) {
      if (response.status === 401) {
        throw new Error("Unauthorized: invalid or expired token");
      }
      if (response.status === 403) {
        throw new Error("Forbidden: access to inbox denied");
      }
      throw new Error(`fetchInbox failed (${response.status})`);
    }

    const data = await response.json();
    return (data.messages ?? []) as InAppMessage[];
  }

  async fetchFlags(token?: string, environment: string = "production"): Promise<Record<string, FlagEvaluation>> {
    if (this.externalID && !token) {
      throw new Error(
        "identified user requires a token from the server; pass SignInAppToken to fetchFlags(token)",
      );
    }

    const params = new URLSearchParams({
      tenant: this.tenant,
      app: this.app,
      environment,
    });

    if (token) {
      params.set("token", token);
      if (this.externalID) {
        params.set("external_id", this.externalID);
      }
    } else {
      params.set("anonymous_id", this.anonymousID);
    }

    try {
      const response = await this.request(`${this.endpoint}/v1/flags/evaluate?${params}`, {
        method: "GET",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        if (response.status === 401) {
          throw new Error("Unauthorized: invalid or expired token");
        }
        if (response.status === 403) {
          throw new Error("Forbidden: access to flags denied");
        }
        throw new Error(`fetchFlags failed (${response.status})`);
      }

      const data = (await response.json()) as FlagsResponse;
      this.flags = data.flags ?? {};
      this.currentEnvironment = environment;
      this.persistFlags();
      return this.flags;
    } catch {
      return this.flags;
    }
  }

  getFlag(key: string, defaultValue?: FlagValue): FlagValue | undefined {
    const flag = this.flags[key];
    if (flag) {
      this.track("feature_flag.exposure", {
        flag_key: key,
        variant: flag.variant,
        value: flag.value,
      });
      return flag.value;
    }
    if (defaultValue !== undefined) {
      this.track("feature_flag.exposure", {
        flag_key: key,
        variant: "default",
        value: defaultValue,
      });
      return defaultValue;
    }
    return undefined;
  }

  getVariant(key: string): string | undefined {
    const flag = this.flags[key];
    if (flag) {
      this.track("feature_flag.exposure", {
        flag_key: key,
        variant: flag.variant,
        value: flag.value,
      });
      return flag.variant;
    }
    return undefined;
  }

  async reportImpression(messageId: string, token?: string): Promise<void> {
    await this.reportEngagement(messageId, "impression", token);
  }

  async reportClick(messageId: string, token?: string): Promise<void> {
    await this.reportEngagement(messageId, "click", token);
  }

  async reportDismiss(messageId: string, token?: string): Promise<void> {
    await this.reportEngagement(messageId, "dismiss", token);
  }

  private async reportEngagement(
    messageId: string,
    action: "impression" | "click" | "dismiss",
    token?: string,
  ): Promise<void> {
    if (!messageId.trim()) {
      throw new Error("messageId is required");
    }

    if (this.externalID && !token) {
      throw new Error(
        `identified user requires a token from the server for engagement report; pass SignInAppToken to report${
          action.charAt(0).toUpperCase() + action.slice(1)
        }(messageId, token)`,
      );
    }

    const params = new URLSearchParams({
      tenant: this.tenant,
      app: this.app,
    });

    if (token) {
      params.set("token", token);
      if (this.externalID) {
        params.set("external_id", this.externalID);
      }
    } else {
      params.set("anonymous_id", this.anonymousID);
    }

    const response = await this.request(
      `${this.endpoint}/v1/messages/${encodeURIComponent(messageId)}/${action}?${params}`,
      {
        method: "POST",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({}),
      },
    );

    if (!response.ok) {
      if (response.status === 401) {
        throw new Error("Unauthorized: invalid or expired token");
      }
      if (response.status === 403) {
        throw new Error("Forbidden: access denied");
      }
      if (response.status === 404) {
        throw new Error(`message not found (${messageId})`);
      }
      throw new Error(`report engagement failed (${response.status})`);
    }
  }

  async flush(): Promise<void> {
    if (this.flushing) return this.flushing;
    this.flushing = this.doFlush().finally(() => {
      this.flushing = undefined;
    });
    return this.flushing;
  }

  destroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  private enqueue(event: OpenJourneyEvent): void {
    this.queue.push(event);
    this.persist();
    if (this.queue.length >= this.batchSize) void this.flush();
  }

  private async doFlush(): Promise<void> {
    while (this.queue.length > 0) {
      const batch = this.queue.slice(0, this.batchSize);
      const response = await this.request(`${this.endpoint}/v1/events/batch`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ events: batch }),
        keepalive: true,
      });
      if (!response.ok) {
        throw new Error(`OpenJourney ingestion failed (${response.status})`);
      }
      this.queue.splice(0, batch.length);
      this.persist();
    }
  }

  private loadQueue(): OpenJourneyEvent[] {
    try {
      const value = this.storage?.getItem(QUEUE_KEY);
      return value ? (JSON.parse(value) as OpenJourneyEvent[]) : [];
    } catch {
      return [];
    }
  }

  private persist(): void {
    this.storage?.setItem(QUEUE_KEY, JSON.stringify(this.queue));
  }

  private loadFlags(): Record<string, FlagEvaluation> {
    try {
      const value = this.storage?.getItem(FLAGS_KEY);
      return value ? (JSON.parse(value) as Record<string, FlagEvaluation>) : {};
    } catch {
      return {};
    }
  }

  private persistFlags(): void {
    this.storage?.setItem(FLAGS_KEY, JSON.stringify(this.flags));
  }
}
