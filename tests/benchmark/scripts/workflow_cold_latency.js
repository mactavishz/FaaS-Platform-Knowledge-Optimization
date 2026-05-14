import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import {
  buildInvokePath,
  envBool,
  envInt,
  envList,
  envString,
  parseJsonSafe,
  resolveBenchmarkConfig,
  withAuthHeaders,
  workflowScaledDown,
} from './lib/utils.js';

const entryLatencyMs = new Trend('entry_e2e_ms');
const scaleDownWaitMs = new Trend('scale_down_wait_ms');
const invokeFailures = new Counter('invoke_failures');
const listFailures = new Counter('list_failures');
const scaleDownTimeouts = new Counter('scale_down_timeouts');

const benchmarkConfig = resolveBenchmarkConfig();
const gatewayUrl = benchmarkConfig.gatewayUrl;
const entryFunction = envString('ENTRY_FUNCTION', '', true);
const workflowFunctions = envList('WORKFLOW_FUNCTIONS', true);
const invokePathTemplate = benchmarkConfig.invokePathTemplate;
const listPath = benchmarkConfig.listPath;
const method = envString('METHOD', 'POST', false).toUpperCase();
const body = envString('BODY', '{}', false);
const expectedStatus = envInt('EXPECTED_STATUS', 200);
const invokeTimeoutMs = envInt('INVOKE_TIMEOUT_MS', 60000);
const pollIntervalMs = envInt('POLL_INTERVAL_MS', 500);
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
  const tags = { entry: entryFunction, workflow: workflowFunctions.join(',') };
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

  entryLatencyMs.add(response.timings.duration, tags);

  const waitStart = Date.now();
  while (true) {
    const elapsed = Date.now() - waitStart;
    if (elapsed > scaleDownTimeoutMs) {
      scaleDownTimeouts.add(1, tags);
      throw new Error(
        `Scale-down wait exceeded ${scaleDownTimeoutMs}ms for workflow ${workflowFunctions.join(',')}`,
      );
    }

    const listResponse = http.get(listUrl, {
      headers: benchmarkConfig.authHeaders,
      timeout: `${invokeTimeoutMs}ms`,
      tags,
    });
    const listOk = check(listResponse, {
      'list status ok': (res) => res.status === 200,
    });

    if (!listOk) {
      listFailures.add(1, tags);
    } else {
      const payload = parseJsonSafe(listResponse.body);
      if (!payload) {
        listFailures.add(1, tags);
      } else if (
        workflowScaledDown(payload, workflowFunctions, strictFunctions, benchmarkConfig.platform)
      ) {
        scaleDownWaitMs.add(Date.now() - waitStart, tags);
        break;
      }
    }

    sleep(pollIntervalMs / 1000);
  }
}
