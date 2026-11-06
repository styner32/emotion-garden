import { FlowerSummaryDTO } from '../api/types';
import { drawFlowerOnCanvas } from './flowerSprites';

export interface GardenPlantingSceneState {
  visualStage: number; // 0..4
  activeActionKey?: string; // arrive | dig | plant | cover | water
  progress: number; // [0, 1]
  flower?: FlowerSummaryDTO;
  reducedMotion?: boolean;
}

function clamp01(v: number): number {
  return Math.max(0, Math.min(1, v));
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * clamp01(t);
}

function easeInOut(t: number): number {
  const x = clamp01(t);
  return x < 0.5 ? 2 * x * x : 1 - Math.pow(-2 * x + 2, 2) / 2;
}

function drawGardener(
  ctx: CanvasRenderingContext2D,
  x: number,
  groundY: number,
  opts: {
    walkPhase?: number;
    bend?: number;
    leftArmAngle?: number;
    rightArmAngle?: number;
  } = {}
) {
  const walk = opts.walkPhase ?? 0;
  const bend = opts.bend ?? 0;
  const hipY = groundY - 58;
  const torsoLen = 52;

  ctx.save();
  ctx.translate(x, hipY);

  // Ground shadow
  ctx.fillStyle = 'rgba(34, 44, 28, 0.15)';
  ctx.beginPath();
  ctx.ellipse(0, 58, 26, 7, 0, 0, Math.PI * 2);
  ctx.fill();

  // Legs
  const swing = Math.sin(walk) * 11;
  ctx.strokeStyle = '#4A433B';
  ctx.lineWidth = 8;
  ctx.lineCap = 'round';
  ctx.beginPath();
  ctx.moveTo(-6, 0);
  ctx.lineTo(-8 - swing, 55);
  ctx.moveTo(6, 0);
  ctx.lineTo(8 + swing, 55);
  ctx.stroke();

  ctx.rotate(bend);

  // Botanical apron & linen shirt
  ctx.fillStyle = '#5B7863';
  ctx.beginPath();
  ctx.roundRect(-16, -torsoLen, 32, torsoLen + 6, 12);
  ctx.fill();

  // Head
  ctx.fillStyle = '#F4D5C2';
  ctx.beginPath();
  ctx.arc(0, -torsoLen - 16, 14, 0, Math.PI * 2);
  ctx.fill();

  // Soft sun-hat / hair
  ctx.fillStyle = '#3B2F28';
  ctx.beginPath();
  ctx.arc(-1, -torsoLen - 19, 14.5, Math.PI * 0.95, Math.PI * 2.1);
  ctx.fill();

  const shoulderY = -torsoLen + 10;
  ctx.strokeStyle = '#5B7863';
  ctx.lineWidth = 7;

  const drawArm = (sx: number, angle: number) => {
    ctx.save();
    ctx.translate(sx, shoulderY);
    ctx.rotate(angle);
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.lineTo(0, 34);
    ctx.stroke();
    ctx.fillStyle = '#F4D5C2';
    ctx.beginPath();
    ctx.arc(0, 36, 4.5, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  };

  drawArm(-13, opts.leftArmAngle ?? 0.2);
  drawArm(13, opts.rightArmAngle ?? -0.2);

  ctx.restore();
}

function drawShovelTool(ctx: CanvasRenderingContext2D, x: number, y: number, rot: number) {
  ctx.save();
  ctx.translate(x, y);
  ctx.rotate(rot);
  ctx.strokeStyle = '#855D3E';
  ctx.lineWidth = 4.5;
  ctx.beginPath();
  ctx.moveTo(0, -36);
  ctx.lineTo(0, 20);
  ctx.stroke();
  ctx.fillStyle = '#737B7E';
  ctx.beginPath();
  ctx.roundRect(-9, 18, 18, 20, [2, 2, 9, 9]);
  ctx.fill();
  ctx.restore();
}

function drawWateringCan(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  tiltRad: number,
  pouring: boolean,
  pourProgress: number
) {
  ctx.save();
  ctx.translate(x, y);
  ctx.rotate(tiltRad);

  // Handle
  ctx.strokeStyle = '#4B6B55';
  ctx.lineWidth = 4;
  ctx.beginPath();
  ctx.arc(-4, -8, 16, Math.PI * 0.7, Math.PI * 2.1);
  ctx.stroke();

  // Body
  ctx.fillStyle = '#6C9178';
  ctx.strokeStyle = '#44614D';
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.roundRect(-18, -14, 34, 28, 6);
  ctx.fill();
  ctx.stroke();

  // Spout pointing right-down
  ctx.beginPath();
  ctx.moveTo(15, 2);
  ctx.lineTo(36, -12);
  ctx.lineTo(39, -6);
  ctx.lineTo(15, 10);
  ctx.closePath();
  ctx.fill();
  ctx.stroke();

  // Rose head
  ctx.fillStyle = '#D6B678';
  ctx.beginPath();
  ctx.ellipse(39, -9, 4, 7, 0.4, 0, Math.PI * 2);
  ctx.fill();

  if (pouring) {
    ctx.strokeStyle = 'rgba(120, 178, 214, 0.82)';
    ctx.lineWidth = 2;
    ctx.setLineDash([5, 5]);
    ctx.lineDashOffset = -pourProgress * 48;
    for (let s = -1; s <= 1; s++) {
      ctx.beginPath();
      ctx.moveTo(42, -8 + s * 2);
      ctx.quadraticCurveTo(62, 12 + s * 6, 74 + s * 6, 48);
      ctx.stroke();
    }
    ctx.setLineDash([]);
  }

  ctx.restore();
}

export function renderGardenPlantingScene(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  state: GardenPlantingSceneState
): void {
  const p = state.reducedMotion && state.activeActionKey ? 0.85 : clamp01(state.progress);
  const ep = easeInOut(p);
  const act = state.activeActionKey;
  const stage = state.visualStage;

  ctx.save();
  ctx.scale(width / 720, height / 540);

  // sunlit botanical morning backdrop
  const sky = ctx.createLinearGradient(0, 0, 0, 355);
  sky.addColorStop(0, '#E7EFE2');
  sky.addColorStop(1, '#F7F1E3');
  ctx.fillStyle = sky;
  ctx.fillRect(0, 0, 720, 355);

  // Soft garden hedge & wooden fence
  ctx.fillStyle = '#C5D3B8';
  ctx.fillRect(0, 330, 720, 45);
  ctx.strokeStyle = '#D7C5AA';
  ctx.lineWidth = 4;
  for (let fx = 40; fx < 700; fx += 72) {
    ctx.beginPath();
    ctx.moveTo(fx, 305);
    ctx.lineTo(fx, 365);
    ctx.stroke();
  }
  ctx.beginPath();
  ctx.moveTo(20, 325);
  ctx.lineTo(700, 325);
  ctx.stroke();

  // Garden lawn & warm soil bed
  ctx.fillStyle = '#DCE3CE';
  ctx.fillRect(0, 365, 720, 175);

  // Raised wooden-edged flower bed
  ctx.fillStyle = '#B89878';
  ctx.strokeStyle = '#8F6F52';
  ctx.lineWidth = 3;
  ctx.beginPath();
  ctx.roundRect(330, 408, 250, 76, 14);
  ctx.fill();
  ctx.stroke();

  const spotX = 435;
  const spotY = 442;
  const seed = state.flower?.style_seed ?? '104729';
  const vis = state.flower?.visual_snapshot;

  if (act === 'arrive' || (!act && stage === 0)) {
    const gx = act === 'arrive' ? lerp(120, 275, ep) : 275;
    drawGardener(ctx, gx, 438, {
      walkPhase: act === 'arrive' && !state.reducedMotion ? p * Math.PI * 6 : 0,
    });
    // Seedling waiting in woven tray near bed
    drawFlowerOnCanvas(ctx, vis, seed, 535, 434, 48, { showRootBall: true, growthScale: 0.72 });
  } else if (act === 'dig' || (!act && stage === 1)) {
    const digT = act === 'dig' ? ep : 1;
    // Hole + soil mound
    ctx.fillStyle = '#5E442F';
    ctx.beginPath();
    ctx.ellipse(spotX, spotY, 34 * digT, 12 * digT, 0, 0, Math.PI * 2);
    ctx.fill();

    ctx.fillStyle = '#8D6B4D';
    ctx.beginPath();
    ctx.ellipse(spotX + 58, spotY - 1, 28 * digT, 15 * digT, 0, Math.PI, Math.PI * 2);
    ctx.fill();

    drawFlowerOnCanvas(ctx, vis, seed, 545, 434, 48, { showRootBall: true, growthScale: 0.72 });

    const swing = act === 'dig' && !state.reducedMotion ? Math.sin(p * Math.PI * 6) * 0.32 : 0;
    drawGardener(ctx, 290, 438, { bend: act === 'dig' ? 0.18 : 0.05, rightArmAngle: -0.95 + swing });
    drawShovelTool(ctx, 345, 405, 0.55 + swing);
  } else if (act === 'plant' || (!act && stage === 2)) {
    const plT = act === 'plant' ? ep : 1;
    // Shovel resting on grass to the left
    drawShovelTool(ctx, 218, 426, -1.25);

    // Hole & side mound
    ctx.fillStyle = '#5E442F';
    ctx.beginPath();
    ctx.ellipse(spotX, spotY, 34, 12, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = '#8D6B4D';
    ctx.beginPath();
    ctx.ellipse(spotX + 58, spotY - 1, 28, 15, 0, Math.PI, Math.PI * 2);
    ctx.fill();

    // Gardener cradles root ball with BOTH hands and places into hole
    const gx = act === 'plant' ? lerp(295, 355, Math.sin(plT * Math.PI)) : 295;
    drawGardener(ctx, gx, 438, {
      bend: act === 'plant' ? Math.sin(plT * Math.PI) * 0.34 : 0.05,
      leftArmAngle: act === 'plant' ? -1.05 : 0.15,
      rightArmAngle: act === 'plant' ? -1.15 : -0.15,
    });

    const fx = lerp(385, spotX, plT);
    const fy = lerp(388, spotY, plT);
    drawFlowerOnCanvas(ctx, vis, seed, fx, fy, 54, { showRootBall: true, growthScale: 0.8 });
  } else if (act === 'cover' || (!act && stage === 3)) {
    const covT = act === 'cover' ? ep : 1;
    if (covT < 1) {
      ctx.fillStyle = '#8D6B4D';
      ctx.beginPath();
      ctx.ellipse(spotX + 58, spotY - 1, 28 * (1 - covT), 15 * (1 - covT), 0, Math.PI, Math.PI * 2);
      ctx.fill();
    }
    // Covered firm soil collar around stem
    ctx.fillStyle = '#997758';
    ctx.beginPath();
    ctx.ellipse(spotX, spotY + 2, 38, 11, 0, 0, Math.PI * 2);
    ctx.fill();

    drawFlowerOnCanvas(ctx, vis, seed, spotX, spotY, 58, { showRootBall: false, growthScale: 0.88 });

    const tamp = act === 'cover' && !state.reducedMotion ? Math.sin(p * Math.PI * 6) * 0.2 : 0;
    drawGardener(ctx, 305, 438, { bend: act === 'cover' ? 0.15 : 0.04, rightArmAngle: -0.85 + tamp });
    drawShovelTool(ctx, 362, 412, 0.65 + tamp);
  } else {
    // act === 'water' or completed stage >= 4
    const isWatering = act === 'water';
    const wT = isWatering ? ep : 1;

    // Soil darkens with rich moisture
    ctx.fillStyle = wT > 0.35 ? '#6B4E37' : '#997758';
    ctx.beginPath();
    ctx.ellipse(spotX, spotY + 2, 42, 12, 0, 0, Math.PI * 2);
    ctx.fill();

    const sway = !state.reducedMotion && isWatering ? Math.sin(p * Math.PI * 4) * 0.05 : 0;
    drawFlowerOnCanvas(ctx, vis, seed, spotX, spotY, 64, {
      showRootBall: false,
      growthScale: lerp(0.88, 1.04, wT),
      swayRad: sway,
    });

    if (isWatering && p < 0.86) {
      drawGardener(ctx, 295, 438, { bend: 0.12, rightArmAngle: -1.15 });
      drawWateringCan(ctx, 348, 382, 0.42, true, p);
    } else {
      // Watering finished: gardener stands warmly, watering can rests beside bed
      drawGardener(ctx, 282, 438);
      drawWateringCan(ctx, 518, 435, 0, false, 1);
    }
  }

  ctx.restore();
}
