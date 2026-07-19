import {
  PUBLIC_ANALYTICS_SCHEMA,
  type FirstActivationEventName,
  type PublicAnalytics,
  type PublicAnalyticsEvent,
  type PublicAnalyticsEventName,
  type PublicAnalyticsProperties,
  type PublicAnalyticsTransport,
} from '../types/publicAnalytics';

const ENABLED_VALUE = 'true';
const STORAGE_PREFIX = 'infera.public-analytics.first.';

const noopTransport: PublicAnalyticsTransport = {
  send: () => undefined,
};

interface AnalyticsStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

interface CreatePublicAnalyticsOptions {
  enabled: boolean;
  transport?: PublicAnalyticsTransport;
  storage?: AnalyticsStorage;
}

interface PublicAnalyticsEnvironment {
  readonly VITE_PUBLIC_ANALYTICS_ENABLED?: string | boolean;
}

function sanitizeProperties<Name extends PublicAnalyticsEventName>(
  name: Name,
  properties: unknown,
): PublicAnalyticsProperties<Name> | undefined {
  if (typeof properties !== 'object' || properties === null || Array.isArray(properties)) {
    return undefined;
  }

  const schema = PUBLIC_ANALYTICS_SCHEMA[name] as Record<string, readonly string[]>;
  const schemaKeys = Object.keys(schema);
  const propertyKeys = Reflect.ownKeys(properties);

  if (
    propertyKeys.length !== schemaKeys.length
    || propertyKeys.some((key) => typeof key !== 'string' || !schemaKeys.includes(key))
  ) {
    return undefined;
  }

  const sanitized: Record<string, string> = {};
  for (const key of schemaKeys) {
    const descriptor = Object.getOwnPropertyDescriptor(properties, key);
    const allowedValues = schema[key];
    if (
      descriptor === undefined
      || !descriptor.enumerable
      || !('value' in descriptor)
      || allowedValues === undefined
      || typeof descriptor.value !== 'string'
      || !allowedValues.includes(descriptor.value)
    ) {
      return undefined;
    }
    sanitized[key] = descriptor.value;
  }

  return sanitized as PublicAnalyticsProperties<Name>;
}

function createSafeEvent<Name extends PublicAnalyticsEventName>(
  name: Name,
  properties: unknown,
): PublicAnalyticsEvent<Name> | undefined {
  try {
    const sanitizedProperties = sanitizeProperties(name, properties);
    if (sanitizedProperties === undefined) {
      return undefined;
    }

    return Object.freeze({
      name,
      properties: Object.freeze(sanitizedProperties),
    }) as PublicAnalyticsEvent<Name>;
  } catch {
    return undefined;
  }
}

function sendSafely(
  transport: PublicAnalyticsTransport,
  event: PublicAnalyticsEvent,
): void {
  try {
    void Promise.resolve(transport.send(event)).catch(() => undefined);
  } catch {
    // Analytics must never interrupt the user journey.
  }
}

export function isPublicAnalyticsEnabled(
  value: string | boolean | undefined,
): boolean {
  return value === true || value === ENABLED_VALUE;
}

export function createPublicAnalytics({
  enabled,
  transport = noopTransport,
  storage,
}: CreatePublicAnalyticsOptions): PublicAnalytics {
  const seenFirstEvents = new Set<FirstActivationEventName>();

  const track = <Name extends PublicAnalyticsEventName>(
    name: Name,
    properties: PublicAnalyticsProperties<Name>,
  ): void => {
    if (!enabled) {
      return;
    }

    const event = createSafeEvent(name, properties);
    if (event !== undefined) {
      sendSafely(transport, event);
    }
  };

  const trackFirst = <Name extends FirstActivationEventName>(
    name: Name,
    properties: PublicAnalyticsProperties<Name>,
  ): void => {
    if (!enabled || seenFirstEvents.has(name)) {
      return;
    }

    const event = createSafeEvent(name, properties);
    if (event === undefined) {
      return;
    }

    const storageKey = `${STORAGE_PREFIX}${name}`;
    try {
      if (storage?.getItem(storageKey) === ENABLED_VALUE) {
        seenFirstEvents.add(name);
        return;
      }
      storage?.setItem(storageKey, ENABLED_VALUE);
    } catch {
      // The in-memory guard still prevents duplicates if storage is unavailable.
    }

    seenFirstEvents.add(name);
    sendSafely(transport, event);
  };

  return Object.freeze({ track, trackFirst });
}

export function createPublicAnalyticsFromEnv(
  environment: PublicAnalyticsEnvironment,
  transport?: PublicAnalyticsTransport,
  storage?: AnalyticsStorage,
): PublicAnalytics {
  return createPublicAnalytics({
    enabled: isPublicAnalyticsEnabled(environment.VITE_PUBLIC_ANALYTICS_ENABLED),
    transport,
    storage,
  });
}

function browserSessionStorage(): AnalyticsStorage | undefined {
  try {
    return typeof window === 'undefined' ? undefined : window.sessionStorage;
  } catch {
    return undefined;
  }
}

const viteEnvironment = (
  import.meta as ImportMeta & { readonly env?: PublicAnalyticsEnvironment }
).env ?? {};

/**
 * Safe default for call sites. It remains a no-op until an approved transport is
 * supplied at composition time; this module performs no network requests.
 */
export const publicAnalytics = createPublicAnalyticsFromEnv(
  viteEnvironment,
  undefined,
  browserSessionStorage(),
);
