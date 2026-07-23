/**
 * Public-funnel analytics is deliberately limited to fixed, non-sensitive
 * dimensions. Do not add free-form strings or identifiers to this schema.
 */
export const PUBLIC_ANALYTICS_SCHEMA = {
  public_landing_view: {
    surface: ['migration_landing'],
  },
  public_primary_cta_clicked: {
    action: ['start_building', 'evaluate_deployment_fit', 'request_design_partner_access'],
    placement: ['hero', 'closing', 'public_navigation', 'quickstart', 'footer'],
  },
  public_product_explored: {
    product: ['model_catalog', 'playground', 'openai_compatibility'],
    source: ['landing', 'public_navigation'],
  },
  public_resource_opened: {
    resource: ['quickstart', 'api_docs', 'evaluation'],
    source: ['landing', 'public_navigation', 'onboarding'],
  },
  public_sign_in_intent: {
    source: ['landing', 'public_navigation', 'onboarding', 'invitation', 'sign_in_form'],
  },
  design_partner_request_started: {
    source: ['request_access'],
  },
  design_partner_request_submitted: {
    outcome: ['succeeded', 'validation_failed', 'delivery_failed', 'configuration_missing'],
  },
  activation_first_model_list_succeeded: {
    surface: ['onboarding', 'model_catalog'],
  },
  activation_first_unary_inference_succeeded: {
    surface: ['onboarding', 'playground', 'model_catalog'],
  },
  activation_first_streaming_inference_succeeded: {
    surface: ['onboarding', 'playground'],
  },
} as const;

export type PublicAnalyticsEventName = keyof typeof PUBLIC_ANALYTICS_SCHEMA;

type PublicAnalyticsSchema = typeof PUBLIC_ANALYTICS_SCHEMA;

export type PublicAnalyticsProperties<Name extends PublicAnalyticsEventName> = {
  readonly [Property in keyof PublicAnalyticsSchema[Name]]:
    PublicAnalyticsSchema[Name][Property] extends readonly (infer Value)[]
      ? Value
      : never;
};

export type PublicAnalyticsEvent<
  Name extends PublicAnalyticsEventName = PublicAnalyticsEventName,
> = Name extends PublicAnalyticsEventName
  ? Readonly<{
      name: Name;
      properties: Readonly<PublicAnalyticsProperties<Name>>;
    }>
  : never;

export type FirstActivationEventName = Extract<
  PublicAnalyticsEventName,
  | 'activation_first_model_list_succeeded'
  | 'activation_first_unary_inference_succeeded'
  | 'activation_first_streaming_inference_succeeded'
>;

export interface PublicAnalyticsTransport {
  send(event: PublicAnalyticsEvent): void | Promise<void>;
}

export interface PublicAnalytics {
  track<Name extends PublicAnalyticsEventName>(
    name: Name,
    properties: PublicAnalyticsProperties<Name>,
  ): void;
  trackFirst<Name extends FirstActivationEventName>(
    name: Name,
    properties: PublicAnalyticsProperties<Name>,
  ): void;
}
