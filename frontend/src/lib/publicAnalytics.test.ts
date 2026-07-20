/// <reference types="vitest/globals" />
import { describe, expect, it, vi } from 'vitest';

import {
  createPublicAnalytics,
  createPublicAnalyticsFromEnv,
  createSameOriginPublicAnalyticsTransport,
  isPublicAnalyticsEnabled,
} from './publicAnalytics';
import {
  PUBLIC_ANALYTICS_SCHEMA,
  type PublicAnalyticsEvent,
  type PublicAnalyticsTransport,
} from '../types/publicAnalytics';

function recordingTransport(): {
  events: PublicAnalyticsEvent[];
  transport: PublicAnalyticsTransport;
} {
  const events: PublicAnalyticsEvent[] = [];
  return {
    events,
    transport: {
      send: (event) => {
        events.push(event);
      },
    },
  };
}

describe('public analytics taxonomy', () => {
  it('defines the eight INF-57 funnel events', () => {
    expect(Object.keys(PUBLIC_ANALYTICS_SCHEMA)).toEqual([
      'public_landing_view',
      'public_primary_cta_clicked',
      'public_product_explored',
      'public_resource_opened',
      'public_sign_in_intent',
      'activation_first_model_list_succeeded',
      'activation_first_unary_inference_succeeded',
      'activation_first_streaming_inference_succeeded',
    ]);
  });

  it('emits only typed, allowlisted properties', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });

    analytics.track('public_resource_opened', {
      resource: 'quickstart',
      source: 'landing',
    });

    expect(events).toEqual([
      {
        name: 'public_resource_opened',
        properties: { resource: 'quickstart', source: 'landing' },
      },
    ]);
    expect(Object.isFrozen(events[0])).toBe(true);
    expect(Object.isFrozen(events[0]?.properties)).toBe(true);
  });
});

describe('public analytics privacy boundary', () => {
  it.each(['api_key', 'prompt', 'copied_code', 'payload', 'invitation_token', 'model_output', 'user_id'])(
    'rejects the entire event when the runtime payload includes %s',
    (sensitiveKey) => {
      const { events, transport } = recordingTransport();
      const analytics = createPublicAnalytics({ enabled: true, transport });
      const properties = {
        surface: 'migration_landing',
        [sensitiveKey]: 'must-not-leave-the-call-site',
      };

      analytics.track('public_landing_view', properties as never);

      expect(events).toEqual([]);
    },
  );

  it('rejects non-enumerated property values at runtime', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });

    analytics.track('public_sign_in_intent', { source: 'email-address' } as never);

    expect(events).toEqual([]);
  });

  it('rejects properties whose getter throws', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });
    const properties = Object.defineProperty({}, 'source', {
      enumerable: true,
      get: () => {
        throw new Error('sensitive getter failure');
      },
    });

    expect(() => {
      analytics.track('public_sign_in_intent', properties as never);
    }).not.toThrow();
    expect(events).toEqual([]);
  });

  it('rejects accessors without invoking them', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });
    const getter = vi.fn(() => 'landing');
    const properties = Object.defineProperty({}, 'source', {
      enumerable: true,
      get: getter,
    });

    analytics.track('public_sign_in_intent', properties as never);

    expect(getter).not.toHaveBeenCalled();
    expect(events).toEqual([]);
  });

  it('rejects symbol and non-enumerable properties', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });
    const symbolProperties = {
      surface: 'migration_landing',
      [Symbol('prompt')]: 'must-not-leave-the-call-site',
    };
    const hiddenProperties = Object.defineProperty(
      { surface: 'migration_landing' },
      'api_key',
      { value: 'must-not-leave-the-call-site' },
    );

    analytics.track('public_landing_view', symbolProperties as never);
    analytics.track('public_landing_view', hiddenProperties as never);

    expect(events).toEqual([]);
  });
});

describe('public analytics failure isolation', () => {
  it('is disabled unless the environment value is explicitly true', () => {
    expect(isPublicAnalyticsEnabled(undefined)).toBe(false);
    expect(isPublicAnalyticsEnabled(false)).toBe(false);
    expect(isPublicAnalyticsEnabled('false')).toBe(false);
    expect(isPublicAnalyticsEnabled('TRUE')).toBe(false);
    expect(isPublicAnalyticsEnabled(true)).toBe(true);
    expect(isPublicAnalyticsEnabled('true')).toBe(true);
  });

  it('does not send when disabled by environment', () => {
    const send = vi.fn();
    const analytics = createPublicAnalyticsFromEnv({}, { send });

    analytics.track('public_landing_view', { surface: 'migration_landing' });

    expect(send).not.toHaveBeenCalled();
  });

  it('never throws when a transport fails synchronously', () => {
    const analytics = createPublicAnalytics({
      enabled: true,
      transport: {
        send: () => {
          throw new Error('transport unavailable');
        },
      },
    });

    expect(() => {
      analytics.track('public_primary_cta_clicked', {
        action: 'start_building',
        placement: 'hero',
      });
    }).not.toThrow();
  });

  it('consumes asynchronous transport failures', async () => {
    const unhandledRejection = vi.fn();
    window.addEventListener('unhandledrejection', unhandledRejection);
    const analytics = createPublicAnalytics({
      enabled: true,
      transport: {
        send: () => Promise.reject(new Error('transport unavailable')),
      },
    });

    analytics.track('public_landing_view', { surface: 'migration_landing' });
    await Promise.resolve();
    await Promise.resolve();

    expect(unhandledRejection).not.toHaveBeenCalled();
    window.removeEventListener('unhandledrejection', unhandledRejection);
  });
});

describe('same-origin public analytics transport', () => {
  it('posts only the sanitized event without credentials', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 204 });
    const transport = createSameOriginPublicAnalyticsTransport(fetcher as typeof fetch);
    const event: PublicAnalyticsEvent = {
      name: 'public_sign_in_intent',
      properties: { source: 'public_navigation' },
    };

    await transport.send(event);

    expect(fetcher).toHaveBeenCalledWith('/api/public-analytics/events', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(event),
      credentials: 'omit',
      keepalive: true,
    });
  });

  it('rejects non-success responses so the adapter can consume the failure', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: false, status: 429 });
    const transport = createSameOriginPublicAnalyticsTransport(fetcher as typeof fetch);

    await expect(transport.send({
      name: 'public_landing_view',
      properties: { surface: 'migration_landing' },
    })).rejects.toThrow('Analytics endpoint returned 429');
  });
});

describe('first activation events', () => {
  it('emits each first-success event at most once per adapter', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({ enabled: true, transport });

    analytics.trackFirst('activation_first_unary_inference_succeeded', {
      surface: 'playground',
    });
    analytics.trackFirst('activation_first_unary_inference_succeeded', {
      surface: 'playground',
    });

    expect(events).toHaveLength(1);
  });

  it('uses session storage to deduplicate across adapter instances', () => {
    const { events, transport } = recordingTransport();
    const storage = window.sessionStorage;
    storage.clear();

    createPublicAnalytics({ enabled: true, transport, storage }).trackFirst(
      'activation_first_model_list_succeeded',
      { surface: 'model_catalog' },
    );
    createPublicAnalytics({ enabled: true, transport, storage }).trackFirst(
      'activation_first_model_list_succeeded',
      { surface: 'model_catalog' },
    );

    expect(events).toHaveLength(1);
    storage.clear();
  });

  it('falls back to in-memory deduplication when storage throws', () => {
    const { events, transport } = recordingTransport();
    const analytics = createPublicAnalytics({
      enabled: true,
      transport,
      storage: {
        getItem: () => {
          throw new Error('storage denied');
        },
        setItem: () => {
          throw new Error('storage denied');
        },
      },
    });

    expect(() => {
      analytics.trackFirst('activation_first_streaming_inference_succeeded', {
        surface: 'onboarding',
      });
      analytics.trackFirst('activation_first_streaming_inference_succeeded', {
        surface: 'onboarding',
      });
    }).not.toThrow();
    expect(events).toHaveLength(1);
  });
});
