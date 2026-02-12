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

export function workflowScaledDown(listPayload, expectedNames, strict) {
  if (!Array.isArray(listPayload)) {
    return false;
  }
  const stateByName = new Map();
  for (const item of listPayload) {
    if (item && item.name) {
      stateByName.set(item.name, item.running === true);
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
