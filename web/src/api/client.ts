import {
  AISettingsView,
  ActiveSlots,
  ApiErrorPayload,
  ApiMeta,
  Appearance,
  AvatarCatalog,
  BootstrapResponse,
  Character,
  ComparisonsResponse,
  ConversationDetail,
  ConversationKind,
  DraftDTO,
  DraftDisposition,
  FlowerSummaryDTO,
  GardenView,
  GenerationMode,
  ReplyPolicy,
  RitualRoute,
  RitualView,
  TurnSnapshotDTO,
} from './types';
import { updateServerClockOffset } from './timeSync';

export class ApiClientError extends Error {
  readonly code: ApiErrorPayload['code'];
  readonly retryable: boolean;
  readonly details?: Record<string, unknown>;
  readonly status: number;

  constructor(status: number, payload: ApiErrorPayload) {
    super(payload.message);
    this.name = 'ApiClientError';
    this.status = status;
    this.code = payload.code;
    this.retryable = payload.retryable;
    this.details = payload.details;
  }
}

type PurgeListener = (conversationId: string) => void;
const purgeListeners = new Set<PurgeListener>();

let purgeChannel: BroadcastChannel | null = null;
try {
  if (typeof BroadcastChannel !== 'undefined') {
    purgeChannel = new BroadcastChannel('emotion-garden-sync');
  }
} catch {
  purgeChannel = null;
}

if (purgeChannel) {
  purgeChannel.onmessage = (ev) => {
    if (ev.data?.type === 'CONVERSATION_PURGED' && typeof ev.data.conversationId === 'string') {
      for (const fn of purgeListeners) {
        fn(ev.data.conversationId);
      }
    }
  };
}

export function onConversationPurged(listener: PurgeListener): () => void {
  purgeListeners.add(listener);
  return () => {
    purgeListeners.delete(listener);
  };
}

export function notifyConversationPurged(conversationId: string): void {
  for (const fn of purgeListeners) {
    fn(conversationId);
  }
  purgeChannel?.postMessage({ type: 'CONVERSATION_PURGED', conversationId });
}

export function createIdempotencyKey(prefix = 'idem'): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}_${crypto.randomUUID()}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
  purgedConversationHintId?: string;
}

async function apiRequest<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? 'GET';
  const headers: Record<string, string> = {
    Accept: 'application/json',
  };

  let serializedBody: string | undefined;
  if (method !== 'GET') {
    headers['Content-Type'] = 'application/json';
    if (method !== 'PUT') {
      headers['Idempotency-Key'] = opts.idempotencyKey ?? createIdempotencyKey();
    }
    serializedBody = JSON.stringify(opts.body ?? {});
  }

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: serializedBody,
    signal: opts.signal,
    credentials: 'same-origin',
  });

  const json = (await res.json().catch(() => null)) as {
    data?: T;
    error?: ApiErrorPayload;
    meta?: ApiMeta;
  } | null;

  if (json?.meta?.server_now) {
    updateServerClockOffset(json.meta.server_now);
  }

  if (!res.ok || !json || json.error) {
    const errPayload: ApiErrorPayload = json?.error ?? {
      code: 'INTERNAL_ERROR',
      message: '일시적인 네트워크 또는 서버 오류가 발생했어요.',
      retryable: true,
    };
    if (errPayload.code === 'CONTENT_PURGED' && opts.purgedConversationHintId) {
      notifyConversationPurged(opts.purgedConversationHintId);
    }
    throw new ApiClientError(res.status, errPayload);
  }

  return json.data as T;
}

export const api = {
  getBootstrap: () => apiRequest<BootstrapResponse>('/bootstrap'),
  getAvatarCatalog: () => apiRequest<AvatarCatalog>('/catalogs/avatars'),

  listCharacters: (includeArchived = false) =>
    apiRequest<{ characters: Character[] }>(
      `/characters${includeArchived ? '?include_archived=true' : ''}`
    ),

  createCharacter: (
    input: {
      kind: ConversationKind;
      name: string;
      age?: number;
      gender: 'female' | 'male' | 'custom' | 'unspecified';
      gender_custom?: string;
      appearance: Omit<Appearance, 'schema_version' | 'asset_catalog_version'> &
        Partial<Pick<Appearance, 'schema_version' | 'asset_catalog_version'>>;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<Character>('/characters', {
      method: 'POST',
      body: input,
      idempotencyKey,
    }),

  updateCharacter: (
    id: string,
    input: {
      expected_version: string;
      name?: string;
      age?: number;
      gender?: 'female' | 'male' | 'custom' | 'unspecified';
      gender_custom?: string;
      appearance?: Omit<Appearance, 'schema_version' | 'asset_catalog_version'> &
        Partial<Pick<Appearance, 'schema_version' | 'asset_catalog_version'>>;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<Character>(`/characters/${id}`, {
      method: 'PATCH',
      body: input,
      idempotencyKey,
    }),

  archiveCharacter: (id: string, expectedVersion: string, idempotencyKey?: string) =>
    apiRequest<Character>(`/characters/${id}/archive`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }),

  getActiveConversations: () => apiRequest<ActiveSlots>('/conversations/active'),

  createConversation: (
    input: {
      kind: ConversationKind;
      character_id: string;
      greeting_key?: string;
      reply_policy?: ReplyPolicy;
      generation_mode?: GenerationMode;
      model_aliases?: string[];
    },
    idempotencyKey?: string
  ) =>
    apiRequest<ConversationDetail>('/conversations', {
      method: 'POST',
      body: input,
      idempotencyKey,
    }),

  getConversation: (id: string) =>
    apiRequest<ConversationDetail>(`/conversations/${id}`, {
      purgedConversationHintId: id,
    }),

  updatePreferences: (
    id: string,
    input: {
      expected_version: string;
      reply_policy?: ReplyPolicy;
      generation_mode?: GenerationMode;
      model_aliases?: string[];
    },
    idempotencyKey?: string
  ) =>
    apiRequest<ConversationDetail>(`/conversations/${id}/preferences`, {
      method: 'PATCH',
      body: input,
      idempotencyKey,
      purgedConversationHintId: id,
    }),

  saveDraft: (
    id: string,
    input: {
      draft_version: string;
      captured_epoch: string;
      text: string;
    }
  ) =>
    apiRequest<DraftDTO>(`/conversations/${id}/draft`, {
      method: 'PUT',
      body: input,
      purgedConversationHintId: id,
    }),

  submitTurn: (
    conversationId: string,
    input: {
      client_turn_id: string;
      expected_version: string;
      text: string;
      generation_mode?: GenerationMode;
      model_aliases?: string[];
    },
    idempotencyKey?: string
  ) =>
    apiRequest<{
      turn_id: string;
      conversation_version: string;
      draft_version: string;
      turn_version: string;
      state: string;
      runs: Array<{ id: string; slot: string; is_mock: boolean }>;
      events_url?: string;
    }>(`/conversations/${conversationId}/turns`, {
      method: 'POST',
      body: input,
      idempotencyKey,
      purgedConversationHintId: conversationId,
    }),

  getTurn: (turnId: string) => apiRequest<TurnSnapshotDTO>(`/turns/${turnId}`),

  selectCandidate: (
    turnId: string,
    expectedVersion: string,
    runId: string,
    idempotencyKey?: string
  ) =>
    apiRequest<TurnSnapshotDTO>(`/turns/${turnId}/selection`, {
      method: 'POST',
      body: { expected_version: expectedVersion, run_id: runId },
      idempotencyKey,
    }),

  skipTurn: (turnId: string, expectedVersion: string, idempotencyKey?: string) =>
    apiRequest<TurnSnapshotDTO>(`/turns/${turnId}/skip`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }),

  submitFeedback: (
    turnId: string,
    scores: Record<string, Record<string, number>>,
    idempotencyKey?: string
  ) =>
    apiRequest<{ saved: boolean }>(`/turns/${turnId}/feedback`, {
      method: 'POST',
      body: { scores },
      idempotencyKey,
    }),

  cancelRun: (runId: string, expectedTurnVersion: string, idempotencyKey?: string) =>
    apiRequest<TurnSnapshotDTO>(`/runs/${runId}/cancel`, {
      method: 'POST',
      body: { expected_turn_version: expectedTurnVersion },
      idempotencyKey,
    }),

  retryRun: (runId: string, expectedTurnVersion: string, idempotencyKey?: string) =>
    apiRequest<TurnSnapshotDTO>(`/runs/${runId}/retry`, {
      method: 'POST',
      body: { expected_turn_version: expectedTurnVersion },
      idempotencyKey,
    }),

  beginClosure: (
    conversationId: string,
    input: {
      expected_version: string;
      cancel_pending?: boolean;
      draft_disposition?: DraftDisposition;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<{
      conversation_id: string;
      conversation_stage: string;
      conversation_version: string;
      ritual?: RitualView;
      flower?: FlowerSummaryDTO;
    }>(`/conversations/${conversationId}/closure`, {
      method: 'POST',
      body: input,
      idempotencyKey,
      purgedConversationHintId: conversationId,
    }),

  getRitual: (ritualId: string) => apiRequest<RitualView>(`/rituals/${ritualId}`),

  returnToChat: (ritualId: string, expectedVersion: string, idempotencyKey?: string) =>
    apiRequest<{
      conversation_id: string;
      conversation_stage: string;
      conversation_version: string;
    }>(`/rituals/${ritualId}/return-to-chat`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }),

  selectRitualRoute: (
    ritualId: string,
    expectedVersion: string,
    route: Extract<RitualRoute, 'sea' | 'incinerate' | 'bury'>,
    idempotencyKey?: string
  ) =>
    apiRequest<RitualView>(`/rituals/${ritualId}/route`, {
      method: 'POST',
      body: { expected_version: expectedVersion, route },
      idempotencyKey,
    }),

  startRitualAction: (
    ritualId: string,
    expectedVersion: string,
    actionKey: string,
    idempotencyKey?: string
  ) =>
    apiRequest<RitualView>(`/rituals/${ritualId}/actions`, {
      method: 'POST',
      body: { expected_version: expectedVersion, action_key: actionKey },
      idempotencyKey,
    }),

  skipRitualAction: (
    ritualId: string,
    actionId: string,
    expectedVersion: string,
    idempotencyKey?: string
  ) =>
    apiRequest<RitualView>(`/rituals/${ritualId}/actions/${actionId}/skip`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }),

  completeRitual: (
    ritualId: string,
    expectedVersion: string,
    conversationIdHint?: string,
    idempotencyKey?: string
  ) =>
    apiRequest<{
      conversation_id: string;
      conversation_stage: string;
      conversation_version: string;
      close_reason: string;
      purged: boolean;
      ritual_id: string;
      ritual_status: string;
      flower?: FlowerSummaryDTO;
      garden_version?: string;
    }>(`/rituals/${ritualId}/complete`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }).then((res) => {
      if (res.purged) {
        notifyConversationPurged(res.conversation_id || conversationIdHint || '');
      }
      return res;
    }),

  updateFlower: (
    flowerId: string,
    input: {
      expected_version: string;
      kept_line?: string;
      pending_row?: number;
      pending_col?: number;
      clear_pending?: boolean;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<FlowerSummaryDTO>(`/flowers/${flowerId}`, {
      method: 'PATCH',
      body: input,
      idempotencyKey,
    }),

  beginPlanting: (
    flowerId: string,
    input: {
      row: number;
      col: number;
      expected_garden_version: string;
      expected_conversation_version: string;
      expected_flower_version: string;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<{
      garden_version: string;
      conversation_id: string;
      conversation_stage: string;
      conversation_version: string;
      flower: FlowerSummaryDTO;
      ritual: RitualView;
    }>(`/flowers/${flowerId}/planting`, {
      method: 'POST',
      body: input,
      idempotencyKey,
    }),

  discardFlower: (
    flowerId: string,
    expectedVersion: string,
    conversationIdHint?: string,
    idempotencyKey?: string
  ) =>
    apiRequest<{
      conversation_id: string;
      conversation_stage: string;
      conversation_version: string;
      close_reason: string;
      purged: boolean;
      flower: FlowerSummaryDTO;
    }>(`/flowers/${flowerId}/discard`, {
      method: 'POST',
      body: { expected_version: expectedVersion },
      idempotencyKey,
    }).then((res) => {
      if (res.purged) {
        notifyConversationPurged(res.conversation_id || conversationIdHint || '');
      }
      return res;
    }),

  getGarden: () => apiRequest<GardenView>('/garden'),

  resizeGarden: (
    expectedVersion: string,
    cols: number,
    rows: number,
    idempotencyKey?: string
  ) =>
    apiRequest<GardenView>('/garden', {
      method: 'PATCH',
      body: { expected_version: expectedVersion, cols, rows },
      idempotencyKey,
    }),

  getAISettings: () => apiRequest<AISettingsView>('/settings/ai'),

  updateAISettings: (
    input: {
      expected_version: string;
      release_mode?: GenerationMode;
      grow_mode?: GenerationMode;
      model_aliases?: Record<string, string>;
      blind_compare?: boolean;
      detailed_eval_enabled?: boolean;
      compare_notice_ack?: boolean;
      reduced_motion?: boolean;
      daily_budget_usd?: string;
    },
    idempotencyKey?: string
  ) =>
    apiRequest<AISettingsView>('/settings/ai', {
      method: 'PATCH',
      body: input,
      idempotencyKey,
    }),

  listComparisons: (pageSize = 30) =>
    apiRequest<ComparisonsResponse>(`/comparisons?page_size=${pageSize}`),
};
