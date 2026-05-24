import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import {
  buildInvokePath,
  envBool,
  envInt,
  envString,
  parseJsonSafe,
  randomWebshopCartItem,
  resolveBenchmarkConfig,
  resolveWorkflowPreset,
  withAuthHeaders,
  workflowScaledDown,
} from './lib/utils.js';

const browseMs = new Trend('browse_latency_ms');
const addcartMs = new Trend('addcart_latency_ms');
const checkoutMs = new Trend('checkout_latency_ms');

const invokeFailures = new Counter('invoke_failures');
const listFailures = new Counter('list_failures');
const stateValidationFailures = new Counter('state_validation_failures');
const scaleDownTimeouts = new Counter('scale_down_timeouts');

const benchmarkConfig = resolveBenchmarkConfig();
const workflow = resolveWorkflowPreset('webshop');
if (workflow.name !== 'webshop') {
  throw new Error('webshop_user_journey.js only supports WORKFLOW=webshop');
}

const gatewayUrl = benchmarkConfig.gatewayUrl;
const entryFunction = workflow.entryFunction;
const invokePathTemplate = benchmarkConfig.invokePathTemplate;
const listPath = benchmarkConfig.listPath;

const workflowFunctions = workflow.functions;
const strictFunctions = envBool('STRICT_FUNCTIONS', true);

const expectedStatus = envInt('EXPECTED_STATUS', 200);
const invokeTimeoutMs = envInt('INVOKE_TIMEOUT_MS', 60000);
const pollIntervalMs = envInt('POLL_INTERVAL_MS', 500);
const scaleDownTimeoutMs = envInt('SCALE_DOWN_TIMEOUT_MS', 120000);
const maxDuration = envString('MAX_DURATION', '60m', false);
const gracefulStop = envString('GRACEFUL_STOP', '30s', false);

const currency = envString('CURRENCY', 'EUR', false);
const runId = envString('RUN_ID', 'webshop-bench', false);
const runLabel = envString('RUN_LABEL', '', false);

const checkoutAddress = parseJsonOrDefault(
  envString('CHECKOUT_ADDRESS_JSON', '{"street":"123 Main St"}', false),
  { street: '123 Main St' },
  'CHECKOUT_ADDRESS_JSON',
);
const checkoutEmail = envString('CHECKOUT_EMAIL', 'bench-user@example.com', false);
const checkoutCreditCard = parseJsonOrDefault(
  envString('CHECKOUT_CREDIT_CARD_JSON', '{"creditCardNumber":"4111111111111111"}', false),
  { creditCardNumber: '4111111111111111' },
  'CHECKOUT_CREDIT_CARD_JSON',
);

const frontendUrl = `${gatewayUrl}${buildInvokePath(invokePathTemplate, entryFunction)}`;
const listUrl = `${gatewayUrl}${listPath}`;

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
  const userId = `${runId}-vu${__VU}-iter${__ITER}`;
  const baseTags = {
    entry: entryFunction,
    workflow: workflow.name,
    label: runLabel,
  };

  waitForScaleDown({ ...baseTags, phase: 'before_browse' });

  const browseResult = invokeFrontend(
    'get',
    {
      userId,
      currency,
    },
    { ...baseTags, step: 'browse' },
  );
  browseMs.add(browseResult.response.timings.duration, { ...baseTags, step: 'browse' });
  validateBrowsePayload(browseResult.body, baseTags);

  waitForScaleDown({ ...baseTags, phase: 'before_addcart' });

  const product = randomWebshopCartItem();
  const productTags = {
    ...baseTags,
    product_id: product.productId,
    quantity: String(product.quantity),
  };
  const addcartResult = invokeFrontend(
    'addcart',
    {
      userId,
      productId: product.productId,
      quantity: product.quantity,
    },
    { ...productTags, step: 'addcart' },
  );
  addcartMs.add(addcartResult.response.timings.duration, {
    ...productTags,
    step: 'addcart',
  });
  validateAddcartPayload(addcartResult.body, product, baseTags);

  waitForScaleDown({ ...baseTags, phase: 'before_checkout' });

  const checkoutResult = invokeFrontend(
    'checkout',
    {
      userId,
      currency,
      address: checkoutAddress,
      email: checkoutEmail,
      creditCard: checkoutCreditCard,
    },
    { ...baseTags, step: 'checkout' },
  );
  checkoutMs.add(checkoutResult.response.timings.duration, { ...baseTags, step: 'checkout' });
  validateCheckoutPayload(checkoutResult.body, userId, baseTags);
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

function invokeFrontend(operation, payload, tags) {
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

function waitForScaleDown(tags) {
  if (workflowFunctions.length === 0) {
    return;
  }

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
        break;
      }
    }

    sleep(pollIntervalMs / 1000);
  }
}

function validateBrowsePayload(payload, baseTags) {
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

function validateAddcartPayload(payload, product, baseTags) {
  const valid = check(payload, {
    'addcart playload contains updated cart': (data) => data && Array.isArray(data.cart),
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

function validateCheckoutPayload(payload, userId, baseTags) {
  const valid = check(payload, {
    'checkout returns valid payload': (data) => data && data.userId === userId,
  });

  if (!valid) {
    stateValidationFailures.add(1, { ...baseTags, step: 'checkout_validate' });
  }
}
