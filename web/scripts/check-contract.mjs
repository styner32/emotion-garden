import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import YAML from 'yaml';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..');

const openapiRaw = fs.readFileSync(path.join(repoRoot, 'api', 'openapi.yaml'), 'utf8');
const openapi = YAML.parse(openapiRaw);
const schemas = openapi?.components?.schemas ?? {};

const tsTypesRaw = fs.readFileSync(path.join(repoRoot, 'web', 'src', 'api', 'types.ts'), 'utf8');
const goErrorsRaw = fs.readFileSync(path.join(repoRoot, 'internal', 'apperr', 'errors.go'), 'utf8');
const sqlRaw = fs.readFileSync(path.join(repoRoot, 'migrations', '0001_init.up.sql'), 'utf8');

function extractTsConstArray(name) {
  const re = new RegExp(`export const ${name}\\s*=\\s*\\[([\\s\\S]*?)\\]\\s*as const;`);
  const m = tsTypesRaw.match(re);
  if (!m) {
    throw new Error(`Missing TS constant array ${name} in web/src/api/types.ts`);
  }
  return [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1]);
}

function assertSameSet(label, expected, actual) {
  const expSorted = [...expected].sort();
  const actSorted = [...actual].sort();
  if (JSON.stringify(expSorted) !== JSON.stringify(actSorted)) {
    throw new Error(
      `Contract mismatch for ${label}:\n  OpenAPI: ${JSON.stringify(expSorted)}\n  Actual : ${JSON.stringify(actSorted)}`
    );
  }
}

const checks = [
  ['ConversationKind', 'CONVERSATION_KINDS'],
  ['ConversationStage', 'CONVERSATION_STAGES'],
  ['ReplyPolicy', 'REPLY_POLICIES'],
  ['GenerationMode', 'GENERATION_MODES'],
  ['TurnMode', 'TURN_MODES'],
  ['RunSlot', 'RUN_SLOTS'],
  ['RunStatus', 'RUN_STATUSES'],
  ['UsageStatus', 'USAGE_STATUSES'],
  ['RitualRoute', 'RITUAL_ROUTES'],
  ['RitualStatus', 'RITUAL_STATUSES'],
  ['FlowerStatus', 'FLOWER_STATUSES'],
  ['OccupancyState', 'OCCUPANCY_STATES'],
  ['DraftDisposition', 'DRAFT_DISPOSITIONS'],
  ['ErrorCode', 'ERROR_CODES'],
];

for (const [schemaName, tsConstName] of checks) {
  const oaEnum = schemas[schemaName]?.enum;
  if (!Array.isArray(oaEnum)) {
    throw new Error(`OpenAPI schema ${schemaName} missing enum array`);
  }
  const tsValues = extractTsConstArray(tsConstName);
  assertSameSet(`${schemaName} <-> TS ${tsConstName}`, oaEnum, tsValues);
}

// Verify Go apperr error codes match OpenAPI ErrorCode enum
const goErrorCodes = [...goErrorsRaw.matchAll(/Code\w+\s*=\s*"([A-Z_]+)"/g)].map((m) => m[1]);
assertSameSet('OpenAPI ErrorCode <-> Go apperr', schemas.ErrorCode.enum, goErrorCodes);

// Verify SQL stage enum values appear in migrations
for (const stage of schemas.ConversationStage.enum) {
  if (!sqlRaw.includes(`'${stage}'`)) {
    throw new Error(`SQL schema missing ConversationStage value '${stage}'`);
  }
}

console.log(`Contract check PASSED: ${checks.length} enum groups verified across OpenAPI, Go, SQL, and TypeScript.`);
