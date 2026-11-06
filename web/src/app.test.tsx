import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SSEFrameParser } from './api/sse';
import { computeActionProgress, updateServerClockOffset } from './api/timeSync';
import { PortraitRenderer } from './characters/PortraitRenderer';
import { createSeededRng, hashSeedToUint32 } from './scenes/flowerSprites';
import { SceneCanvas } from './scenes/SceneCanvas';

describe('SSEFrameParser', () => {
  it('parses chunked Korean SSE frames, multi-line data, and ignores heartbeats', () => {
    const parser = new SSEFrameParser();

    const part1 = ': heartbeat 2026-11-06T00:00:00Z\n\nid: 1\nevent: run.del';
    const part2 = 'ta\ndata: {"run_id":"r1","text":"안녕';
    const part3 = '하세요"}\n\nid: 2\nevent: custom\ndata: 첫째 줄\ndata: 둘째 줄\n\n';

    expect(parser.feed(part1)).toEqual([]);
    expect(parser.feed(part2)).toEqual([]);

    const frames = parser.feed(part3);
    expect(frames).toHaveLength(2);
    expect(frames[0]).toEqual({
      id: '1',
      event: 'run.delta',
      data: '{"run_id":"r1","text":"안녕하세요"}',
    });
    expect(frames[1]).toEqual({
      id: '2',
      event: 'custom',
      data: '첫째 줄\n둘째 줄',
    });
  });
});

describe('Server Clock Sync & Ritual Action Progress', () => {
  it('compensates client clock skew and clamps action progress to [0, 1]', () => {
    const serverNow = '2026-11-06T12:00:10.000Z';
    const clientNow = Date.parse('2026-11-06T12:00:05.000Z'); // client 5s behind server
    updateServerClockOffset(serverNow, clientNow);

    const start = '2026-11-06T12:00:08.000Z';
    const end = '2026-11-06T12:00:12.000Z';

    // At clientNow (which corresponds to server 12:00:10), progress should be 0.5
    const mid = computeActionProgress(start, end, clientNow + 5000);
    expect(mid).toBeCloseTo(0.5, 4);

    // Before start -> clamped to 0
    expect(computeActionProgress(start, end, Date.parse('2026-11-06T12:00:01.000Z'))).toBe(0);

    // Long after planned_end_at (tab slept) -> clamped to 1
    expect(computeActionProgress(start, end, Date.parse('2026-11-06T12:05:00.000Z'))).toBe(1);
  });
});

describe('Deterministic Flower Style RNG', () => {
  it('produces identical float sequences for the same style_seed', () => {
    const h1 = hashSeedToUint32('seed_987654321');
    const h2 = hashSeedToUint32('seed_987654321');
    expect(h1).toBe(h2);

    const rngA = createSeededRng('seed_987654321');
    const rngB = createSeededRng('seed_987654321');
    const seqA = [rngA(), rngA(), rngA(), rngA()];
    const seqB = [rngB(), rngB(), rngB(), rngB()];
    expect(seqA).toEqual(seqB);
  });
});

describe('PortraitRenderer & SceneCanvas Accessibility / Fallbacks', () => {
  it('renders distinct layered SVG for all 6 hair styles and independent skin traits', () => {
    const hairStyles = ['perm', 'long_straight', 'braid', 'sport', 'leaf_cut', 'dandy_cut'];
    const outputs = new Set<string>();

    for (const hair of hairStyles) {
      const html = renderToStaticMarkup(
        <PortraitRenderer
          name="테스트"
          expression="apologetic"
          appearance={{
            schema_version: 'v1',
            asset_catalog_version: '2026-01',
            face_preset: 'warm_round',
            hair_style: hair,
            hair_color: '#3B2F2F',
            skin_tone: 'tone_03',
            eye_style: 'sparkling',
            acne_level: 'mild',
            freckles_level: 'distinct',
            wrinkles_level: 'mild',
            clothing_color: '#5E7A68',
          }}
        />
      );
      expect(html).toContain('<svg');
      outputs.add(html);
    }

    // Every hair style produces distinct SVG geometry
    expect(outputs.size).toBe(6);

    // Fallback when appearance is missing (CHAR05)
    const fallbackHtml = renderToStaticMarkup(<PortraitRenderer name="나무" appearance={null} />);
    expect(fallbackHtml).toContain('나무');
  });

  it('renders text-and-card fallback when SceneCanvas fallback is active (RIT08)', () => {
    const html = renderToStaticMarkup(
      <SceneCanvas
        forceFallback
        ritual={{
          id: 'rit-1',
          conversation_id: 'conv-1',
          kind: 'release',
          definition_version: 'v1',
          route: 'sea',
          route_label: '바다로 던지기',
          cursor: 2,
          total_actions: 4,
          visual_stage: 2,
          status: 'active',
          version: '3',
          current_step_label: '바다를 향해 던지기',
          current_description: '봉투가 포물선을 그리며 파도 위로 날아갑니다.',
          allowed_commands: ['skip_current_action'],
        }}
      />
    );
    expect(html).toContain('scene-fallback');
    expect(html).toContain('바다로 던지기');
    expect(html).toContain('바다를 향해 던지기');
  });
});
