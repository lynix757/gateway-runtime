import http from "k6/http";
import { check } from "k6";

const target = __ENV.TARGET_URL || "http://host.docker.internal:18080";
const path = __ENV.PATH || "/api/me";
const rate = Number(__ENV.RATE || "10");
const duration = __ENV.DURATION || "2m";
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || "20");
const maxVUs = Number(__ENV.MAX_VUS || "500");
const sessionCookie = __ENV.SESSION_COOKIE || "";

export const options = {
  scenarios: {
    api: {
      executor: "constant-arrival-rate",
      rate,
      timeUnit: "1s",
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<300"],
    checks: ["rate>0.99"],
  },
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
};

export default function () {
  const headers = {};
  if (sessionCookie) {
    headers.Cookie = `__Host-bff_session=${sessionCookie}`;
  }

  const res = http.get(`${target}${path}`, {
    headers,
    tags: { endpoint: path },
  });

  const expected = sessionCookie || path.includes("/health/")
    ? 200
    : 401;

  check(res, {
    "expected status": (r) => r.status === expected,
  });
}
