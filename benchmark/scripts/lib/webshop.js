import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import {
  buildInvokePath,
  envBool,
  envInt,
  envString,
  parseJsonSafe,
  randomWebshopCartItem,
  resolveBenchmarkConfig,
  resolveWorkflowPreset,
  waitForScaleDown,
  withAuthHeaders,
} from './utils.js';

export const invokeFailures = new Counter('invoke_failures');
export const listFailures = new Counter('list_failures');
export const stateValidationFailures = new Counter('state_validation_failures');
export const scaleDownTimeouts = new Counter('scale_down_timeouts');

export const benchmarkConfig = resolveBenchmarkConfig();
export const workflow = resolveWorkflowPreset('webshop');
if (workflow.name !== 'webshop') {
  throw new Error('webshop benchmark scripts only support WORKFLOW=webshop');
}

export const gatewayUrl = benchmarkConfig.gatewayUrl;
export const entryFunction = workflow.entryFunction;
export const invokePathTemplate = benchmarkConfig.invokePathTemplate;
export const listPath = benchmarkConfig.listPath;

export const workflowFunctions = workflow.functions;
export const strictFunctions = envBool('STRICT_FUNCTIONS', true);

export const expectedStatus = envInt('EXPECTED_STATUS', 200);
export const invokeTimeoutMs = envInt('INVOKE_TIMEOUT_MS', 60000);
export const pollIntervalMs = envInt('POLL_INTERVAL_MS', 15000);
export const scaleDownTimeoutMs = envInt('SCALE_DOWN_TIMEOUT_MS', 120000);
export const maxDuration = envString('MAX_DURATION', '60m', false);
export const gracefulStop = envString('GRACEFUL_STOP', '30s', false);

export const currency = envString('CURRENCY', 'EUR', false);
export const runId = envString('RUN_ID', 'webshop-bench', false);
export const runLabel = envString('RUN_LABEL', '', false);

export const checkoutAddress = parseJsonOrDefault(
  envString('CHECKOUT_ADDRESS_JSON', '{"street":"123 Main St"}', false),
  { street: '123 Main St' },
  'CHECKOUT_ADDRESS_JSON',
);
export const checkoutEmail = envString('CHECKOUT_EMAIL', 'bench-user@example.com', false);
export const checkoutCreditCard = parseJsonOrDefault(
  envString('CHECKOUT_CREDIT_CARD_JSON', '{"creditCardNumber":"4111111111111111"}', false),
  { creditCardNumber: '4111111111111111' },
  'CHECKOUT_CREDIT_CARD_JSON',
);

const frontendUrl = `${gatewayUrl}${buildInvokePath(invokePathTemplate, entryFunction)}`;
const listUrl = `${gatewayUrl}${listPath}`;

export function createOptions() {
  return {
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
}

export function createUserId() {
  return `${runId}-vu${__VU}-iter${__ITER}`;
}

export function createBaseTags() {
  return {
    entry: entryFunction,
    workflow: workflow.name,
    label: runLabel,
  };
}

export function randomCartItem() {
  return randomWebshopCartItem();
}

export function invokeFrontend(operation, payload, tags) {
  const requestBody = JSON.stringify({ operation, ...payload });
  const response = http.post(frontendUrl, requestBody, {
    headers: withAuthHeaders({ 'Content-Type': 'application/json' }, benchmarkConfig.authHeaders),
    timeout: `${invokeTimeoutMs}ms`,
    tags,
  });

  const statusOk = check(response, {
    'invoke status ok': (res) => res.status === expectedStatus,
  });

  if (!statusOk) {
    invokeFailures.add(1, tags);
  }

  return {
    response,
    body: parseJsonSafe(response.body),
  };
}

export function waitForInitialScaleDown(tags) {
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

export function validateBrowsePayload(payload, baseTags) {
  if (!payload) {
    stateValidationFailures.add(1, { ...baseTags, step: 'browse_validate' });
    return;
  }

  const valid = check(payload, {
    'browse returns products': (data) =>
      Array.isArray(data.productsList) && data.productsList.length === 11,
    'browse returns cart': (data) => Array.isArray(data.cart),
    'browse returns recommendations': (data) => Array.isArray(data.recommendations),
  });

  if (!valid) {
    stateValidationFailures.add(1, { ...baseTags, step: 'browse_validate' });
  }
}

export function validateAddcartPayload(payload, product, baseTags) {
  const valid = check(payload, {
    'addcart payload contains updated cart': (data) => data && Array.isArray(data.cart),
    'addcart payload contains added product': (data) =>
      data &&
      Array.isArray(data.cart) &&
      data.cart.some((row) => row && String(row.itemId) === product.productId),
    'addcart payload quantity matches': (data) => {
      if (!data || !Array.isArray(data.cart)) {
        return false;
      }
      const cartItem = data.cart.find((row) => row && String(row.itemId) === product.productId);
      return (
        cartItem &&
        (cartItem.quantity === undefined || Number(cartItem.quantity) === Number(product.quantity))
      );
    },
  });

  if (!valid) {
    stateValidationFailures.add(1, {
      ...baseTags,
      step: 'addcart_validate',
      product_id: product.productId,
      quantity: String(product.quantity),
    });
  }
}

export function validateCheckoutPayload(payload, userId, baseTags) {
  const valid = check(payload, {
    'checkout returns valid payload': (data) => data && data.userId === userId,
  });

  if (!valid) {
    stateValidationFailures.add(1, { ...baseTags, step: 'checkout_validate' });
  }
}

function parseJsonOrDefault(raw, defaultValue, envKey) {
  const parsed = parseJsonSafe(raw);
  if (parsed === null) {
    throw new Error(`Invalid JSON for ${envKey}`);
  }
  if (typeof parsed !== 'object' || Array.isArray(parsed)) {
    return defaultValue;
  }
  return parsed;
}
