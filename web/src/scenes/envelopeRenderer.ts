import { RitualRoute } from '../api/types';

export interface EnvelopeSceneState {
  route: Exclude<RitualRoute, 'garden'>;
  visualStage: number;
  activeActionKey?: string;
  progress: number; // [0, 1]
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

function drawHumanFigure(
  ctx: CanvasRenderingContext2D,
  x: number,
  groundY: number,
  opts: {
    walkPhase?: number;
    bend?: number;
    leftArmAngle?: number;
    rightArmAngle?: number;
    coatColor?: string;
  } = {}
) {
  const walk = opts.walkPhase ?? 0;
  const bend = opts.bend ?? 0;
  const hipY = groundY - 58;
  const torsoLen = 52;

  ctx.save();
  ctx.translate(x, hipY);

  // Soft ground shadow
  ctx.fillStyle = 'rgba(38, 28, 20, 0.16)';
  ctx.beginPath();
  ctx.ellipse(0, 58, 26, 7, 0, 0, Math.PI * 2);
  ctx.fill();

  // Legs
  const legSwing = Math.sin(walk) * 12;
  ctx.strokeStyle = '#3D3732';
  ctx.lineWidth = 8;
  ctx.lineCap = 'round';
  ctx.beginPath();
  ctx.moveTo(-6, 0);
  ctx.lineTo(-8 - legSwing, 55);
  ctx.moveTo(6, 0);
  ctx.lineTo(8 + legSwing, 55);
  ctx.stroke();

  // Upper body with optional bend
  ctx.rotate(bend);

  // Coat / Torso
  ctx.fillStyle = opts.coatColor ?? '#6E5D4F';
  ctx.beginPath();
  ctx.roundRect(-16, -torsoLen, 32, torsoLen + 6, 12);
  ctx.fill();

  // Head
  ctx.fillStyle = '#F2D2BE';
  ctx.beginPath();
  ctx.arc(0, -torsoLen - 16, 14, 0, Math.PI * 2);
  ctx.fill();

  // Hair cap
  ctx.fillStyle = '#2D2520';
  ctx.beginPath();
  ctx.arc(-1, -torsoLen - 19, 14.5, Math.PI * 0.95, Math.PI * 2.1);
  ctx.fill();

  // Left & right arms (shoulder at y = -torsoLen + 8)
  const shoulderY = -torsoLen + 10;
  ctx.strokeStyle = opts.coatColor ?? '#6E5D4F';
  ctx.lineWidth = 7;

  const drawArm = (sx: number, angle: number) => {
    ctx.save();
    ctx.translate(sx, shoulderY);
    ctx.rotate(angle);
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.lineTo(0, 34);
    ctx.stroke();
    ctx.fillStyle = '#F2D2BE';
    ctx.beginPath();
    ctx.arc(0, 36, 4.5, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  };

  drawArm(-13, opts.leftArmAngle ?? 0.2);
  drawArm(13, opts.rightArmAngle ?? -0.2);

  ctx.restore();
}

function drawPaperSack(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  scale = 1,
  tied = true,
  rotation = 0
) {
  ctx.save();
  ctx.translate(x, y);
  ctx.rotate(rotation);
  ctx.scale(scale, scale);

  // Kraft paper body
  ctx.fillStyle = '#D9B68C';
  ctx.strokeStyle = '#9C7853';
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.moveTo(-22, -12);
  ctx.quadraticCurveTo(-28, 18, -18, 28);
  ctx.lineTo(18, 28);
  ctx.quadraticCurveTo(28, 18, 22, -12);
  ctx.closePath();
  ctx.fill();
  ctx.stroke();

  // Top ruffle & twine knot
  ctx.fillStyle = '#E5C7A1';
  ctx.beginPath();
  ctx.moveTo(-12, -12);
  ctx.lineTo(-18, -24);
  ctx.lineTo(-5, -20);
  ctx.lineTo(0, -26);
  ctx.lineTo(6, -20);
  ctx.lineTo(18, -24);
  ctx.lineTo(12, -12);
  ctx.closePath();
  ctx.fill();
  ctx.stroke();

  if (tied) {
    ctx.strokeStyle = '#8C4332';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(-14, -12);
    ctx.lineTo(14, -12);
    ctx.stroke();
    // Little sealing tag
    ctx.fillStyle = '#A64B38';
    ctx.beginPath();
    ctx.arc(0, -11, 4, 0, Math.PI * 2);
    ctx.fill();
  }

  ctx.restore();
}

export function renderEnvelopeScene(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  state: EnvelopeSceneState
): void {
  const p = state.reducedMotion && state.activeActionKey ? 0.85 : clamp01(state.progress);
  const ep = easeInOut(p);
  const act = state.activeActionKey;
  const stage = state.visualStage;

  ctx.save();
  ctx.scale(width / 720, height / 540);

  if (state.route === 'common') {
    // Warm indoor paper room
    const sky = ctx.createLinearGradient(0, 0, 0, 540);
    sky.addColorStop(0, '#F7F1E6');
    sky.addColorStop(1, '#EAE0CF');
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, 720, 540);

    // Wooden floorboard horizon
    ctx.fillStyle = '#D8C7B0';
    ctx.fillRect(0, 390, 720, 150);
    ctx.strokeStyle = '#C2AE95';
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(0, 390);
    ctx.lineTo(720, 390);
    ctx.stroke();

    if (act === 'pack' || (stage === 0 && !act)) {
      drawHumanFigure(ctx, 290, 415, { leftArmAngle: -0.55, rightArmAngle: -0.75 });
      const sackScale = lerp(0.88, 1.05, ep);
      drawPaperSack(ctx, 390, 388, sackScale, p > 0.65);

      // Speech bubbles gliding into the sack
      if (p < 0.85) {
        const alpha = 1 - p / 0.85;
        ctx.save();
        ctx.globalAlpha = alpha;
        const bubbles = [
          { sx: 220, sy: 210 },
          { sx: 460, sy: 190 },
          { sx: 360, sy: 150 },
        ];
        for (const b of bubbles) {
          const bx = lerp(b.sx, 390, ep);
          const by = lerp(b.sy, 365, ep);
          ctx.fillStyle = '#FFFDF9';
          ctx.strokeStyle = '#C7B69F';
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          ctx.roundRect(bx - 28, by - 16, 56, 30, 10);
          ctx.fill();
          ctx.stroke();
        }
        ctx.restore();
      }
    } else if (act === 'lift') {
      const bend = lerp(0.38, 0.04, ep);
      const sackX = lerp(385, 314, ep);
      const sackY = lerp(388, 302, ep);
      drawHumanFigure(ctx, 330, 415, {
        bend,
        leftArmAngle: lerp(-0.8, -2.1, ep),
        rightArmAngle: lerp(-0.9, -2.3, ep),
      });
      drawPaperSack(ctx, sackX, sackY, 0.96, true, lerp(0, -0.25, ep));
    } else {
      // Ready for route choice (stage >= 2): sack resting securely on shoulder
      drawHumanFigure(ctx, 340, 415, {
        leftArmAngle: -2.2,
        rightArmAngle: 0.25,
      });
      drawPaperSack(ctx, 320, 298, 0.95, true, -0.24);
    }

    ctx.restore();
    return;
  }

  if (state.route === 'sea') {
    // Calm twilight / afternoon coastal sky
    const sky = ctx.createLinearGradient(0, 0, 0, 320);
    sky.addColorStop(0, '#DCE8EE');
    sky.addColorStop(1, '#F3E7D8');
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, 720, 320);

    // Distant sea water
    const water = ctx.createLinearGradient(0, 285, 0, 430);
    water.addColorStop(0, '#5A8499');
    water.addColorStop(1, '#7CA3B5');
    ctx.fillStyle = water;
    ctx.fillRect(0, 285, 720, 145);

    // Gentle foam lines
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.55)';
    ctx.lineWidth = 2.5;
    for (let i = 0; i < 3; i++) {
      const wy = 315 + i * 32 + (state.reducedMotion ? 0 : Math.sin(p * Math.PI * 2 + i) * 2.5);
      ctx.beginPath();
      ctx.moveTo(40 + i * 30, wy);
      ctx.quadraticCurveTo(360, wy - 6, 680 - i * 25, wy);
      ctx.stroke();
    }

    // Warm sandy shore
    ctx.fillStyle = '#E4D2B7';
    ctx.beginPath();
    ctx.moveTo(0, 405);
    ctx.quadraticCurveTo(340, 385, 720, 420);
    ctx.lineTo(720, 540);
    ctx.lineTo(0, 540);
    ctx.closePath();
    ctx.fill();

    const isArrive = act === 'arrive';
    const isPrepare = act === 'prepare' || (!act && stage === 1);
    const isThrow = act === 'throw';
    const isDone = !act && stage >= 3;

    if (isArrive) {
      const x = lerp(130, 260, ep);
      drawHumanFigure(ctx, x, 448, {
        walkPhase: state.reducedMotion ? 0 : p * Math.PI * 6,
        leftArmAngle: -2.2,
        rightArmAngle: 0.3,
      });
      drawPaperSack(ctx, x - 18, 331, 0.92, true, -0.22);
    } else if (isPrepare || (!act && stage === 2)) {
      const windUp = act === 'prepare' ? ep : 1;
      drawHumanFigure(ctx, 260, 448, {
        bend: lerp(0, -0.14, windUp),
        leftArmAngle: lerp(-2.2, -1.2, windUp),
        rightArmAngle: lerp(0.3, 1.15, windUp),
      });
      drawPaperSack(ctx, lerp(242, 232, windUp), lerp(331, 362, windUp), 0.92, true, 0.28);
    } else if (isThrow) {
      const releaseT = clamp01((p - 0.18) / 0.62);
      drawHumanFigure(ctx, 265, 448, {
        bend: p < 0.2 ? -0.1 : 0.18,
        leftArmAngle: -0.4,
        rightArmAngle: lerp(1.1, -1.45, clamp01(p * 2.5)),
      });

      if (p < 0.8) {
        // Parabolic arc into the sea
        const sx = lerp(278, 535, releaseT);
        const sy = lerp(355, 336, releaseT) - Math.sin(releaseT * Math.PI) * 125;
        const sinkScale = releaseT > 0.85 ? lerp(0.88, 0.2, (releaseT - 0.85) / 0.15) : 0.88;
        drawPaperSack(ctx, sx, sy, sinkScale, true, releaseT * 2.8);
      }

      if (p >= 0.72) {
        // Water splash & concentric ripples
        const sp = clamp01((p - 0.72) / 0.28);
        ctx.strokeStyle = `rgba(255, 255, 255, ${1 - sp * 0.7})`;
        ctx.lineWidth = 2.5;
        ctx.beginPath();
        ctx.ellipse(535, 342, 16 + sp * 46, 6 + sp * 14, 0, 0, Math.PI * 2);
        ctx.stroke();

        if (sp < 0.65) {
          ctx.fillStyle = '#FFFFFF';
          for (let d = -2; d <= 2; d++) {
            ctx.beginPath();
            ctx.arc(535 + d * 11 * (0.4 + sp), 336 - Math.sin(sp * Math.PI) * (24 - Math.abs(d) * 5), 3, 0, Math.PI * 2);
            ctx.fill();
          }
        }
      }
    } else if (isDone) {
      // Calm sea horizon after envelope vanished
      drawHumanFigure(ctx, 265, 448, { leftArmAngle: 0.18, rightArmAngle: -0.18 });
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.35)';
      ctx.lineWidth = 1.8;
      ctx.beginPath();
      ctx.ellipse(535, 342, 54, 12, 0, 0, Math.PI * 2);
      ctx.stroke();
    }

    ctx.restore();
    return;
  }

  if (state.route === 'incinerate') {
    // Quiet dusk clearing
    const sky = ctx.createLinearGradient(0, 0, 0, 380);
    sky.addColorStop(0, '#D9D5CE');
    sky.addColorStop(1, '#EDE3D4');
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, 720, 380);

    // Stone/gravel clearing
    ctx.fillStyle = '#C9BFAEE8';
    ctx.fillStyle = '#C9BFAE';
    ctx.fillRect(0, 380, 720, 160);

    // Metal burn barrel on the right side (x = 575)
    ctx.fillStyle = '#5A534E';
    ctx.strokeStyle = '#3B3632';
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.roundRect(542, 326, 68, 94, 6);
    ctx.fill();
    ctx.stroke();
    // Barrel ribs
    ctx.beginPath();
    ctx.moveTo(542, 355);
    ctx.lineTo(610, 355);
    ctx.moveTo(542, 388);
    ctx.lineTo(610, 388);
    ctx.stroke();
    // Warm ember glow at top of barrel
    ctx.fillStyle = '#E87A3D';
    ctx.beginPath();
    ctx.ellipse(576, 326, 26, 8, 0, 0, Math.PI * 2);
    ctx.fill();

    // Safe stone pad in center-right (x = 435, well separated from character at x = 250)
    ctx.fillStyle = '#A89F93';
    ctx.beginPath();
    ctx.ellipse(435, 434, 58, 16, 0, 0, Math.PI * 2);
    ctx.fill();

    if (act === 'arrive') {
      const x = lerp(120, 250, ep);
      drawHumanFigure(ctx, x, 434, {
        walkPhase: state.reducedMotion ? 0 : p * Math.PI * 6,
        leftArmAngle: -2.2,
      });
      drawPaperSack(ctx, x - 18, 318, 0.92, true, -0.22);
    } else if (act === 'place' || (!act && stage === 1)) {
      const t = act === 'place' ? ep : 0;
      drawHumanFigure(ctx, lerp(250, 290, Math.sin(t * Math.PI)), 434, {
        bend: Math.sin(t * Math.PI) * 0.28,
        rightArmAngle: lerp(-2.0, -0.8, t),
      });
      drawPaperSack(ctx, lerp(240, 435, t), lerp(320, 408, t), 0.94, true);
    } else if (act === 'take_torch' || (!act && stage === 2)) {
      drawPaperSack(ctx, 435, 408, 0.94, true);
      if (!act) {
        drawHumanFigure(ctx, 255, 434);
      } else {
        // Walk toward barrel side safely, pick lit torch, step back to safe distance (x=285)
        const hx = p < 0.5 ? lerp(255, 500, p * 2) : lerp(500, 285, (p - 0.5) * 2);
        drawHumanFigure(ctx, hx, 434, { rightArmAngle: -1.1 });
        if (p >= 0.42) {
          // Torch stick + small tip flame
          ctx.strokeStyle = '#6B442A';
          ctx.lineWidth = 4;
          ctx.beginPath();
          ctx.moveTo(hx + 28, 366);
          ctx.lineTo(hx + 68, 344);
          ctx.stroke();
          ctx.fillStyle = '#F28B30';
          ctx.beginPath();
          ctx.arc(hx + 71, 341, 7, 0, Math.PI * 2);
          ctx.fill();
        }
      }
    } else if (act === 'ignite' || (!act && stage === 3)) {
      if (!act && stage === 3) {
        drawPaperSack(ctx, 435, 408, 0.94, true);
        drawHumanFigure(ctx, 285, 434, { rightArmAngle: -1.15 });
        ctx.strokeStyle = '#6B442A';
        ctx.lineWidth = 4;
        ctx.beginPath();
        ctx.moveTo(312, 366);
        ctx.lineTo(356, 348);
        ctx.stroke();
        ctx.fillStyle = '#F28B30';
        ctx.beginPath();
        ctx.arc(359, 345, 7, 0, Math.PI * 2);
        ctx.fill();
      } else {
        // Character stands well back at x=275 extending long stick to reach pad at x=435
        drawHumanFigure(ctx, 275, 434, { rightArmAngle: -1.25 });
        ctx.strokeStyle = '#6B442A';
        ctx.lineWidth = 4;
        ctx.beginPath();
        ctx.moveTo(304, 365);
        ctx.lineTo(375, 386);
        ctx.stroke();

        // Sack shrinks into soft ash as fire peaks then subsides
        if (p < 0.72) {
          drawPaperSack(ctx, 435, 410 + p * 14, lerp(0.94, 0.35, p / 0.72), true);
        }

        // Ash mound appears from p > 0.45
        if (p > 0.45) {
          const ashT = clamp01((p - 0.45) / 0.55);
          ctx.fillStyle = '#7D7872';
          ctx.beginPath();
          ctx.ellipse(435, 432, 28 * ashT, 9 * ashT, 0, 0, Math.PI * 2);
          ctx.fill();
        }

        // Clean stylized flames strictly over the stone pad (x=435)
        if (p > 0.1 && p < 0.88) {
          const fireIntensity = p < 0.5 ? (p - 0.1) / 0.4 : (0.88 - p) / 0.38;
          const fh = 76 * clamp01(fireIntensity);
          ctx.fillStyle = 'rgba(232, 106, 44, 0.88)';
          ctx.beginPath();
          ctx.moveTo(405, 428);
          ctx.quadraticCurveTo(418, 428 - fh, 435, 428 - fh * 1.15);
          ctx.quadraticCurveTo(452, 428 - fh, 465, 428);
          ctx.closePath();
          ctx.fill();

          ctx.fillStyle = 'rgba(250, 196, 84, 0.92)';
          ctx.beginPath();
          ctx.moveTo(417, 428);
          ctx.quadraticCurveTo(435, 428 - fh * 0.8, 453, 428);
          ctx.closePath();
          ctx.fill();
        }
      }
    } else {
      // Completed stage >= 4: calm ash mound only
      drawHumanFigure(ctx, 270, 434);
      ctx.fillStyle = '#7D7872';
      ctx.beginPath();
      ctx.ellipse(435, 432, 30, 9, 0, 0, Math.PI * 2);
      ctx.fill();
    }

    ctx.restore();
    return;
  }

  // Route: bury
  const sky = ctx.createLinearGradient(0, 0, 0, 360);
  sky.addColorStop(0, '#E3E9DE');
  sky.addColorStop(1, '#F2EAD9');
  ctx.fillStyle = sky;
  ctx.fillRect(0, 0, 720, 360);

  // Soft hills & earthen ground
  ctx.fillStyle = '#B8C4A8';
  ctx.beginPath();
  ctx.moveTo(0, 365);
  ctx.quadraticCurveTo(240, 330, 490, 360);
  ctx.quadraticCurveTo(620, 372, 720, 350);
  ctx.lineTo(720, 400);
  ctx.lineTo(0, 400);
  ctx.closePath();
  ctx.fill();

  ctx.fillStyle = '#AD9172';
  ctx.fillRect(0, 385, 720, 155);

  const pitX = 430;
  const pitY = 438;

  const drawShovel = (sx: number, sy: number, rot: number) => {
    ctx.save();
    ctx.translate(sx, sy);
    ctx.rotate(rot);
    ctx.strokeStyle = '#7A5438';
    ctx.lineWidth = 4.5;
    ctx.beginPath();
    ctx.moveTo(0, -36);
    ctx.lineTo(0, 22);
    ctx.stroke();
    ctx.fillStyle = '#6A7175';
    ctx.beginPath();
    ctx.roundRect(-9, 20, 18, 20, [2, 2, 9, 9]);
    ctx.fill();
    ctx.restore();
  };

  if (act === 'arrive') {
    const x = lerp(125, 265, ep);
    drawHumanFigure(ctx, x, 434, {
      walkPhase: state.reducedMotion ? 0 : p * Math.PI * 6,
      leftArmAngle: -2.2,
    });
    drawPaperSack(ctx, x - 18, 318, 0.92, true, -0.22);
  } else if (act === 'place' || (!act && stage === 1)) {
    const t = act === 'place' ? ep : 0;
    drawHumanFigure(ctx, 275, 434, {
      bend: Math.sin(t * Math.PI) * 0.25,
      rightArmAngle: lerp(-2.1, -0.6, t),
    });
    drawPaperSack(ctx, lerp(258, 345, t), lerp(318, 412, t), 0.92, true);
  } else if (act === 'dig' || (!act && stage === 2)) {
    drawPaperSack(ctx, 335, 414, 0.9, true);
    const digT = act === 'dig' ? ep : 0;
    if (digT > 0.05) {
      // Pit & soil mound growing simultaneously
      ctx.fillStyle = '#5E4632';
      ctx.beginPath();
      ctx.ellipse(pitX, pitY, 44 * digT, 15 * digT, 0, 0, Math.PI * 2);
      ctx.fill();

      ctx.fillStyle = '#8C6B4F';
      ctx.beginPath();
      ctx.ellipse(pitX + 74, pitY - 2, 36 * digT, 18 * digT, 0, Math.PI, Math.PI * 2);
      ctx.fill();
    }
    const motion = act === 'dig' && !state.reducedMotion ? Math.sin(p * Math.PI * 6) * 0.35 : 0;
    drawHumanFigure(ctx, 278, 434, { bend: 0.16, rightArmAngle: -0.9 + motion });
    drawShovel(322, 398, 0.55 + motion);
  } else if (act === 'lower' || (!act && stage === 3)) {
    const lowT = act === 'lower' ? ep : 0;
    // Pit & mound
    ctx.fillStyle = '#5E4632';
    ctx.beginPath();
    ctx.ellipse(pitX, pitY, 44, 15, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = '#8C6B4F';
    ctx.beginPath();
    ctx.ellipse(pitX + 74, pitY - 2, 36, 18, 0, Math.PI, Math.PI * 2);
    ctx.fill();

    drawShovel(220, 410, -0.35);
    drawHumanFigure(ctx, lerp(278, 345, Math.sin(lowT * Math.PI)), 434, {
      bend: Math.sin(lowT * Math.PI) * 0.32,
      rightArmAngle: -0.95,
    });
    drawPaperSack(ctx, lerp(335, pitX, lowT), lerp(414, pitY - 4, lowT), lerp(0.9, 0.72, lowT), true);
  } else if (act === 'cover' || (!act && stage === 4)) {
    const covT = act === 'cover' ? ep : 0;
    // Pit with sack gradually covered by mound
    if (covT < 0.85) {
      ctx.fillStyle = '#5E4632';
      ctx.beginPath();
      ctx.ellipse(pitX, pitY, 44 * (1 - covT * 0.6), 15 * (1 - covT * 0.6), 0, 0, Math.PI * 2);
      ctx.fill();
      drawPaperSack(ctx, pitX, pitY - 2, 0.68 * (1 - covT), true);
    }
    // Side mound shrinks while covered earth mound forms
    if (covT < 1) {
      ctx.fillStyle = '#8C6B4F';
      ctx.beginPath();
      ctx.ellipse(pitX + 74, pitY - 2, 36 * (1 - covT), 18 * (1 - covT), 0, Math.PI, Math.PI * 2);
      ctx.fill();
    }
    ctx.fillStyle = '#967658';
    ctx.beginPath();
    ctx.ellipse(pitX, pitY + 2, 48 * covT, 12 * covT, 0, 0, Math.PI * 2);
    ctx.fill();

    const pat = act === 'cover' && !state.reducedMotion ? Math.sin(p * Math.PI * 6) * 0.25 : 0;
    drawHumanFigure(ctx, 310, 434, { bend: 0.15, rightArmAngle: -0.85 + pat });
    drawShovel(362, 405, 0.65 + pat);
  } else {
    // Stage >= 5: calm patted earth mound
    ctx.fillStyle = '#967658';
    ctx.beginPath();
    ctx.ellipse(pitX, pitY + 2, 50, 11, 0, 0, Math.PI * 2);
    ctx.fill();
    drawHumanFigure(ctx, 285, 434);
    drawShovel(240, 412, -0.2);
  }

  ctx.restore();
}
