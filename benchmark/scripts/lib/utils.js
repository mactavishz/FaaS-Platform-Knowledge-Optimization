import encoding from 'k6/encoding';

const defaultGatewayUrl = 'http://127.0.0.1:8080';

const platformDefaults = {
  tinyfaas: {
    invokePath: '/fn/{name}',
    listPath: '/system/list',
  },
  faasd: {
    invokePath: '/function/{name}',
    listPath: '/system/functions',
  },
};

export function envString(key, defaultValue, required) {
  const value = __ENV[key];
  if (value === undefined || value === '') {
    if (required) {
      throw new Error(`Missing required env var: ${key}`);
    }
    return defaultValue;
  }
  return value;
}

export function envInt(key, defaultValue) {
  const raw = __ENV[key];
  if (raw === undefined || raw === '') {
    return defaultValue;
  }
  const parsed = parseInt(raw, 10);
  if (Number.isNaN(parsed)) {
    throw new Error(`Invalid integer for ${key}: ${raw}`);
  }
  return parsed;
}

export function envBool(key, defaultValue) {
  const raw = __ENV[key];
  if (raw === undefined || raw === '') {
    return defaultValue;
  }
  return raw.toLowerCase() === 'true' || raw === '1' || raw.toLowerCase() === 'yes';
}

export function envList(key, required) {
  const raw = envString(key, '', required);
  if (!raw) {
    return [];
  }
  return raw
    .split(',')
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

export function normalizeBaseUrl(url) {
  if (!url) {
    return '';
  }
  return url.endsWith('/') ? url.slice(0, -1) : url;
}

export function buildInvokePath(template, entryFunction) {
  if (template.includes('{name}')) {
    return template.replace('{name}', entryFunction);
  }
  if (template.endsWith('/')) {
    return `${template}${entryFunction}`;
  }
  return `${template}/${entryFunction}`;
}

export function resolveBenchmarkConfig() {
  const platform = envString('PLATFORM', 'tinyfaas', false).toLowerCase();
  const defaults = platformDefaults[platform];
  if (!defaults) {
    throw new Error(`Invalid PLATFORM: ${platform}. Expected tinyfaas or faasd`);
  }

  return {
    platform,
    gatewayUrl: normalizeBaseUrl(envString('GATEWAY_URL', defaultGatewayUrl, false)),
    invokePathTemplate: envString('INVOKE_PATH', defaults.invokePath, false),
    listPath: envString('LIST_PATH', defaults.listPath, false),
    authHeaders: basicAuthHeaders(),
  };
}

export function withAuthHeaders(headers, authHeaders) {
  return {
    ...headers,
    ...authHeaders,
  };
}

function basicAuthHeaders() {
  const user = envString('BASIC_AUTH_USER', '', false);
  const password = envString('BASIC_AUTH_PASSWORD', '', false);
  if (!user || !password) {
    return {};
  }

  return {
    Authorization: `Basic ${encoding.b64encode(`${user}:${password}`, 'std')}`,
  };
}

export function parseJsonSafe(raw) {
  if (!raw) {
    return null;
  }
  try {
    return JSON.parse(raw);
  } catch (error) {
    return null;
  }
}

export function workflowScaledDown(listPayload, expectedNames, strict, platform = 'tinyfaas') {
  if (!Array.isArray(listPayload)) {
    return false;
  }
  const stateByName = new Map();
  for (const item of listPayload) {
    if (item && item.name) {
      stateByName.set(item.name, functionRunning(item, platform));
    }
  }

  for (const name of expectedNames) {
    if (!stateByName.has(name)) {
      if (strict) {
        throw new Error(`Function missing from list: ${name}`);
      }
      continue;
    }
    if (stateByName.get(name)) {
      return false;
    }
  }
  return true;
}

function functionRunning(item, platform) {
  if (platform === 'faasd') {
    return Number(item.availableReplicas || 0) > 0;
  }
  return item.running === true;
}
