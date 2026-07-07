import http from "k6/http";
import { check } from "k6";

export const options = {
  scenarios: {
    sustained_ingestion: {
      executor: "constant-arrival-rate",
      rate: Number(__ENV.RATE || 20),
      timeUnit: "1s",
      duration: __ENV.DURATION || "30s",
      preAllocatedVUs: Number(__ENV.VUS || 50),
      maxVUs: Number(__ENV.MAX_VUS || 200),
    },
  },
  thresholds: {
    http_req_failed: [`rate<${__ENV.MAX_FAILURE_RATE || "0.001"}`],
    http_req_duration: [`p(99)<${__ENV.P99_MS || "500"}`],
    checks: ["rate>0.999"],
  },
};

const endpoint = __ENV.ENDPOINT || "http://localhost:8080";
const apiKey = __ENV.API_KEY || "local-development-key";

export default function () {
  const events = [];
  const batchSize = Number(__ENV.BATCH_SIZE || 10);
  for (let index = 0; index < batchSize; index += 1) {
    const id = `${__VU}-${__ITER}-${index}-${Date.now()}`;
    events.push({
      event_type: "profile.updated",
      schema_version: 1,
      external_id: `load-${id}`,
      idempotency_key: id,
      occurred_at: new Date().toISOString(),
      source: "load-test",
      data_classification: "internal",
      payload: { attributes: { cohort: "load" } },
    });
  }
  const response = http.post(`${endpoint}/v1/events/batch`, JSON.stringify({
    events,
  }), { headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" } });
  check(response, { accepted: (result) => result.status === 202 });
}
