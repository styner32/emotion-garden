# 마음 한 칸 (Emotion Garden)

비움(털어놓기)과 피움(고민 나누기) 두 가지 감정 흐름을 제공하는 웹 애플리케이션입니다. 모든 마무리된 대화의 원문·초안·스트리밍 이벤트는 완료 시점 트랜잭션에서 즉시 삭제되며, 피움의 꽃밭에는 오직 꽃의 모습과 심은 날짜(`Asia/Seoul`), 사용자가 직접 남긴 짧은 한 줄(`<=80`자)만 남습니다.

## 핵심 기능 요약

- **등장인물 제작 및 3D 음영 레이어드 초상**: 역할 고정(`release` / `grow`, 반대 역할 복제 지원), 얼굴 윤곽 프리셋 5종, 머리 스타일 6종(`파마`, `긴 생머리`, `땋은 머리`, `스포츠`, `리프컷`, `댄디컷`), 피부 톤 6종, 눈빛 2종, 독립 조합 가능한 피부 특징 3축(`여드름`, `주근깨`, `주름`), HEX 색상 커스터마이징.
- **병렬 활성 슬롯 및 AI 스트리밍**: 비움 1개 + 피움 1개 동시 진행 지원, 무응답(`quiet`) 모드, OpenAI Responses API(`store=false`), Google Gemini Interactions SSE API(`store=false`), 두 모델 블라인드 비교(`compare`) 모드, API 키 미설정 시 즉시 체험 가능한 한국어 스트리밍 Mock 어댑터 내장.
- **서버 권위 의식(Ritual) 상태머신 및 4:3 Canvas 2D 연출**:
  - **비움(봉투 떠나보내기)**: 봉투 담기·들기(경로 선택 전까지 대화 복귀 가능) → `바다로 던지기` / `화로에 소각하기` / `땅에 묻기` 단계별 연출 → 완료 즉시 대화 원문 완전 삭제.
  - **피움(꽃 선물 & 꽃밭 심기)**: 1회성 꽃 선물 생성(사계절 화훼 5종 + 네잎클로버 희귀 변종) → `80`자 이내 남길 한 줄 작성 → `1..6 × 1..8` 꽃밭 자리 예약 → 사람이 직접 삽으로 땅을 파고 꽃을 심고 흙을 덮고 물뿌리개로 물을 주는 5단계 연출 → 완료 즉시 대화 원문 삭제 및 꽃밭 영구 식재.

---

## 로컬 실행 및 세팅 가이드

### 방법 1: Docker Compose로 한 번에 실행하기 (권장 · macOS / Windows / Linux 공통)

Go나 PostgreSQL을 별도로 설치하지 않아도 **Docker Desktop**(또는 Docker Engine)만 있으면 즉시 실행됩니다.

```bash
# 1. 저장소 클론 및 폴더 이동
git clone https://github.com/styner32/emotion-garden.git
cd emotion-garden

# 2. (선택) OpenAI / Gemini 실제 API 키를 사용하려면 .env 파일 생성
cp .env.example .env
# .env 파일 내 OPENAI_API_KEY=..., GEMINI_API_KEY=... 입력 (비워두면 한국어 Mock AI로 동작)

# 3. DB + 마이그레이션 + 웹 서버 통합 빌드 및 실행
docker compose up --build
```

- 컨테이너 기동이 완료되면 내 PC 브라우저에서 `http://localhost:8080` 으로 접속합니다.
- 종료하려면 터미널에서 `Ctrl+C` 후 `docker compose down`을 실행합니다(DB 데이터까지 초기화하려면 `docker compose down -v`).

---

### 방법 2: 로컬 네이티브 개발 환경에서 직접 실행하기

코드를 수정하며 개발하거나 테스트 스위트를 직접 실행할 때 사용합니다.

#### 1) 사전 요구 사항
- **Go** `1.24+` (`1.27` 권장)
- **Node.js** `22+` (`24 LTS` 권장) 및 `npm`
- **PostgreSQL** `15+` (`initdb`, `pg_ctl`, `psql`, `pg_dump`, `pg_restore` CLI 포함)

#### 2) 의존성 설치 및 실행

```bash
# 1. 프론트엔드 패키지 설치
npm --prefix web ci

# 2. (선택) 환경 변수 파일 복사 및 API 키 설정
cp .env.example .env

# 3. 프론트엔드 번들 + Go 서버 빌드 후 로컬 DB 자동 기동 & 마이그레이션 & 서버 시작
make preview
```

- 서버가 시작되면 브라우저에서 `http://localhost:8080` 으로 접속합니다.
- 만약 이미 별도로 실행 중인 PostgreSQL 인스턴스를 사용하려면 `.env`의 `DATABASE_URL`을 수정한 뒤 아래 순서로 실행하세요:

```bash
set -a && source .env && set +a
go run ./cmd/migrate up
npm --prefix web run build
go run ./cmd/server
```

#### 3) 프론트엔드 핫 리로드(Vite Dev Server) 개발 모드

```bash
# 터미널 1: 백엔드 서버 (:8080) 기동
make preview

# 터미널 2: Vite 개발 서버 (:5173 -> /api 프록시) 실행
npm --prefix web run dev
```

---

## 환경 변수 설정 요약 (`.env`)

기본값(`ALLOW_MOCK_AI=true`)에서는 별도 유료 API 키 없이도 한국어 스트리밍 Mock 어댑터(`mock_openai`, `mock_gemini`)로 모든 대화·비교 모드를 즉시 체험할 수 있습니다.

| 변수명 | 기본값 | 설명 |
| :--- | :--- | :--- |
| `OPENAI_API_KEY` | *(비어 있음)* | OpenAI Responses API 키 (`config/models.yaml` 모델 사용 시) |
| `GEMINI_API_KEY` | *(비어 있음)* | Google Gemini Interactions API 키 |
| `ALLOW_MOCK_AI` | `true` | `true`일 때 API 키 미설정 시 내장 한국어 Mock 스트리밍 사용 |
| `DATABASE_URL` | `postgres://app:change_me@127.0.0.1:5432/emotion_garden?sslmode=disable` | PostgreSQL 연결 문자열 |
| `HTTP_ADDR` | `127.0.0.1:8080` | 백엔드 바인딩 주소 |
| `PUBLIC_ORIGIN` | `http://localhost:8080` | 허용 Origin 기준 주소 |

---

## 디렉터리 구조

- `cmd/server`, `cmd/migrate`, `cmd/eval-live`: Go HTTP/SSE 서버, 마이그레이션·무결성 검증 CLI, 라이브 모델 평가 CLI
- `internal/`: 도메인 서비스(`character`, `conversation`, `ai`, `ritual`, `garden`, `identity`, `storage`, `httpapi`)
- `migrations/`: PostgreSQL 스키마 마이그레이션 (`0001_init.up.sql`, `0001_init.down.sql`)
- `config/`, `prompts/`: 모델/가격표, 아바타/꽃/의식 카탈로그 YAML 및 버전 관리 시스템 프롬프트
- `api/openapi.yaml`: OpenAPI 3.1 계약 문서
- `web/`: React 19 + TypeScript + Vite SPA, Vitest 단위 테스트, Playwright E2E 테스트
- `scripts/`: 로컬 PostgreSQL 클러스터(`dev-db.sh`), 백업/복원 검증(`verify-backup-restore.sh`), 프리뷰 실행(`start-preview.sh`)

## 테스트 및 무결성 검증 명령어

```bash
# 1. 전체 빌드 (Go 바이너리 + 프론트엔드 프로덕션 번들)
make build

# 2. 계약 일관성 검사 (OpenAPI <-> Go <-> SQL <-> TypeScript)
make check-contract

# 3. 백엔드 단위/통합 테스트 (PostgreSQL + Race Detector)
make test-go

# 4. 프론트엔드 단위 테스트 (Vitest) & 브라우저 E2E 테스트 (Playwright)
make test-web
make test-e2e

# 5. PostgreSQL 백업/복원 및 스키마·고아 레코드 무결성 검증
make backup-verify
```
