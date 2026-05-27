import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import {
  buildInvokePath,
  envBool,
  envInt,
  envString,
  resolveBenchmarkConfig,
  resolveWorkflowPreset,
  waitForScaleDown,
  withAuthHeaders,
} from './lib/utils.js';

const workflowLatencyMs = new Trend('workflow_latency_ms');
const invokeFailures = new Counter('invoke_failures');
const listFailures = new Counter('list_failures');
const scaleDownTimeouts = new Counter('scale_down_timeouts');

const benchmarkConfig = resolveBenchmarkConfig();
const workflow = resolveWorkflowPreset();
const gatewayUrl = benchmarkConfig.gatewayUrl;
const entryFunction = workflow.entryFunction;
const workflowFunctions = workflow.functions;
const invokePathTemplate = benchmarkConfig.invokePathTemplate;
const listPath = benchmarkConfig.listPath;
const method = envString('METHOD', 'POST', false).toUpperCase();
const body = envString('BODY', '{}', false);
const expectedStatus = envInt('EXPECTED_STATUS', 200);
const invokeTimeoutMs = envInt('INVOKE_TIMEOUT_MS', 60000);
const pollIntervalMs = envInt('POLL_INTERVAL_MS', 15000);
const scaleDownTimeoutMs = envInt('SCALE_DOWN_TIMEOUT_MS', 900000);
const strictFunctions = envBool('STRICT_FUNCTIONS', true);
const maxDuration = envString('MAX_DURATION', '60m', false);
const gracefulStop = envString('GRACEFUL_STOP', '30s', false);

export const options = {
  scenarios: {
    default: {
      executor: 'shared-iterations',
      vus: envInt('VUS', 1),
      iterations: envInt('ITERATIONS', 10),
      maxDuration,
      gracefulStop,
    },
  },
};

export default function () {
  const invokeUrl = `${gatewayUrl}${buildInvokePath(invokePathTemplate, entryFunction)}`;
  const listUrl = `${gatewayUrl}${listPath}`;
  const tags = { entry: entryFunction, workflow: workflow.name, platform: benchmarkConfig.platform };
  const requestBody = method === 'GET' || method === 'HEAD' ? null : body;

  const response = http.request(method, invokeUrl, requestBody, {
    headers: withAuthHeaders({ 'Content-Type': 'application/json' }, benchmarkConfig.authHeaders),
    timeout: `${invokeTimeoutMs}ms`,
    tags,
  });

  const invokeOk = check(response, {
    'invoke status ok': (res) => res.status === expectedStatus,
  });

  if (!invokeOk) {
    invokeFailures.add(1, tags);
  }

  workflowLatencyMs.add(response.timings.duration, tags);

  waitForScaleDown({
    listUrl,
    authHeaders: benchmarkConfig.authHeaders,
    timeoutMs: invokeTimeoutMs,
    pollIntervalMs,
    scaleDownTimeoutMs,
    workflowFunctions,
    strictFunctions,
    platform: benchmarkConfig.platform,
    tags,
    listFailures,
    scaleDownTimeouts,
  });
}
