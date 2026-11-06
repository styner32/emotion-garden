export const CONVERSATION_KINDS = ['release', 'grow'] as const;
export type ConversationKind = (typeof CONVERSATION_KINDS)[number];

export const CONVERSATION_STAGES = ['chat', 'release_ritual', 'gift_ready', 'planting', 'completed'] as const;
export type ConversationStage = (typeof CONVERSATION_STAGES)[number];

export const REPLY_POLICIES = ['apology', 'quiet', 'supportive'] as const;
export type ReplyPolicy = (typeof REPLY_POLICIES)[number];

export const GENERATION_MODES = ['openai', 'gemini', 'compare'] as const;
export type GenerationMode = (typeof GENERATION_MODES)[number];

export const TURN_MODES = ['quiet', 'openai', 'gemini', 'compare'] as const;
export type TurnMode = (typeof TURN_MODES)[number];

export const RUN_SLOTS = ['primary', 'a', 'b'] as const;
export type RunSlot = (typeof RUN_SLOTS)[number];

export const RUN_STATUSES = [
  'queued',
  'running',
  'streaming',
  'succeeded',
  'failed',
  'incomplete',
  'blocked',
  'cancelled',
  'interrupted',
] as const;
export type RunStatus = (typeof RUN_STATUSES)[number];

export const USAGE_STATUSES = ['known', 'partial', 'unknown'] as const;
export type UsageStatus = (typeof USAGE_STATUSES)[number];

export const RITUAL_ROUTES = ['common', 'sea', 'incinerate', 'bury', 'garden'] as const;
export type RitualRoute = (typeof RITUAL_ROUTES)[number];

export const RITUAL_STATUSES = ['active', 'completed', 'abandoned'] as const;
export type RitualStatus = (typeof RITUAL_STATUSES)[number];

export const FLOWER_STATUSES = ['available', 'planting', 'planted', 'discarded'] as const;
export type FlowerStatus = (typeof FLOWER_STATUSES)[number];

export const OCCUPANCY_STATES = ['reserved', 'planted'] as const;
export type OccupancyState = (typeof OCCUPANCY_STATES)[number];

export const DRAFT_DISPOSITIONS = ['require_empty', 'discard'] as const;
export type DraftDisposition = (typeof DRAFT_DISPOSITIONS)[number];

export const ERROR_CODES = [
  'VALIDATION_ERROR',
  'UNSENT_DRAFT',
  'ACTIVE_CONVERSATION_EXISTS',
  'TURN_UNRESOLVED',
  'VERSION_CONFLICT',
  'IDEMPOTENCY_KEY_REUSED',
  'ALREADY_SELECTED',
  'CONTEXT_LIMIT',
  'PROVIDER_NOT_CONFIGURED',
  'MODEL_UNAVAILABLE',
  'RATE_LIMITED',
  'INVALID_TRANSITION',
  'ACTION_IN_PROGRESS',
  'CELL_OCCUPIED',
  'GARDEN_BUSY',
  'GARDEN_SHRINK_CONFLICT',
  'CONTENT_PURGED',
  'EVENT_CURSOR_EXPIRED',
  'NOT_FOUND',
  'ORIGIN_FORBIDDEN',
  'INTERNAL_ERROR',
] as const;
export type ErrorCode = (typeof ERROR_CODES)[number];

export interface ApiMeta {
  request_id: string;
  server_now: string;
}

export interface ApiErrorPayload {
  code: ErrorCode;
  message: string;
  retryable: boolean;
  details?: Record<string, unknown>;
}

export interface Appearance {
  schema_version: string;
  face_preset: string;
  hair_style: string;
  hair_color: string;
  skin_tone: string;
  eye_style: string;
  acne_level: string;
  freckles_level: string;
  wrinkles_level: string;
  clothing_color: string;
  asset_catalog_version: string;
}

export interface Character {
  id: string;
  kind: ConversationKind;
  name: string;
  age: number;
  gender: 'female' | 'male' | 'custom' | 'unspecified';
  gender_custom?: string;
  appearance: Appearance;
  appearance_version: string;
  version: string;
  archived_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CharacterSnapshot {
  id: string;
  kind: ConversationKind;
  name: string;
  age: number;
  gender: 'female' | 'male' | 'custom' | 'unspecified';
  gender_custom?: string;
  appearance: Appearance;
  appearance_version: string;
}

export interface AvatarCatalog {
  schema_version: string;
  catalog_version: string;
  face_presets: Array<{
    id: string;
    label: string;
    description: string;
    recommended_for: ConversationKind[];
  }>;
  hair_styles: Array<{ id: string; label: string }>;
  hair_palette: string[];
  skin_tones: Array<{
    id: string;
    label: string;
    base_hex: string;
    shadow_hex: string;
    highlight_hex: string;
  }>;
  eye_styles: Array<{ id: string; label: string }>;
  feature_levels: Array<{ id: string; label: string }>;
  clothing_palette: string[];
  expressions: Array<{ id: string; label: string }>;
  greetings: Record<
    ConversationKind,
    {
      default_key: string;
      options: Array<{ key: string; text: string }>;
    }
  >;
}

export interface SlotSummary {
  conversation_id: string;
  kind: ConversationKind;
  stage: ConversationStage;
  ui_status: string;
  ui_status_label: string;
  character: CharacterSnapshot;
  reply_policy: ReplyPolicy;
  generation_mode: GenerationMode;
  active_turn_id?: string;
  active_ritual_id?: string;
  flower_id?: string;
  version: string;
  updated_at: string;
}

export interface ActiveSlots {
  release: SlotSummary | null;
  grow: SlotSummary | null;
}

export interface MessageDTO {
  id: string;
  turn_id?: string;
  seq: number;
  role: 'user' | 'assistant';
  origin: 'opening' | 'user' | 'ai_selected';
  content: string;
  selected_run_id?: string;
  created_at: string;
}

export interface DraftDTO {
  text: string;
  draft_version: string;
  content_epoch: string;
  updated_at: string;
}

export interface UsageDTO {
  input_tokens?: number;
  public_output_tokens?: number;
  output_tokens?: number;
  reasoning_tokens?: number;
  cached_input_tokens?: number;
  cache_write_tokens?: number;
  total_tokens?: number;
  status: UsageStatus;
}

export interface RunDTO {
  id: string;
  turn_id: string;
  slot: RunSlot;
  display_label: string;
  provider: string;
  model_alias: string;
  requested_model: string;
  reported_model?: string;
  is_mock: boolean;
  blind_hidden: boolean;
  attempt_no: number;
  status: RunStatus;
  can_accept: boolean;
  can_retry: boolean;
  can_cancel: boolean;
  output_text?: string;
  error_code?: string;
  finish_reason?: string;
  queue_ms?: number;
  first_text_ms?: number;
  total_ms?: number;
  usage?: UsageDTO;
  usage_status: UsageStatus;
  price_version?: string;
  estimated_cost_usd?: string;
}

export interface TurnSnapshotDTO {
  id: string;
  conversation_id: string;
  turn_no: number;
  client_turn_id: string;
  mode: TurnMode;
  blind_mode: boolean;
  state: 'generating' | 'awaiting_selection' | 'failed' | 'resolved';
  selected_run_id?: string;
  resolution?: string;
  resolved_at?: string;
  version: string;
  last_event_seq: string;
  events_url: string;
  runs: RunDTO[];
  has_feedback: boolean;
}

export interface ActiveActionDTO {
  id: string;
  key: string;
  label: string;
  description: string;
  started_at: string;
  planned_end_at: string;
  duration_ms: number;
}

export interface FlowerVisualSnapshot {
  species_id: string;
  species_name: string;
  petal_hex: string;
  center_hex: string;
  stem_hex: string;
  style_seed: string;
}

export interface FlowerSummaryDTO {
  id: string;
  species_id: string;
  species_name: string;
  style_seed: string;
  visual_snapshot: FlowerVisualSnapshot;
  status: FlowerStatus;
  kept_line: string;
  pending_row?: number;
  pending_col?: number;
  planted_row?: number;
  planted_col?: number;
  planted_at?: string;
  version: string;
}

export interface RitualView {
  id: string;
  conversation_id: string;
  kind: ConversationKind;
  definition_version: string;
  route: RitualRoute;
  route_label: string;
  cursor: number;
  total_actions: number;
  visual_stage: number;
  status: RitualStatus;
  version: string;
  current_step_label: string;
  current_description: string;
  next_action_key?: string;
  next_action_label?: string;
  active_action?: ActiveActionDTO;
  allowed_commands: string[];
  flower?: FlowerSummaryDTO;
}

export interface ConversationDetail {
  id: string;
  kind: ConversationKind;
  stage: ConversationStage;
  character: CharacterSnapshot;
  expression_state: 'challenging' | 'apologetic' | 'listening';
  greeting_key: string;
  reply_policy: ReplyPolicy;
  generation_mode: GenerationMode;
  model_aliases: string[];
  version: string;
  content_epoch: string;
  messages: MessageDTO[];
  draft: DraftDTO;
  latest_turn?: TurnSnapshotDTO;
  active_ritual?: RitualView;
  flower_gift?: FlowerSummaryDTO;
  created_at: string;
  updated_at: string;
}

export interface ModelEntry {
  alias: string;
  provider: string;
  model_id: string;
  display_name: string;
  api_variant: string;
  price_version: string;
  enabled: boolean;
}

export interface AISettingsView {
  release_mode: GenerationMode;
  grow_mode: GenerationMode;
  model_aliases: Record<string, string>;
  blind_compare: boolean;
  detailed_eval_enabled: boolean;
  compare_notice_ack: boolean;
  reduced_motion: boolean;
  daily_budget_usd?: string;
  version: string;
  openai_key_configured: boolean;
  gemini_key_configured: boolean;
  mock_mode_enabled: boolean;
  models: ModelEntry[];
}

export interface OccupancyDTO {
  row: number;
  col: number;
  state: OccupancyState;
  flower_id: string;
  ritual_id: string;
  conversation_id: string;
  species_id: string;
  species_name: string;
  style_seed: string;
  visual_snapshot: FlowerVisualSnapshot;
  kept_line?: string;
  reserved_at: string;
  planted_at?: string;
  planted_date_local?: string;
}

export interface GardenView {
  id: string;
  cols: number;
  rows: number;
  max_cols: number;
  max_rows: number;
  version: string;
  timezone: string;
  has_reserved: boolean;
  occupancies: OccupancyDTO[];
}

export interface BootstrapResponse {
  user: {
    id: string;
    auth_mode: string;
    timezone: string;
  };
  capabilities: {
    garden_max_cols: number;
    garden_max_rows: number;
    conversation_max_turns: number;
    content_context_max_bytes: number;
    message_max_chars: number;
    kept_line_max_chars: number;
    openai_key_configured: boolean;
    gemini_key_configured: boolean;
    mock_mode_enabled: boolean;
  };
  active_conversations: ActiveSlots;
  garden_summary: {
    cols: number;
    rows: number;
    planted_count: number;
    has_reserved: boolean;
    garden_version: string;
  };
  settings: AISettingsView;
}

export interface ComparisonGroupSummary {
  group_key: string;
  prompt_version: string;
  openai_model: string;
  gemini_model: string;
  complete_comparisons: number;
  incomplete_early_selections: number;
  both_skipped: number;
  technical_failures: number;
  wins_by_provider: Record<string, number>;
}

export interface ComparisonItemDTO {
  turn_id: string;
  conversation_id: string;
  kind: ConversationKind;
  prompt_version: string;
  context_hash: string;
  resolution?: string;
  category: string;
  selected_slot?: string;
  selected_provider?: string;
  created_at: string;
  runs: RunDTO[];
  feedback_scores?: Record<string, Record<string, number>>;
}

export interface ComparisonsResponse {
  groups: ComparisonGroupSummary[];
  items: ComparisonItemDTO[];
}
