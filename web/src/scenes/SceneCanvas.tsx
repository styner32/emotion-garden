import React, { useEffect, useRef, useState } from 'react';
import { computeActionProgress, estimateServerNowMs } from '../api/timeSync';
import { FlowerSummaryDTO, RitualView } from '../api/types';
import { renderEnvelopeScene } from './envelopeRenderer';
import { renderGardenPlantingScene } from './gardenPlantingRenderer';
import styles from './SceneCanvas.module.css';

export interface SceneCanvasProps {
  ritual: RitualView;
  flower?: FlowerSummaryDTO;
  reducedMotion?: boolean;
  forceFallback?: boolean;
  onActionTimerElapsed?: () => void;
}

const LOGICAL_WIDTH = 720;
const LOGICAL_HEIGHT = 540;

export const SceneCanvas: React.FC<SceneCanvasProps> = ({
  ritual,
  flower,
  reducedMotion = false,
  forceFallback = false,
  onActionTimerElapsed,
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [renderFailed, setRenderFailed] = useState(false);
  const elapsedFiredForActionRef = useRef<string | null>(null);

  const prefersReduced =
    reducedMotion ||
    (typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches);

  useEffect(() => {
    if (forceFallback || renderFailed) return;
    const canvas = canvasRef.current;
    if (!canvas) return;

    let ctx: CanvasRenderingContext2D | null = null;
    try {
      ctx = canvas.getContext('2d');
    } catch {
      setRenderFailed(true);
      return;
    }
    if (!ctx) {
      setRenderFailed(true);
      return;
    }

    const dpr = Math.min(2, typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1);
    const targetW = Math.round(LOGICAL_WIDTH * dpr);
    const targetH = Math.round(LOGICAL_HEIGHT * dpr);
    if (canvas.width !== targetW || canvas.height !== targetH) {
      canvas.width = targetW;
      canvas.height = targetH;
    }

    let rafId = 0;
    let active = true;

    const drawFrame = () => {
      if (!active || !ctx) return;
      const act = ritual.active_action;
      const nowServerMs = estimateServerNowMs();
      const progress = act
        ? computeActionProgress(act.started_at, act.planned_end_at, nowServerMs)
        : 1;

      try {
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        if (ritual.route === 'garden') {
          renderGardenPlantingScene(ctx, canvas.width, canvas.height, {
            visualStage: ritual.visual_stage,
            activeActionKey: act?.key,
            progress,
            flower: flower ?? ritual.flower,
            reducedMotion: prefersReduced,
          });
        } else {
          renderEnvelopeScene(ctx, canvas.width, canvas.height, {
            route: ritual.route,
            visualStage: ritual.visual_stage,
            activeActionKey: act?.key,
            progress,
            reducedMotion: prefersReduced,
          });
        }
      } catch {
        setRenderFailed(true);
        return;
      }

      if (act) {
        if (progress >= 1 && elapsedFiredForActionRef.current !== act.id) {
          elapsedFiredForActionRef.current = act.id;
          onActionTimerElapsed?.();
        } else if (progress < 1) {
          rafId = window.requestAnimationFrame(drawFrame);
        }
      }
    };

    drawFrame();

    return () => {
      active = false;
      if (rafId) {
        window.cancelAnimationFrame(rafId);
      }
    };
  }, [
    ritual.id,
    ritual.route,
    ritual.visual_stage,
    ritual.version,
    ritual.active_action?.id,
    ritual.active_action?.key,
    ritual.active_action?.started_at,
    ritual.active_action?.planned_end_at,
    flower,
    ritual.flower,
    prefersReduced,
    forceFallback,
    renderFailed,
    onActionTimerElapsed,
  ]);

  if (forceFallback || renderFailed) {
    return (
      <div className={styles.wrapper} data-testid="scene-fallback">
        <div className={styles.fallbackCard}>
          <span className={styles.stageBadge}>{ritual.route_label}</span>
          <h3>{ritual.current_step_label}</h3>
          <p>{ritual.current_description}</p>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.wrapper} data-testid="scene-canvas-wrapper">
      <canvas
        ref={canvasRef}
        className={styles.canvas}
        role="img"
        aria-label={`${ritual.route_label} — ${ritual.current_step_label}: ${ritual.current_description}`}
      />
    </div>
  );
};
