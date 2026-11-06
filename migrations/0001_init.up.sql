-- 0001_init.up.sql
-- Emotion Garden (마음 한 칸) v1 Core Schema & Invariants

CREATE TABLE app_users (
    id uuid PRIMARY KEY,
    account_kind text NOT NULL CHECK (account_kind IN ('bootstrap', 'registered')),
    timezone text NOT NULL DEFAULT 'Asia/Seoul',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_bootstrap_user
    ON app_users (account_kind)
    WHERE account_kind = 'bootstrap';

CREATE TABLE auth_identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE INDEX idx_auth_identities_user ON auth_identities (user_id);

CREATE TABLE user_settings (
    owner_id uuid PRIMARY KEY,
    release_mode text NOT NULL DEFAULT 'openai' CHECK (release_mode IN ('openai', 'gemini', 'compare')),
    grow_mode text NOT NULL DEFAULT 'openai' CHECK (grow_mode IN ('openai', 'gemini', 'compare')),
    model_aliases jsonb NOT NULL DEFAULT '{"openai":"openai_chat","gemini":"gemini_chat"}'::jsonb,
    blind_compare boolean NOT NULL DEFAULT false,
    detailed_eval_enabled boolean NOT NULL DEFAULT false,
    compare_notice_ack boolean NOT NULL DEFAULT false,
    reduced_motion boolean NOT NULL DEFAULT false,
    daily_budget_usd numeric(12,6),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE characters (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('release', 'grow')),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 20),
    age smallint NOT NULL DEFAULT 28 CHECK (age BETWEEN 1 AND 120),
    gender text NOT NULL CHECK (gender IN ('female', 'male', 'custom', 'unspecified')),
    gender_custom text CHECK (gender_custom IS NULL OR char_length(btrim(gender_custom)) BETWEEN 1 AND 20),
    appearance_json jsonb NOT NULL,
    appearance_version text NOT NULL DEFAULT 'v1',
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_character_gender_custom CHECK (
        (gender = 'custom' AND gender_custom IS NOT NULL)
        OR (gender <> 'custom' AND gender_custom IS NULL)
    )
);

CREATE INDEX idx_characters_owner_archived
    ON characters (owner_id, archived_at, created_at DESC);

CREATE TABLE conversations (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('release', 'grow')),
    character_id uuid NOT NULL,
    character_snapshot jsonb NOT NULL,
    greeting_key text NOT NULL,
    stage text NOT NULL CHECK (stage IN ('chat', 'release_ritual', 'gift_ready', 'planting', 'completed')),
    reply_policy text NOT NULL CHECK (reply_policy IN ('apology', 'quiet', 'supportive')),
    generation_mode text NOT NULL DEFAULT 'openai' CHECK (generation_mode IN ('openai', 'gemini', 'compare')),
    model_aliases jsonb NOT NULL DEFAULT '[]'::jsonb,
    next_message_seq integer NOT NULL DEFAULT 1 CHECK (next_message_seq >= 1),
    next_turn_no integer NOT NULL DEFAULT 1 CHECK (next_turn_no >= 1),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    content_epoch bigint NOT NULL DEFAULT 1 CHECK (content_epoch >= 1),
    closed_at timestamptz,
    close_reason text CHECK (
        close_reason IS NULL OR close_reason IN (
            'ritual_sea', 'ritual_incinerate', 'ritual_bury',
            'flower_planted', 'flower_discarded'
        )
    ),
    purged_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_conversation_policy_kind CHECK (
        (kind = 'release' AND reply_policy IN ('apology', 'quiet'))
        OR (kind = 'grow' AND reply_policy = 'supportive')
    ),
    CONSTRAINT ck_conversation_closed_stage CHECK (
        (stage = 'completed' AND closed_at IS NOT NULL)
        OR (stage <> 'completed' AND closed_at IS NULL)
    )
);

CREATE UNIQUE INDEX uq_open_conversation
    ON conversations (owner_id, kind)
    WHERE closed_at IS NULL;

CREATE INDEX idx_conversations_owner_closed
    ON conversations (owner_id, closed_at, updated_at DESC);

CREATE TABLE conversation_drafts (
    conversation_id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    content text NOT NULL DEFAULT '' CHECK (char_length(content) <= 4000),
    draft_version bigint NOT NULL DEFAULT 1 CHECK (draft_version >= 1),
    content_epoch bigint NOT NULL DEFAULT 1 CHECK (content_epoch >= 1),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversation_turns (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    turn_no integer NOT NULL CHECK (turn_no >= 1),
    client_turn_id text NOT NULL CHECK (char_length(client_turn_id) BETWEEN 1 AND 128),
    mode text NOT NULL CHECK (mode IN ('quiet', 'openai', 'gemini', 'compare')),
    slot_order jsonb NOT NULL DEFAULT '[]'::jsonb,
    context_snapshot jsonb,
    context_hash text NOT NULL,
    prompt_version text NOT NULL,
    prompt_hash text NOT NULL,
    selected_run_id uuid,
    resolution text CHECK (
        resolution IS NULL OR resolution IN (
            'no_reply', 'auto_accepted', 'user_selected', 'no_answer', 'cancelled_on_closure'
        )
    ),
    resolved_at timestamptz,
    purged_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    content_epoch bigint NOT NULL DEFAULT 1 CHECK (content_epoch >= 1),
    next_event_seq bigint NOT NULL DEFAULT 1 CHECK (next_event_seq >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_turn_resolution CHECK (
        (resolved_at IS NULL AND resolution IS NULL)
        OR (resolved_at IS NOT NULL AND resolution IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_unresolved_turn
    ON conversation_turns (conversation_id)
    WHERE resolved_at IS NULL;

CREATE UNIQUE INDEX uq_client_turn
    ON conversation_turns (conversation_id, client_turn_id);

CREATE UNIQUE INDEX uq_turn_no
    ON conversation_turns (conversation_id, turn_no);

CREATE INDEX idx_turns_owner_created
    ON conversation_turns (owner_id, created_at DESC);

CREATE TABLE conversation_messages (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid,
    seq integer NOT NULL CHECK (seq >= 1),
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    origin text NOT NULL CHECK (origin IN ('opening', 'user', 'ai_selected')),
    content text NOT NULL,
    selected_run_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_message_shape CHECK (
        (origin = 'opening' AND role = 'assistant' AND turn_id IS NULL AND selected_run_id IS NULL)
        OR (origin = 'user' AND role = 'user' AND turn_id IS NOT NULL AND selected_run_id IS NULL)
        OR (origin = 'ai_selected' AND role = 'assistant' AND turn_id IS NOT NULL AND selected_run_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_message_seq
    ON conversation_messages (conversation_id, seq);

CREATE UNIQUE INDEX uq_opening
    ON conversation_messages (conversation_id)
    WHERE origin = 'opening';

CREATE UNIQUE INDEX uq_user_message
    ON conversation_messages (turn_id)
    WHERE origin = 'user';

CREATE UNIQUE INDEX uq_selected_answer
    ON conversation_messages (turn_id)
    WHERE origin = 'ai_selected';

CREATE TABLE ai_runs (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    slot text NOT NULL CHECK (slot IN ('primary', 'a', 'b')),
    attempt_no integer NOT NULL DEFAULT 1 CHECK (attempt_no >= 1),
    provider text NOT NULL CHECK (provider IN ('openai', 'gemini', 'mock_openai', 'mock_gemini')),
    model_alias text NOT NULL,
    requested_model text NOT NULL,
    reported_model text,
    is_mock boolean NOT NULL DEFAULT false,
    settings_snapshot jsonb NOT NULL,
    prompt_version text NOT NULL,
    prompt_hash text NOT NULL,
    context_hash text NOT NULL,
    sdk_version text NOT NULL,
    adapter_version text NOT NULL,
    api_revision text NOT NULL,
    status text NOT NULL CHECK (
        status IN (
            'queued', 'running', 'streaming', 'succeeded',
            'failed', 'incomplete', 'blocked', 'cancelled', 'interrupted'
        )
    ),
    output_text text,
    provider_request_id text,
    finish_reason text,
    error_code text,
    superseded_at timestamptz,
    lease_token text,
    lease_expires_at timestamptz,
    captured_epoch bigint NOT NULL,
    queued_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    first_text_at timestamptz,
    finished_at timestamptz,
    purged_at timestamptz,
    usage_json jsonb,
    usage_status text NOT NULL DEFAULT 'unknown' CHECK (usage_status IN ('known', 'partial', 'unknown')),
    price_version text,
    estimated_cost numeric(14,8),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_current_run
    ON ai_runs (turn_id, slot)
    WHERE superseded_at IS NULL;

CREATE UNIQUE INDEX uq_run_attempt
    ON ai_runs (turn_id, slot, attempt_no);

CREATE INDEX idx_ai_runs_queued
    ON ai_runs (queued_at, id)
    WHERE status = 'queued' AND superseded_at IS NULL;

CREATE INDEX idx_ai_runs_lease
    ON ai_runs (lease_expires_at)
    WHERE status IN ('running', 'streaming');

CREATE INDEX idx_ai_runs_owner_created
    ON ai_runs (owner_id, created_at DESC);

CREATE TABLE ai_run_events (
    turn_id uuid NOT NULL,
    seq bigint NOT NULL CHECK (seq >= 1),
    owner_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    run_id uuid,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (turn_id, seq)
);

CREATE UNIQUE INDEX uq_turn_event
    ON ai_run_events (turn_id, seq);

CREATE INDEX idx_ai_run_events_created
    ON ai_run_events (created_at);

CREATE INDEX idx_ai_run_events_conversation
    ON ai_run_events (conversation_id);

CREATE TABLE turn_feedbacks (
    turn_id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    scores_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rituals (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('release', 'planting')),
    definition_version text NOT NULL DEFAULT 'v1',
    route text NOT NULL CHECK (route IN ('common', 'sea', 'incinerate', 'bury', 'garden')),
    cursor integer NOT NULL DEFAULT 0 CHECK (cursor >= 0),
    next_ordinal integer NOT NULL DEFAULT 1 CHECK (next_ordinal >= 1),
    active_action_id uuid,
    status text NOT NULL CHECK (status IN ('active', 'completed', 'abandoned')),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_active_ritual
    ON rituals (conversation_id)
    WHERE status = 'active';

CREATE TABLE ritual_actions (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    ritual_id uuid NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 1),
    route text NOT NULL,
    action_key text NOT NULL,
    started_at timestamptz NOT NULL,
    planned_end_at timestamptz NOT NULL,
    completed_at timestamptz,
    skipped boolean NOT NULL DEFAULT false,
    UNIQUE (ritual_id, ordinal)
);

CREATE UNIQUE INDEX uq_active_action
    ON ritual_actions (ritual_id)
    WHERE completed_at IS NULL;

CREATE TABLE gardens (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    cols smallint NOT NULL DEFAULT 4 CHECK (cols > 0),
    rows smallint NOT NULL DEFAULT 3 CHECK (rows > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_garden_owner
    ON gardens (owner_id);

CREATE TABLE flower_gifts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    source_conversation_id uuid NOT NULL,
    catalog_version text NOT NULL DEFAULT 'v1',
    species_id text NOT NULL,
    species_name text NOT NULL,
    style_seed bigint NOT NULL,
    visual_snapshot jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('available', 'planting', 'planted', 'discarded')),
    kept_line text NOT NULL DEFAULT '' CHECK (char_length(kept_line) <= 80),
    pending_row smallint CHECK (pending_row IS NULL OR pending_row > 0),
    pending_col smallint CHECK (pending_col IS NULL OR pending_col > 0),
    planted_row smallint CHECK (planted_row IS NULL OR planted_row > 0),
    planted_col smallint CHECK (planted_col IS NULL OR planted_col > 0),
    planted_at timestamptz,
    discarded_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_flower_source
    ON flower_gifts (source_conversation_id);

CREATE INDEX idx_flower_gifts_owner
    ON flower_gifts (owner_id, created_at DESC);

CREATE TABLE garden_occupancies (
    garden_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    row_no smallint NOT NULL CHECK (row_no > 0),
    col_no smallint NOT NULL CHECK (col_no > 0),
    flower_id uuid NOT NULL UNIQUE,
    ritual_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('reserved', 'planted')),
    reserved_at timestamptz NOT NULL,
    planted_at timestamptz,
    PRIMARY KEY (garden_id, row_no, col_no),
    CHECK (
        (state = 'reserved' AND planted_at IS NULL)
        OR (state = 'planted' AND planted_at IS NOT NULL)
    )
);

CREATE INDEX idx_garden_occupancies_owner
    ON garden_occupancies (owner_id, garden_id);

CREATE TABLE idempotency_records (
    owner_id uuid NOT NULL,
    scope text NOT NULL,
    key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 128),
    request_fingerprint text NOT NULL,
    status_code integer NOT NULL,
    response_json jsonb NOT NULL,
    result_ids jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (owner_id, scope, key)
);

CREATE INDEX idx_idempotency_expires
    ON idempotency_records (expires_at);
