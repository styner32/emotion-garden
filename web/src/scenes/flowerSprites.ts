import { FlowerVisualSnapshot } from '../api/types';

export function hashSeedToUint32(seedStr: string): number {
  let h = 2166136261 >>> 0;
  const s = seedStr || '104729';
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

export function createSeededRng(seedStr: string): () => number {
  let a = hashSeedToUint32(seedStr);
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export interface DrawFlowerOptions {
  showRootBall?: boolean;
  growthScale?: number; // 0.35..1.0
  swayRad?: number;
}

/**
 * Deterministically draws any of the 5 catalog flower species (including four-leaf clover)
 * onto a Canvas 2D context anchored at the soil base `(baseX, baseY)`.
 */
export function drawFlowerOnCanvas(
  ctx: CanvasRenderingContext2D,
  visual: Partial<FlowerVisualSnapshot> | undefined,
  styleSeed: string,
  baseX: number,
  baseY: number,
  sizePx: number,
  opts: DrawFlowerOptions = {}
): void {
  const rng = createSeededRng(styleSeed || visual?.style_seed || '1');
  const speciesId = visual?.species_id ?? 'apricot_blossom';
  const petalHex = visual?.petal_hex ?? '#F4A988';
  const centerHex = visual?.center_hex ?? '#E07A5F';
  const stemHex = visual?.stem_hex ?? '#588157';

  const lean = (rng() - 0.5) * 0.18 + (opts.swayRad ?? 0);
  const scale = (opts.growthScale ?? 1) * (0.92 + rng() * 0.16);
  const stemH = sizePx * 0.62 * scale;
  const bloomR = sizePx * 0.28 * scale;

  ctx.save();
  ctx.translate(baseX, baseY);

  if (opts.showRootBall) {
    ctx.fillStyle = '#6A4B35';
    ctx.beginPath();
    ctx.ellipse(0, 4, sizePx * 0.22, sizePx * 0.15, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.strokeStyle = '#8B684F';
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.moveTo(-4, 6);
    ctx.lineTo(-9, 13);
    ctx.moveTo(3, 7);
    ctx.lineTo(8, 14);
    ctx.stroke();
  }

  ctx.rotate(lean);

  // Curved botanical stem
  const tipX = (rng() - 0.5) * sizePx * 0.1;
  const tipY = -stemH;
  ctx.strokeStyle = stemHex;
  ctx.lineWidth = Math.max(2, sizePx * 0.055);
  ctx.lineCap = 'round';
  ctx.beginPath();
  ctx.moveTo(0, 0);
  ctx.quadraticCurveTo(tipX * 1.4, -stemH * 0.5, tipX, tipY);
  ctx.stroke();

  // Pair of side leaves
  ctx.fillStyle = stemHex;
  ctx.beginPath();
  ctx.ellipse(-sizePx * 0.12, -stemH * 0.38, sizePx * 0.12, sizePx * 0.055, -0.45, 0, Math.PI * 2);
  ctx.fill();
  ctx.beginPath();
  ctx.ellipse(sizePx * 0.12, -stemH * 0.48, sizePx * 0.12, sizePx * 0.055, 0.45, 0, Math.PI * 2);
  ctx.fill();

  ctx.translate(tipX, tipY);

  if (speciesId === 'four_leaf_clover') {
    for (let i = 0; i < 4; i++) {
      ctx.save();
      ctx.rotate((i * Math.PI) / 2 + 0.25);
      ctx.fillStyle = petalHex;
      ctx.beginPath();
      ctx.moveTo(0, 0);
      ctx.bezierCurveTo(-bloomR * 0.85, -bloomR * 0.55, -bloomR * 0.75, -bloomR * 1.35, 0, -bloomR * 0.95);
      ctx.bezierCurveTo(bloomR * 0.75, -bloomR * 1.35, bloomR * 0.85, -bloomR * 0.55, 0, 0);
      ctx.fill();
      ctx.restore();
    }
    ctx.fillStyle = centerHex;
    ctx.beginPath();
    ctx.arc(0, 0, bloomR * 0.2, 0, Math.PI * 2);
    ctx.fill();
  } else if (speciesId === 'lavender_bloom') {
    const tiers = 4;
    for (let t = 0; t < tiers; t++) {
      const yOff = t * bloomR * 0.42 - bloomR * 0.45;
      const w = bloomR * (0.42 + t * 0.05);
      ctx.fillStyle = petalHex;
      ctx.beginPath();
      ctx.ellipse(-w * 0.48, yOff, w * 0.55, bloomR * 0.28, -0.3, 0, Math.PI * 2);
      ctx.ellipse(w * 0.48, yOff, w * 0.55, bloomR * 0.28, 0.3, 0, Math.PI * 2);
      ctx.ellipse(0, yOff - bloomR * 0.12, w * 0.5, bloomR * 0.3, 0, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.fillStyle = centerHex;
    ctx.beginPath();
    ctx.arc(0, -bloomR * 0.65, bloomR * 0.22, 0, Math.PI * 2);
    ctx.fill();
  } else {
    const petalCount = speciesId === 'little_daisy' ? 10 : speciesId === 'pink_petal' ? 6 : 5;
    const angleOffset = rng() * Math.PI;
    for (let i = 0; i < petalCount; i++) {
      ctx.save();
      ctx.rotate(angleOffset + (i * Math.PI * 2) / petalCount);
      ctx.fillStyle = petalHex;
      ctx.strokeStyle = 'rgba(60, 40, 30, 0.14)';
      ctx.lineWidth = 1;
      ctx.beginPath();
      if (speciesId === 'little_daisy') {
        ctx.ellipse(0, -bloomR * 0.72, bloomR * 0.26, bloomR * 0.62, 0, 0, Math.PI * 2);
      } else {
        ctx.ellipse(0, -bloomR * 0.58, bloomR * 0.48, bloomR * 0.62, 0, 0, Math.PI * 2);
      }
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    }
    ctx.fillStyle = centerHex;
    ctx.beginPath();
    ctx.arc(0, 0, bloomR * 0.36, 0, Math.PI * 2);
    ctx.fill();
  }

  ctx.restore();
}
