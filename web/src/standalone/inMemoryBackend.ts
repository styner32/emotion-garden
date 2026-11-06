import {
  AISettingsView,
  AvatarCatalog,
  BootstrapResponse,
  Character,
  ComparisonItemDTO,
  ComparisonsResponse,
  ConversationDetail,
  ConversationKind,
  ConversationStage,
  DraftDTO,
  ErrorCode,
  FlowerSummaryDTO,
  GardenView,
  GenerationMode,
  MessageDTO,
  OccupancyDTO,
  ReplyPolicy,
  RitualRoute,
  RitualView,
  RunDTO,
  SlotSummary,
  TurnSnapshotDTO,
} from '../api/types';

const AVATAR_CATALOG: AvatarCatalog = {
  schema_version: 'appearance_v1',
  catalog_version: 'v1',
  face_presets: [
    { id: 'calm_oval', label: '담담한 계란형', description: '차분하고 균형 잡힌 기본 인상', recommended_for: ['grow', 'release'] },
    { id: 'warm_round', label: '포근한 둥근형', description: '부드러운 눈썹과 편안한 턱선', recommended_for: ['grow'] },
    { id: 'sharp_stern', label: '날카롭고 까칠한 인상', description: '치켜올라간 눈썹과 단호한 턱선', recommended_for: ['release'] },
    { id: 'blunt_square', label: '무심하고 단단한 인상', description: '일자 눈썹과 각진 윤곽', recommended_for: ['release'] },
    { id: 'cynical_narrow', label: '예민하고 냉담한 인상', description: '좁은 미간과 차가운 분위기', recommended_for: ['release'] },
  ],
  hair_styles: [
    { id: 'perm', label: '파마' },
    { id: 'long_straight', label: '긴 생머리' },
    { id: 'braid', label: '땋은 머리' },
    { id: 'sport', label: '스포츠' },
    { id: 'leaf_cut', label: '리프컷' },
    { id: 'dandy_cut', label: '댄디컷' },
  ],
  hair_palette: ['#231F20', '#3E2B23', '#6B442A', '#9A6B43', '#C79A63', '#5A4A66', '#7A3B3B', '#6F767D'],
  skin_tones: [
    { id: 'tone_01', label: '톤 1 (가장 밝음)', base_hex: '#FCE7DC', shadow_hex: '#DDB8A6', highlight_hex: '#FFF5F0' },
    { id: 'tone_02', label: '톤 2 (밝음)', base_hex: '#F5D3C1', shadow_hex: '#CFA18B', highlight_hex: '#FFEBE0' },
    { id: 'tone_03', label: '톤 3 (중간 밝음)', base_hex: '#E5B99E', shadow_hex: '#BA886C', highlight_hex: '#F6D5C0' },
    { id: 'tone_04', label: '톤 4 (중간)', base_hex: '#C99372', shadow_hex: '#9B6749', highlight_hex: '#DFB093' },
    { id: 'tone_05', label: '톤 5 (깊음)', base_hex: '#9D694A', shadow_hex: '#70452C', highlight_hex: '#B88566' },
    { id: 'tone_06', label: '톤 6 (가장 깊음)', base_hex: '#6F4630', shadow_hex: '#482B1B', highlight_hex: '#8C6047' },
  ],
  eye_styles: [
    { id: 'sparkling', label: '반짝이는 눈' },
    { id: 'glaring', label: '째려보는 눈' },
  ],
  feature_levels: [
    { id: 'none', label: '없음' },
    { id: 'mild', label: '약함' },
    { id: 'distinct', label: '뚜렷함' },
  ],
  clothing_palette: ['#5E7A68', '#7D6B5D', '#4F6478', '#A46756', '#D4C6B4', '#3A3F45'],
  expressions: [
    { id: 'challenging', label: '도전적 (첫 인사)' },
    { id: 'apologetic', label: '사과하는 표정' },
    { id: 'listening', label: '경청하는 표정' },
  ],
  greetings: {
    release: {
      default_key: 'what_do_you_want',
      options: [
        { key: 'what_do_you_want', text: '나한테 할 말이 뭔데?' },
        { key: 'what_is_your_complaint', text: '뭐가 불만이야?' },
      ],
    },
    grow: {
      default_key: 'tell_me_slowly',
      options: [{ key: 'tell_me_slowly', text: '오늘 마음에 걸린 일이 있어? 천천히 말해줘.' }],
    },
  },
};

const FLOWER_SPECIES = [
  { id: 'apricot_blossom', name_ko: '살구빛 꽃', petal_hex: '#F4A988', center_hex: '#E07A5F', stem_hex: '#588157' },
  { id: 'little_daisy', name_ko: '작은 데이지', petal_hex: '#FFFDF7', center_hex: '#F2C14E', stem_hex: '#5E8C61' },
  { id: 'lavender_bloom', name_ko: '연보라 꽃', petal_hex: '#B8A4DF', center_hex: '#7D64B0', stem_hex: '#52796F' },
  { id: 'pink_petal', name_ko: '분홍 꽃', petal_hex: '#F2A6B8', center_hex: '#D66A86', stem_hex: '#588157' },
  { id: 'four_leaf_clover', name_ko: '네잎클로버', petal_hex: '#6AA86E', center_hex: '#467A4B', stem_hex: '#3E6B42' },
];

const ROUTE_STEPS: Record<RitualRoute, { routeLabel: string; steps: Array<{ key: string; label: string; desc: string; durationMs: number }> }> = {
  common: {
    routeLabel: '봉투에 담기',
    steps: [
      { key: 'pack', label: '대화를 봉투에 담고 봉인하기', desc: '털어놓은 문장들을 종이봉투 안에 차곡차곡 접어 넣고 인장으로 봉인합니다.', durationMs: 2000 },
      { key: 'lift', label: '봉투를 들어 보낼 준비하기', desc: '봉인된 봉투를 손에 가만히 들어 어디로 떠나보낼지 고릅니다.', durationMs: 1600 },
    ],
  },
  sea: {
    routeLabel: '바다로 던지기',
    steps: [
      { key: 'arrive', label: '바닷가에 서기', desc: '잔잔한 파도 소리가 들리는 수평선 앞에 멈춰 섭니다.', durationMs: 1600 },
      { key: 'throw', label: '파도 너머로 던지기', desc: '손에 쥔 봉투를 넓은 바다를 향해 힘껏 던져 보냅니다.', durationMs: 1800 },
      { key: 'float', label: '물결 따라 멀어지기', desc: '봉투가 물결을 타고 수평선 쪽으로 천천히 흘러갑니다.', durationMs: 1800 },
      { key: 'vanish', label: '수평선 너머 사라지기', desc: '시야 끝에서 봉투가 완전히 자취를 감추고 파도만 남습니다.', durationMs: 1600 },
    ],
  },
  incinerate: {
    routeLabel: '화로에 소각하기',
    steps: [
      { key: 'arrive', label: '작은 화로 앞에 서기', desc: '온기가 감도는 작은 화로 앞에 봉투를 들고 섭니다.', durationMs: 1600 },
      { key: 'drop', label: '화로 속에 넣기', desc: '봉인된 봉투를 화로 안 불씨 위로 조심스럽게 내려놓습니다.', durationMs: 1600 },
      { key: 'burn', label: '따뜻한 불꽃으로 태우기', desc: '종이가 잔잔한 불꽃에 타오르며 가벼운 불티로 흩어집니다.', durationMs: 2000 },
      { key: 'ash', label: '연기와 재로 흩어지기', desc: '옅은 연기만 하늘로 올라가고 무거운 마음도 함께 가벼워집니다.', durationMs: 1600 },
    ],
  },
  bury: {
    routeLabel: '땅에 묻기',
    steps: [
      { key: 'arrive', label: '조용한 흙터에 도착하기', desc: '부드러운 흙내음이 나는 한적한 터 앞에 섭니다.', durationMs: 1600 },
      { key: 'dig', label: '깊은 구덩이 파기', desc: '삽으로 흙을 파내어 봉투가 쉴 작은 자리를 마련합니다.', durationMs: 1800 },
      { key: 'place', label: '구덩이에 봉투 내려놓기', desc: '봉인된 봉투를 흙 구덩이 아래에 조용히 내려놓습니다.', durationMs: 1600 },
      { key: 'cover', label: '흙을 덮고 단단히 다지기', desc: '파낸 흙을 다시 덮고 토닥여 침묵 속에 재웁니다.', durationMs: 1800 },
    ],
  },
  garden: {
    routeLabel: '꽃밭에 직접 심기',
    steps: [
      { key: 'arrive', label: '선택한 꽃밭 자리 앞에 서기', desc: '꽃모종과 도구를 들고 고른 흙 칸 앞에 다가갑니다.', durationMs: 1600 },
      { key: 'dig', label: '삽으로 부드러운 흙 파기', desc: '뿌리가 편안히 자리 잡도록 흙을 알맞게 파냅니다.', durationMs: 1800 },
      { key: 'plant', label: '두 손으로 꽃모종 심기', desc: '모종을 두 손으로 감싸 파둔 구덩이에 정성껏 세웁니다.', durationMs: 1800 },
      { key: 'cover', label: '흙을 덮고 주변 토닥이기', desc: '뿌리 둘레로 흙을 부드럽게 덮고 단단히 다져 줍니다.', durationMs: 1800 },
      { key: 'water', label: '물뿌리개로 맑은 물 주기', desc: '촉촉한 물을 흠뻑 주어 꽃이 싱그럽게 피어나게 합니다.', durationMs: 2000 },
    ],
  },
};

interface InternalRitual {
  id: string;
  conversation_id: string;
  kind: ConversationKind;
  route: RitualRoute;
  cursor: number;
  status: 'active' | 'completed' | 'abandoned';
  version: number;
  activeAction?: {
    id: string;
    key: string;
    label: string;
    description: string;
    started_at: string;
    planned_end_at: string;
    duration_ms: number;
  };
  flowerId?: string;
}

interface InternalTurn {
  snap: TurnSnapshotDTO;
  events: Array<{ seq: number; event: string; payload: string }>;
  listeners: Set<() => void>;
}

let seqCounter = 100;
function nextId(prefix: string): string {
  seqCounter += 1;
  return `00000000-0000-4000-8000-${String(seqCounter).padStart(12, '0') || prefix}`;
}

function seoulDateDot(): string {
  const fmt = new Intl.DateTimeFormat('ko-KR', {
    timeZone: 'Asia/Seoul',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const parts = fmt.formatToParts(new Date());
  const y = parts.find((p) => p.type === 'year')?.value ?? '2026';
  const m = parts.find((p) => p.type === 'month')?.value ?? '01';
  const d = parts.find((p) => p.type === 'day')?.value ?? '01';
  return `${y}. ${m}. ${d}`;
}

function buildMockReply(kind: ConversationKind, charName: string, userText: string, variant: 'a' | 'b'): string {
  const snippet = Array.from(userText.trim()).slice(0, 22).join('');
  if (kind === 'release') {
    if (variant === 'a') {
      return `“${snippet}${userText.length > 22 ? '…' : ''}”라고 말해줘서 이제야 네가 얼마나 답답하고 서운했을지 제대로 알겠어. 변명하지 않을게. 내가 네 마음을 세심하게 살피지 못해서 정말 미안해.`;
    }
    return `그때 내 말과 행동 때문에 네가 혼자 속앓이를 많이 했구나. “${snippet}${userText.length > 22 ? '…' : ''}”라는 말을 들으니 내 잘못이 분명해. 진심으로 사과할게, 정말 미안했어.`;
  }
  if (variant === 'a') {
    return `“${snippet}${userText.length > 22 ? '…' : ''}” 일로 마음이 꽤 무거웠겠다. ${charName}에게 이렇게 꺼내 놓은 것만으로도 이미 큰 걸음을 뗀 거야. 지금 가장 마음에 걸리는 장면을 조금만 더 들려줄래?`;
  }
  return `혼자 그 생각을 붙잡고 있으면서 얼마나 지쳤을지 느껴져. 서둘러 결론을 내리지 않아도 괜찮으니, 오늘 네 마음이 제일 바라는 작은 쉼이 무엇인지 같이 천천히 살펴보자.`;
}

export function installStandaloneBackend(): void {
  const nowIso = () => new Date().toISOString();

  const characters = new Map<string, Character>();
  const starterRelease: Character = {
    id: nextId('char'),
    kind: 'release',
    name: '박 팀장',
    age: 38,
    gender: 'male',
    appearance: {
      schema_version: 'appearance_v1',
      asset_catalog_version: 'v1',
      face_preset: 'sharp_stern',
      hair_style: 'dandy_cut',
      hair_color: '#231F20',
      skin_tone: 'tone_03',
      eye_style: 'glaring',
      acne_level: 'none',
      freckles_level: 'none',
      wrinkles_level: 'mild',
      clothing_color: '#4F6478',
    },
    appearance_version: '1',
    version: '1',
    created_at: nowIso(),
    updated_at: nowIso(),
  };
  const starterGrow: Character = {
    id: nextId('char'),
    kind: 'grow',
    name: '숲지기 윤',
    age: 31,
    gender: 'female',
    appearance: {
      schema_version: 'appearance_v1',
      asset_catalog_version: 'v1',
      face_preset: 'warm_round',
      hair_style: 'braid',
      hair_color: '#6B442A',
      skin_tone: 'tone_02',
      eye_style: 'sparkling',
      acne_level: 'none',
      freckles_level: 'mild',
      wrinkles_level: 'none',
      clothing_color: '#5E7A68',
    },
    appearance_version: '1',
    version: '1',
    created_at: nowIso(),
    updated_at: nowIso(),
  };
  characters.set(starterRelease.id, starterRelease);
  characters.set(starterGrow.id, starterGrow);

  let settings: AISettingsView = {
    release_mode: 'openai',
    grow_mode: 'openai',
    model_aliases: { openai: 'openai_fast', gemini: 'gemini_fast' },
    blind_compare: true,
    detailed_eval_enabled: false,
    compare_notice_ack: false,
    reduced_motion: false,
    version: '1',
    openai_key_configured: false,
    gemini_key_configured: false,
    mock_mode_enabled: true,
    models: [
      { alias: 'openai_fast', provider: 'openai', model_id: 'gpt-5.4-mini', display_name: 'GPT-5.4 Mini', api_variant: 'responses', price_version: '2026-03', enabled: true },
      { alias: 'gemini_fast', provider: 'gemini', model_id: 'gemini-2.5-flash', display_name: 'Gemini 2.5 Flash', api_variant: 'interactions', price_version: '2026-03', enabled: true },
    ],
  };

  let garden: GardenView = {
    id: nextId('garden'),
    cols: 4,
    rows: 3,
    max_cols: 6,
    max_rows: 8,
    version: '1',
    timezone: 'Asia/Seoul',
    has_reserved: false,
    occupancies: [],
  };

  const conversations = new Map<string, ConversationDetail>();
  const purgedIds = new Set<string>();
  const turns = new Map<string, InternalTurn>();
  const rituals = new Map<string, InternalRitual>();
  const flowers = new Map<string, FlowerSummaryDTO>();
  const comparisonItems: ComparisonItemDTO[] = [];

  function settleRitualIfElapsed(r: InternalRitual): void {
    if (r.activeAction && Date.now() >= Date.parse(r.activeAction.planned_end_at)) {
      r.cursor += 1;
      r.activeAction = undefined;
      r.version += 1;
    }
  }

  function startStepOnRitual(r: InternalRitual, actionKey: string): void {
    const def = ROUTE_STEPS[r.route];
    const step = def.steps[r.cursor];
    if (!step || step.key !== actionKey) return;
    const startMs = Date.now();
    r.activeAction = {
      id: nextId('act'),
      key: step.key,
      label: step.label,
      description: step.desc,
      started_at: new Date(startMs).toISOString(),
      planned_end_at: new Date(startMs + step.durationMs).toISOString(),
      duration_ms: step.durationMs,
    };
    r.version += 1;
  }

  function toRitualView(r: InternalRitual): RitualView {
    settleRitualIfElapsed(r);
    const def = ROUTE_STEPS[r.route];
    const total = def.steps.length;
    const cmds: string[] = [];

    if (r.status === 'active') {
      if (r.activeAction) {
        cmds.push('skip_current_action');
      } else if (r.cursor < total) {
        cmds.push('start_next_action');
      }
      if (r.route === 'common' && !r.activeAction) {
        cmds.push('return_to_chat');
        if (r.cursor >= total) {
          cmds.push('select_route:sea', 'select_route:incinerate', 'select_route:bury');
        }
      }
      if (r.route !== 'common' && !r.activeAction && r.cursor >= total) {
        cmds.push('complete_ritual');
      }
    }

    const curStep = r.activeAction
      ? { label: r.activeAction.label, desc: r.activeAction.description }
      : r.cursor < total
        ? { label: def.steps[r.cursor].label, desc: def.steps[r.cursor].desc }
        : r.route === 'common'
          ? { label: '떠나보낼 경로 선택하기', desc: '봉인된 봉투를 바다, 화로, 땅 중 어디로 보낼지 선택해 주세요.' }
          : { label: '모든 준비가 끝났어요', desc: '마무리를 확정하면 대화 원문이 즉시 정리됩니다.' };

    const nextStep = !r.activeAction && r.cursor < total ? def.steps[r.cursor] : undefined;

    return {
      id: r.id,
      conversation_id: r.conversation_id,
      kind: r.kind,
      definition_version: 'v1',
      route: r.route,
      route_label: def.routeLabel,
      cursor: r.activeAction ? r.cursor + 1 : r.cursor,
      total_actions: total,
      visual_stage: r.cursor,
      status: r.status,
      version: String(r.version),
      current_step_label: curStep.label,
      current_description: curStep.desc,
      next_action_key: nextStep?.key,
      next_action_label: nextStep?.label,
      active_action: r.activeAction,
      allowed_commands: cmds,
      flower: r.flowerId ? flowers.get(r.flowerId) : undefined,
    };
  }

  function activeSlotFor(kind: ConversationKind): SlotSummary | null {
    for (const c of conversations.values()) {
      if (c.kind === kind && c.stage !== 'completed' && !purgedIds.has(c.id)) {
        const labelMap: Record<ConversationStage, string> = {
          chat: '대화 진행 중',
          release_ritual: '봉투 마무리 중',
          gift_ready: '꽃 선물 도착',
          planting: '꽃 심는 중',
          completed: '완료됨',
        };
        return {
          conversation_id: c.id,
          kind: c.kind,
          stage: c.stage,
          ui_status: c.stage,
          ui_status_label: labelMap[c.stage],
          character: c.character,
          reply_policy: c.reply_policy,
          generation_mode: c.generation_mode,
          active_turn_id: c.latest_turn?.id,
          active_ritual_id: c.active_ritual?.id,
          flower_id: c.flower_gift?.id,
          version: c.version,
          updated_at: c.updated_at,
        };
      }
    }
    return null;
  }

  function purgeConversationNow(convId: string): void {
    const conv = conversations.get(convId);
    if (conv) {
      conv.messages = [];
      conv.draft.text = '';
      conv.stage = 'completed';
      conversations.delete(convId);
    }
    purgedIds.add(convId);
  }

  function jsonResponse(status: number, data: unknown): Response {
    return new Response(
      JSON.stringify({
        data,
        meta: { request_id: nextId('req'), server_now: nowIso() },
      }),
      { status, headers: { 'Content-Type': 'application/json; charset=utf-8' } }
    );
  }

  function errResponse(status: number, code: ErrorCode, message: string, details?: Record<string, unknown>): Response {
    return new Response(
      JSON.stringify({
        error: { code, message, retryable: false, details },
        meta: { request_id: nextId('req'), server_now: nowIso() },
      }),
      { status, headers: { 'Content-Type': 'application/json; charset=utf-8' } }
    );
  }

  const origFetch = window.fetch.bind(window);

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const parsed = new URL(rawUrl, 'http://localhost');
    if (!parsed.pathname.startsWith('/api/v1')) {
      return origFetch(input, init);
    }

    const method = (init?.method ?? 'GET').toUpperCase();
    const path = parsed.pathname.slice('/api/v1'.length);
    const body = init?.body && typeof init.body === 'string' ? JSON.parse(init.body) : {};

    if (method === 'GET' && path === '/bootstrap') {
      const resp: BootstrapResponse = {
        user: { id: 'user-standalone', auth_mode: 'single_user', timezone: 'Asia/Seoul' },
        capabilities: {
          garden_max_cols: 6,
          garden_max_rows: 8,
          conversation_max_turns: 100,
          content_context_max_bytes: 131072,
          message_max_chars: 4000,
          kept_line_max_chars: 80,
          openai_key_configured: false,
          gemini_key_configured: false,
          mock_mode_enabled: true,
        },
        active_conversations: {
          release: activeSlotFor('release'),
          grow: activeSlotFor('grow'),
        },
        garden_summary: {
          cols: garden.cols,
          rows: garden.rows,
          planted_count: garden.occupancies.filter((o) => o.state === 'planted').length,
          has_reserved: garden.has_reserved,
          garden_version: garden.version,
        },
        settings,
      };
      return jsonResponse(200, resp);
    }

    if (method === 'GET' && path === '/catalogs/avatars') {
      return jsonResponse(200, AVATAR_CATALOG);
    }

    if (method === 'GET' && path === '/characters') {
      const incArch = parsed.searchParams.get('include_archived') === 'true';
      const list = Array.from(characters.values()).filter((c) => incArch || !c.archived_at);
      return jsonResponse(200, { characters: list });
    }

    if (method === 'POST' && path === '/characters') {
      const ch: Character = {
        id: nextId('char'),
        kind: body.kind,
        name: String(body.name ?? '').trim(),
        age: Number(body.age ?? 29),
        gender: body.gender ?? 'unspecified',
        gender_custom: body.gender_custom,
        appearance: {
          ...body.appearance,
          schema_version: 'appearance_v1',
          asset_catalog_version: 'v1',
        },
        appearance_version: '1',
        version: '1',
        created_at: nowIso(),
        updated_at: nowIso(),
      };
      characters.set(ch.id, ch);
      return jsonResponse(201, ch);
    }

    const charPatchMatch = path.match(/^\/characters\/([^/]+)$/);
    if (method === 'PATCH' && charPatchMatch) {
      const ch = characters.get(charPatchMatch[1]);
      if (!ch) return errResponse(404, 'NOT_FOUND', '인물을 찾을 수 없어요.');
      const updated: Character = {
        ...ch,
        name: body.name !== undefined ? String(body.name).trim() : ch.name,
        age: body.age !== undefined ? Number(body.age) : ch.age,
        gender: body.gender ?? ch.gender,
        gender_custom: body.gender_custom ?? ch.gender_custom,
        appearance: body.appearance
          ? { ...body.appearance, schema_version: 'appearance_v1', asset_catalog_version: 'v1' }
          : ch.appearance,
        version: String(Number(ch.version) + 1),
        updated_at: nowIso(),
      };
      characters.set(updated.id, updated);
      return jsonResponse(200, updated);
    }

    const charArchMatch = path.match(/^\/characters\/([^/]+)\/archive$/);
    if (method === 'POST' && charArchMatch) {
      const ch = characters.get(charArchMatch[1]);
      if (!ch) return errResponse(404, 'NOT_FOUND', '인물을 찾을 수 없어요.');
      ch.archived_at = nowIso();
      ch.version = String(Number(ch.version) + 1);
      return jsonResponse(200, ch);
    }

    if (method === 'GET' && path === '/conversations/active') {
      return jsonResponse(200, { release: activeSlotFor('release'), grow: activeSlotFor('grow') });
    }

    if (method === 'POST' && path === '/conversations') {
      const kind: ConversationKind = body.kind;
      const existing = activeSlotFor(kind);
      if (existing) {
        return errResponse(409, 'ACTIVE_CONVERSATION_EXISTS', '이미 진행 중인 대화가 있어요.', {
          conversation_id: existing.conversation_id,
        });
      }
      const ch = characters.get(body.character_id);
      if (!ch) return errResponse(404, 'NOT_FOUND', '대화 상대를 찾을 수 없어요.');

      const gKey = body.greeting_key || AVATAR_CATALOG.greetings[kind].default_key;
      const gOpt = AVATAR_CATALOG.greetings[kind].options.find((o) => o.key === gKey) ?? AVATAR_CATALOG.greetings[kind].options[0];
      const convId = nextId('conv');
      const opening: MessageDTO = {
        id: nextId('msg'),
        seq: 1,
        role: 'assistant',
        origin: 'opening',
        content: gOpt.text,
        created_at: nowIso(),
      };
      const detail: ConversationDetail = {
        id: convId,
        kind,
        stage: 'chat',
        character: {
          id: ch.id,
          kind: ch.kind,
          name: ch.name,
          age: ch.age,
          gender: ch.gender,
          gender_custom: ch.gender_custom,
          appearance: { ...ch.appearance },
          appearance_version: ch.appearance_version,
        },
        expression_state: kind === 'release' ? 'challenging' : 'listening',
        greeting_key: gKey,
        reply_policy: body.reply_policy ?? (kind === 'release' ? 'apology' : 'supportive'),
        generation_mode: body.generation_mode ?? (kind === 'release' ? settings.release_mode : settings.grow_mode),
        model_aliases: ['openai_fast'],
        version: '1',
        content_epoch: '1',
        messages: [opening],
        draft: { text: '', draft_version: '1', content_epoch: '1', updated_at: nowIso() },
        created_at: nowIso(),
        updated_at: nowIso(),
      };
      conversations.set(convId, detail);
      return jsonResponse(201, detail);
    }

    const convGetMatch = path.match(/^\/conversations\/([^/]+)$/);
    if (method === 'GET' && convGetMatch) {
      const id = convGetMatch[1];
      if (purgedIds.has(id)) {
        return errResponse(410, 'CONTENT_PURGED', '이미 마무리되어 원문이 정리된 대화예요.');
      }
      const c = conversations.get(id);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');
      if (c.active_ritual) {
        const ir = rituals.get(c.active_ritual.id);
        if (ir) c.active_ritual = toRitualView(ir);
      }
      if (c.flower_gift) {
        c.flower_gift = flowers.get(c.flower_gift.id) ?? c.flower_gift;
      }
      return jsonResponse(200, JSON.parse(JSON.stringify(c)));
    }

    const convPrefMatch = path.match(/^\/conversations\/([^/]+)\/preferences$/);
    if (method === 'PATCH' && convPrefMatch) {
      const c = conversations.get(convPrefMatch[1]);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');
      if (body.reply_policy) c.reply_policy = body.reply_policy;
      if (body.generation_mode) c.generation_mode = body.generation_mode;
      c.version = String(Number(c.version) + 1);
      c.updated_at = nowIso();
      return jsonResponse(200, JSON.parse(JSON.stringify(c)));
    }

    const convDraftMatch = path.match(/^\/conversations\/([^/]+)\/draft$/);
    if (method === 'PUT' && convDraftMatch) {
      const id = convDraftMatch[1];
      if (purgedIds.has(id)) return errResponse(410, 'CONTENT_PURGED', '정리된 대화예요.');
      const c = conversations.get(id);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');
      const nextDraft: DraftDTO = {
        text: String(body.text ?? ''),
        draft_version: String(Number(c.draft.draft_version) + 1),
        content_epoch: c.content_epoch,
        updated_at: nowIso(),
      };
      c.draft = nextDraft;
      return jsonResponse(200, nextDraft);
    }

    const convTurnsMatch = path.match(/^\/conversations\/([^/]+)\/turns$/);
    if (method === 'POST' && convTurnsMatch) {
      const c = conversations.get(convTurnsMatch[1]);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');

      const userMsg: MessageDTO = {
        id: nextId('msg'),
        seq: c.messages.length + 1,
        role: 'user',
        origin: 'user',
        content: String(body.text ?? ''),
        created_at: nowIso(),
      };
      c.messages.push(userMsg);
      c.draft = {
        text: '',
        draft_version: String(Number(c.draft.draft_version) + 1),
        content_epoch: c.content_epoch,
        updated_at: nowIso(),
      };
      c.version = String(Number(c.version) + 1);

      const turnId = nextId('turn');
      if (c.kind === 'release' && c.reply_policy === 'quiet' && !body.generation_mode) {
        const quietSnap: TurnSnapshotDTO = {
          id: turnId,
          conversation_id: c.id,
          turn_no: c.messages.length,
          client_turn_id: body.client_turn_id,
          mode: 'quiet',
          blind_mode: false,
          state: 'resolved',
          resolution: 'quiet_no_reply',
          resolved_at: nowIso(),
          version: '1',
          last_event_seq: '0',
          events_url: '',
          runs: [],
          has_feedback: false,
        };
        c.latest_turn = quietSnap;
        return jsonResponse(201, {
          turn_id: turnId,
          conversation_version: c.version,
          draft_version: c.draft.draft_version,
          turn_version: '1',
          state: 'resolved',
          runs: [],
        });
      }

      const mode: GenerationMode = body.generation_mode ?? c.generation_mode;
      const isCompare = mode === 'compare';
      const blind = isCompare && settings.blind_compare;

      const makeRun = (slot: 'primary' | 'a' | 'b', provider: string, modelId: string): RunDTO => ({
        id: nextId('run'),
        turn_id: turnId,
        slot,
        display_label: slot === 'primary' ? c.character.name : `후보 ${slot.toUpperCase()}`,
        provider,
        model_alias: provider === 'openai' ? 'openai_fast' : 'gemini_fast',
        requested_model: modelId,
        is_mock: true,
        blind_hidden: blind,
        attempt_no: 1,
        status: 'streaming',
        can_accept: false,
        can_retry: false,
        can_cancel: true,
        output_text: '',
        usage_status: 'known',
      });

      const runList: RunDTO[] = isCompare
        ? [makeRun('a', 'mock_openai', 'gpt-5.4-mini'), makeRun('b', 'mock_gemini', 'gemini-2.5-flash')]
        : [
            makeRun(
              'primary',
              mode === 'gemini' ? 'mock_gemini' : 'mock_openai',
              mode === 'gemini' ? 'gemini-2.5-flash' : 'gpt-5.4-mini'
            ),
          ];

      const snap: TurnSnapshotDTO = {
        id: turnId,
        conversation_id: c.id,
        turn_no: c.messages.length,
        client_turn_id: body.client_turn_id,
        mode,
        blind_mode: blind,
        state: 'generating',
        version: '1',
        last_event_seq: '0',
        events_url: `/api/v1/turns/${turnId}/events`,
        runs: runList,
        has_feedback: false,
      };
      const internalTurn: InternalTurn = { snap, events: [], listeners: new Set() };
      turns.set(turnId, internalTurn);
      c.latest_turn = snap;

      const pushEv = (evName: string, payloadObj: unknown) => {
        const seq = internalTurn.events.length + 1;
        internalTurn.events.push({ seq, event: evName, payload: JSON.stringify(payloadObj) });
        snap.last_event_seq = String(seq);
        for (const fn of internalTurn.listeners) fn();
      };

      // Stream Korean tokens smoothly
      runList.forEach((r, idx) => {
        const fullText = buildMockReply(c.kind, c.character.name, userMsg.content, idx === 0 ? 'a' : 'b');
        const words = fullText.match(/.{1,6}/g) ?? [fullText];
        let wIdx = 0;
        const timer = window.setInterval(() => {
          if (r.status === 'cancelled' || purgedIds.has(c.id)) {
            window.clearInterval(timer);
            return;
          }
          if (wIdx < words.length) {
            const chunk = words[wIdx++];
            r.output_text = (r.output_text ?? '') + chunk;
            pushEv('run.delta', { run_id: r.id, slot: r.slot, text: chunk });
          } else {
            window.clearInterval(timer);
            r.status = 'succeeded';
            r.can_cancel = false;
            r.can_accept = isCompare;
            r.first_text_ms = 55;
            r.total_ms = words.length * 38;
            r.usage = { input_tokens: 142, public_output_tokens: 68, output_tokens: 68, status: 'known' };
            pushEv('run.completed', { run_id: r.id, slot: r.slot, status: 'succeeded' });

            if (!isCompare) {
              c.messages.push({
                id: nextId('msg'),
                turn_id: turnId,
                seq: c.messages.length + 1,
                role: 'assistant',
                origin: 'ai_selected',
                content: r.output_text ?? '',
                selected_run_id: r.id,
                created_at: nowIso(),
              });
              if (c.kind === 'release') c.expression_state = 'apologetic';
              snap.state = 'resolved';
              snap.selected_run_id = r.id;
              snap.resolution = 'auto_accepted';
              snap.resolved_at = nowIso();
              snap.version = String(Number(snap.version) + 1);
              c.version = String(Number(c.version) + 1);
              pushEv('turn.resolved', { turn_id: turnId, resolution: 'auto_accepted', selected_run_id: r.id });
            } else {
              snap.state = 'awaiting_selection';
              snap.version = String(Number(snap.version) + 1);
              pushEv('turn.selection_required', { turn_id: turnId });
            }
          }
        }, 38 + idx * 12);
      });

      return jsonResponse(202, {
        turn_id: turnId,
        conversation_version: c.version,
        draft_version: c.draft.draft_version,
        turn_version: snap.version,
        state: 'generating',
        runs: runList.map((r) => ({ id: r.id, slot: r.slot, is_mock: true })),
        events_url: snap.events_url,
      });
    }

    const turnEventsMatch = path.match(/^\/turns\/([^/]+)\/events$/);
    if (method === 'GET' && turnEventsMatch) {
      const it = turns.get(turnEventsMatch[1]);
      if (!it) return errResponse(404, 'NOT_FOUND', '턴을 찾을 수 없어요.');
      const afterSeq = Number(parsed.searchParams.get('after_seq') ?? '0');
      const encoder = new TextEncoder();

      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          let sentSeq = afterSeq;
          const flushNew = () => {
            while (sentSeq < it.events.length) {
              const ev = it.events[sentSeq];
              sentSeq = ev.seq;
              controller.enqueue(encoder.encode(`id: ${ev.seq}\nevent: ${ev.event}\ndata: ${ev.payload}\n\n`));
              if (ev.event === 'turn.resolved') {
                it.listeners.delete(flushNew);
                controller.close();
                return;
              }
            }
          };
          it.listeners.add(flushNew);
          flushNew();
        },
      });
      return new Response(stream, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream; charset=utf-8' },
      });
    }

    const turnGetMatch = path.match(/^\/turns\/([^/]+)$/);
    if (method === 'GET' && turnGetMatch) {
      const it = turns.get(turnGetMatch[1]);
      if (!it) return errResponse(404, 'NOT_FOUND', '턴을 찾을 수 없어요.');
      return jsonResponse(200, it.snap);
    }

    const turnSelMatch = path.match(/^\/turns\/([^/]+)\/selection$/);
    if (method === 'POST' && turnSelMatch) {
      const it = turns.get(turnSelMatch[1]);
      if (!it) return errResponse(404, 'NOT_FOUND', '턴을 찾을 수 없어요.');
      const c = conversations.get(it.snap.conversation_id);
      const chosen = it.snap.runs.find((r) => r.id === body.run_id);
      if (!c || !chosen) return errResponse(404, 'NOT_FOUND', '후보를 찾을 수 없어요.');

      c.messages.push({
        id: nextId('msg'),
        turn_id: it.snap.id,
        seq: c.messages.length + 1,
        role: 'assistant',
        origin: 'ai_selected',
        content: chosen.output_text ?? '',
        selected_run_id: chosen.id,
        created_at: nowIso(),
      });
      if (c.kind === 'release') c.expression_state = 'apologetic';
      for (const r of it.snap.runs) {
        r.can_accept = false;
        r.can_cancel = false;
        r.blind_hidden = false;
      }
      it.snap.state = 'resolved';
      it.snap.selected_run_id = chosen.id;
      it.snap.resolution = 'selected';
      it.snap.resolved_at = nowIso();
      it.snap.version = String(Number(it.snap.version) + 1);
      c.version = String(Number(c.version) + 1);

      comparisonItems.unshift({
        turn_id: it.snap.id,
        conversation_id: c.id,
        kind: c.kind,
        prompt_version: 'prompt_v1',
        context_hash: 'e3b0c44298fc1c14',
        resolution: 'selected',
        category: 'complete_comparison',
        selected_slot: chosen.slot,
        selected_provider: chosen.provider,
        created_at: nowIso(),
        runs: it.snap.runs.map((r) => ({ ...r, output_text: undefined })),
      });
      return jsonResponse(200, it.snap);
    }

    const turnSkipMatch = path.match(/^\/turns\/([^/]+)\/skip$/);
    if (method === 'POST' && turnSkipMatch) {
      const it = turns.get(turnSkipMatch[1]);
      if (!it) return errResponse(404, 'NOT_FOUND', '턴을 찾을 수 없어요.');
      for (const r of it.snap.runs) {
        if (r.status === 'streaming' || r.status === 'running') r.status = 'cancelled';
        r.can_accept = false;
        r.can_cancel = false;
      }
      it.snap.state = 'resolved';
      it.snap.resolution = 'skipped';
      it.snap.resolved_at = nowIso();
      it.snap.version = String(Number(it.snap.version) + 1);
      return jsonResponse(200, it.snap);
    }

    const turnFbMatch = path.match(/^\/turns\/([^/]+)\/feedback$/);
    if (method === 'POST' && turnFbMatch) {
      const it = turns.get(turnFbMatch[1]);
      if (it) it.snap.has_feedback = true;
      return jsonResponse(200, { saved: true });
    }

    const closureMatch = path.match(/^\/conversations\/([^/]+)\/closure$/);
    if (method === 'POST' && closureMatch) {
      const c = conversations.get(closureMatch[1]);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');
      c.draft.text = '';
      c.version = String(Number(c.version) + 1);

      if (c.kind === 'release') {
        const rit: InternalRitual = {
          id: nextId('rit'),
          conversation_id: c.id,
          kind: 'release',
          route: 'common',
          cursor: 0,
          status: 'active',
          version: 1,
        };
        startStepOnRitual(rit, 'pack');
        rituals.set(rit.id, rit);
        c.stage = 'release_ritual';
        c.active_ritual = toRitualView(rit);
        return jsonResponse(200, {
          conversation_id: c.id,
          conversation_stage: c.stage,
          conversation_version: c.version,
          ritual: c.active_ritual,
        });
      }

      const sp = FLOWER_SPECIES[Math.floor(Math.random() * FLOWER_SPECIES.length)];
      const seed = String(Math.floor(100000 + Math.random() * 900000));
      const flw: FlowerSummaryDTO = {
        id: nextId('flw'),
        species_id: sp.id,
        species_name: sp.name_ko,
        style_seed: seed,
        visual_snapshot: {
          species_id: sp.id,
          species_name: sp.name_ko,
          petal_hex: sp.petal_hex,
          center_hex: sp.center_hex,
          stem_hex: sp.stem_hex,
          style_seed: seed,
        },
        status: 'available',
        kept_line: '',
        version: '1',
      };
      flowers.set(flw.id, flw);
      c.stage = 'gift_ready';
      c.flower_gift = flw;
      return jsonResponse(200, {
        conversation_id: c.id,
        conversation_stage: c.stage,
        conversation_version: c.version,
        flower: flw,
      });
    }

    const ritGetMatch = path.match(/^\/rituals\/([^/]+)$/);
    if (method === 'GET' && ritGetMatch) {
      const r = rituals.get(ritGetMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      return jsonResponse(200, toRitualView(r));
    }

    const ritReturnMatch = path.match(/^\/rituals\/([^/]+)\/return-to-chat$/);
    if (method === 'POST' && ritReturnMatch) {
      const r = rituals.get(ritReturnMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      const c = conversations.get(r.conversation_id);
      if (!c) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');
      r.status = 'abandoned';
      c.stage = 'chat';
      c.active_ritual = undefined;
      c.version = String(Number(c.version) + 1);
      return jsonResponse(200, {
        conversation_id: c.id,
        conversation_stage: 'chat',
        conversation_version: c.version,
      });
    }

    const ritRouteMatch = path.match(/^\/rituals\/([^/]+)\/route$/);
    if (method === 'POST' && ritRouteMatch) {
      const r = rituals.get(ritRouteMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      r.route = body.route;
      r.cursor = 0;
      r.activeAction = undefined;
      startStepOnRitual(r, 'arrive');
      return jsonResponse(200, toRitualView(r));
    }

    const ritActMatch = path.match(/^\/rituals\/([^/]+)\/actions$/);
    if (method === 'POST' && ritActMatch) {
      const r = rituals.get(ritActMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      settleRitualIfElapsed(r);
      startStepOnRitual(r, body.action_key);
      return jsonResponse(200, toRitualView(r));
    }

    const ritSkipMatch = path.match(/^\/rituals\/([^/]+)\/actions\/([^/]+)\/skip$/);
    if (method === 'POST' && ritSkipMatch) {
      const r = rituals.get(ritSkipMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      if (r.activeAction) {
        r.cursor += 1;
        r.activeAction = undefined;
        r.version += 1;
      }
      return jsonResponse(200, toRitualView(r));
    }

    const ritCompMatch = path.match(/^\/rituals\/([^/]+)\/complete$/);
    if (method === 'POST' && ritCompMatch) {
      const r = rituals.get(ritCompMatch[1]);
      if (!r) return errResponse(404, 'NOT_FOUND', '의식 정보를 찾을 수 없어요.');
      r.status = 'completed';
      r.version += 1;

      let flw: FlowerSummaryDTO | undefined;
      if (r.kind === 'grow' && r.flowerId) {
        flw = flowers.get(r.flowerId);
        if (flw && flw.pending_row && flw.pending_col) {
          flw.status = 'planted';
          flw.planted_row = flw.pending_row;
          flw.planted_col = flw.pending_col;
          flw.planted_at = nowIso();
          const occ = garden.occupancies.find((o) => o.flower_id === flw!.id);
          if (occ) {
            occ.state = 'planted';
            occ.kept_line = flw.kept_line;
            occ.planted_at = flw.planted_at;
            occ.planted_date_local = seoulDateDot();
          }
          garden.has_reserved = false;
          garden.version = String(Number(garden.version) + 1);
        }
      }

      purgeConversationNow(r.conversation_id);
      return jsonResponse(200, {
        conversation_id: r.conversation_id,
        conversation_stage: 'completed',
        conversation_version: '99',
        close_reason: r.kind === 'release' ? 'released' : 'planted',
        purged: true,
        ritual_id: r.id,
        ritual_status: 'completed',
        flower: flw,
        garden_version: garden.version,
      });
    }

    const flwPatchMatch = path.match(/^\/flowers\/([^/]+)$/);
    if (method === 'PATCH' && flwPatchMatch) {
      const flw = flowers.get(flwPatchMatch[1]);
      if (!flw) return errResponse(404, 'NOT_FOUND', '꽃을 찾을 수 없어요.');
      if (body.kept_line !== undefined) flw.kept_line = String(body.kept_line);
      flw.version = String(Number(flw.version) + 1);
      return jsonResponse(200, { ...flw });
    }

    const flwPlantMatch = path.match(/^\/flowers\/([^/]+)\/planting$/);
    if (method === 'POST' && flwPlantMatch) {
      const flw = flowers.get(flwPlantMatch[1]);
      if (!flw) return errResponse(404, 'NOT_FOUND', '꽃을 찾을 수 없어요.');
      const row = Number(body.row);
      const col = Number(body.col);
      if (garden.occupancies.some((o) => o.row === row && o.col === col)) {
        return errResponse(409, 'CELL_OCCUPIED', '이미 다른 꽃이 있는 자리예요.');
      }

      let targetConv: ConversationDetail | undefined;
      for (const c of conversations.values()) {
        if (c.flower_gift?.id === flw.id) targetConv = c;
      }
      if (!targetConv) return errResponse(404, 'NOT_FOUND', '대화를 찾을 수 없어요.');

      flw.status = 'planting';
      flw.pending_row = row;
      flw.pending_col = col;
      flw.version = String(Number(flw.version) + 1);

      const rit: InternalRitual = {
        id: nextId('rit'),
        conversation_id: targetConv.id,
        kind: 'grow',
        route: 'garden',
        cursor: 0,
        status: 'active',
        version: 1,
        flowerId: flw.id,
      };
      startStepOnRitual(rit, 'arrive');
      rituals.set(rit.id, rit);

      const occ: OccupancyDTO = {
        row,
        col,
        state: 'reserved',
        flower_id: flw.id,
        ritual_id: rit.id,
        conversation_id: targetConv.id,
        species_id: flw.species_id,
        species_name: flw.species_name,
        style_seed: flw.style_seed,
        visual_snapshot: flw.visual_snapshot,
        kept_line: flw.kept_line,
        reserved_at: nowIso(),
      };
      garden.occupancies.push(occ);
      garden.has_reserved = true;
      garden.version = String(Number(garden.version) + 1);

      targetConv.stage = 'planting';
      targetConv.active_ritual = toRitualView(rit);
      targetConv.version = String(Number(targetConv.version) + 1);

      return jsonResponse(200, {
        garden_version: garden.version,
        conversation_id: targetConv.id,
        conversation_stage: 'planting',
        conversation_version: targetConv.version,
        flower: flw,
        ritual: targetConv.active_ritual,
      });
    }

    const flwDiscMatch = path.match(/^\/flowers\/([^/]+)\/discard$/);
    if (method === 'POST' && flwDiscMatch) {
      const flw = flowers.get(flwDiscMatch[1]);
      if (!flw) return errResponse(404, 'NOT_FOUND', '꽃을 찾을 수 없어요.');
      flw.status = 'discarded';
      let convId = '';
      for (const c of conversations.values()) {
        if (c.flower_gift?.id === flw.id) convId = c.id;
      }
      if (convId) purgeConversationNow(convId);
      return jsonResponse(200, {
        conversation_id: convId,
        conversation_stage: 'completed',
        conversation_version: '99',
        close_reason: 'discarded',
        purged: true,
        flower: flw,
      });
    }

    if (method === 'GET' && path === '/garden') {
      return jsonResponse(200, JSON.parse(JSON.stringify(garden)));
    }

    if (method === 'PATCH' && path === '/garden') {
      const nextCols = Number(body.cols);
      const nextRows = Number(body.rows);
      let minCols = 1;
      let minRows = 1;
      for (const o of garden.occupancies) {
        if (o.col > minCols) minCols = o.col;
        if (o.row > minRows) minRows = o.row;
      }
      if (nextCols < minCols || nextRows < minRows) {
        return errResponse(409, 'GARDEN_SHRINK_CONFLICT', '이미 심어진 꽃이 있는 영역보다 작게 줄일 수 없어요.', {
          minimum_cols: minCols,
          minimum_rows: minRows,
        });
      }
      garden = {
        ...garden,
        cols: nextCols,
        rows: nextRows,
        version: String(Number(garden.version) + 1),
      };
      return jsonResponse(200, JSON.parse(JSON.stringify(garden)));
    }

    if (method === 'GET' && path === '/settings/ai') {
      return jsonResponse(200, settings);
    }

    if (method === 'PATCH' && path === '/settings/ai') {
      settings = {
        ...settings,
        release_mode: body.release_mode ?? settings.release_mode,
        grow_mode: body.grow_mode ?? settings.grow_mode,
        model_aliases: body.model_aliases ?? settings.model_aliases,
        blind_compare: body.blind_compare ?? settings.blind_compare,
        detailed_eval_enabled: body.detailed_eval_enabled ?? settings.detailed_eval_enabled,
        compare_notice_ack: body.compare_notice_ack ?? settings.compare_notice_ack,
        reduced_motion: body.reduced_motion ?? settings.reduced_motion,
        daily_budget_usd: body.daily_budget_usd ?? settings.daily_budget_usd,
        version: String(Number(settings.version) + 1),
      };
      return jsonResponse(200, settings);
    }

    if (method === 'GET' && path === '/comparisons') {
      const wins: Record<string, number> = {};
      for (const it of comparisonItems) {
        if (it.selected_provider) {
          wins[it.selected_provider] = (wins[it.selected_provider] ?? 0) + 1;
        }
      }
      const resp: ComparisonsResponse = {
        groups:
          comparisonItems.length === 0
            ? []
            : [
                {
                  group_key: 'prompt_v1:gpt-5.4-mini:gemini-2.5-flash',
                  prompt_version: 'prompt_v1',
                  openai_model: 'gpt-5.4-mini',
                  gemini_model: 'gemini-2.5-flash',
                  complete_comparisons: comparisonItems.length,
                  incomplete_early_selections: 0,
                  both_skipped: 0,
                  technical_failures: 0,
                  wins_by_provider: wins,
                },
              ],
        items: comparisonItems,
      };
      return jsonResponse(200, resp);
    }

    return errResponse(404, 'NOT_FOUND', '요청 경로를 찾을 수 없어요.');
  };
}
