import http from "k6/http";
import { check, sleep } from "k6";

const target = __ENV.TARGET_URL || "http://host.docker.internal:18080";

export const options = {
  vus: 1,
  duration: "30s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<300"],
    checks: ["rate>0.99"],
  },
};

export default function () {
  const res = http.get(`${target}/health/ready`);
  check(res, {
    "ready is 200": (r) => r.status === 200,
  });
  sleep(1);
}
