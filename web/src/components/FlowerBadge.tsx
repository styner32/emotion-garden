import React, { useEffect, useRef } from 'react';
import { FlowerVisualSnapshot } from '../api/types';
import { drawFlowerOnCanvas } from '../scenes/flowerSprites';
import styles from './FlowerBadge.module.css';

export interface FlowerBadgeProps {
  visual?: Partial<FlowerVisualSnapshot>;
  styleSeed: string;
  speciesName?: string;
  size?: number;
  showRootBall?: boolean;
  className?: string;
}

export const FlowerBadge: React.FC<FlowerBadgeProps> = ({
  visual,
  styleSeed,
  speciesName = '작은 꽃',
  size = 64,
  showRootBall = false,
  className,
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    let ctx: CanvasRenderingContext2D | null = null;
    try {
      ctx = canvas.getContext('2d');
    } catch {
      return;
    }
    if (!ctx) return;

    const dpr = Math.min(2, typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1);
    const px = Math.round(size * dpr);
    if (canvas.width !== px || canvas.height !== px) {
      canvas.width = px;
      canvas.height = px;
    }

    ctx.save();
    ctx.clearRect(0, 0, px, px);
    ctx.scale(dpr, dpr);

    if (!showRootBall) {
      ctx.fillStyle = 'rgba(116, 92, 68, 0.16)';
      ctx.beginPath();
      ctx.ellipse(size * 0.5, size * 0.86, size * 0.28, size * 0.06, 0, 0, Math.PI * 2);
      ctx.fill();
    }

    drawFlowerOnCanvas(ctx, visual, styleSeed, size * 0.5, size * 0.85, size * 0.82, {
      showRootBall,
      growthScale: 1,
    });
    ctx.restore();
  }, [visual, styleSeed, size, showRootBall]);

  return (
    <span
      className={`${styles.badge} ${className ?? ''}`}
      style={{ width: size, height: size }}
      aria-label={speciesName}
    >
      <canvas ref={canvasRef} className={styles.canvas} style={{ width: size, height: size }} />
    </span>
  );
};
