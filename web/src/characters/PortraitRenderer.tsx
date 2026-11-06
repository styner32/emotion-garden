import React, { useId } from 'react';
import { Appearance } from '../api/types';
import styles from './PortraitRenderer.module.css';

export type PortraitExpression = 'challenging' | 'apologetic' | 'listening';

export interface PortraitRendererProps {
  appearance?: Partial<Appearance> | null;
  expression?: PortraitExpression;
  name?: string;
  size?: number;
  className?: string;
}

const SKIN_TONE_MAP: Record<string, { base: string; shadow: string; highlight: string }> = {
  tone_01: { base: '#FCE7DC', shadow: '#DDB8A6', highlight: '#FFF5F0' },
  tone_02: { base: '#F5D3C1', shadow: '#CFA18B', highlight: '#FFEBE0' },
  tone_03: { base: '#E5B99E', shadow: '#BA886C', highlight: '#F6D5C0' },
  tone_04: { base: '#C99372', shadow: '#9B6749', highlight: '#DFB093' },
  tone_05: { base: '#9D694A', shadow: '#70452C', highlight: '#B88566' },
  tone_06: { base: '#6F4630', shadow: '#482B1B', highlight: '#8C6047' },
};

function sanitizeHex(hex: string | undefined, fallback: string): string {
  if (!hex || !/^#[0-9A-Fa-f]{6}$/.test(hex.trim())) {
    return fallback;
  }
  return hex.trim().toUpperCase();
}

function adjustLuminance(hex: string, delta: number): string {
  const clean = hex.replace('#', '');
  const r = Math.min(255, Math.max(0, parseInt(clean.slice(0, 2), 16) + delta));
  const g = Math.min(255, Math.max(0, parseInt(clean.slice(2, 4), 16) + delta));
  const b = Math.min(255, Math.max(0, parseInt(clean.slice(4, 6), 16) + delta));
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b
    .toString(16)
    .padStart(2, '0')}`;
}

function getHeadPath(facePreset: string): string {
  switch (facePreset) {
    case 'warm_round':
      return 'M 56 84 C 54 128, 72 156, 100 158 C 128 156, 146 128, 144 84 C 143 52, 124 40, 100 40 C 76 40, 57 52, 56 84 Z';
    case 'sharp_stern':
      return 'M 58 82 C 58 118, 74 148, 100 160 C 126 148, 142 118, 142 82 C 141 51, 123 40, 100 40 C 77 40, 59 51, 58 82 Z';
    case 'blunt_square':
      return 'M 57 80 C 57 122, 66 150, 82 155 L 118 155 C 134 150, 143 122, 143 80 C 142 50, 124 40, 100 40 C 76 40, 58 50, 57 80 Z';
    case 'cynical_narrow':
      return 'M 61 82 C 61 122, 77 152, 100 159 C 123 152, 139 122, 139 82 C 138 52, 121 41, 100 41 C 79 41, 62 52, 61 82 Z';
    case 'calm_oval':
    default:
      return 'M 58 84 C 57 124, 73 154, 100 157 C 127 154, 143 124, 142 84 C 141 52, 123 40, 100 40 C 77 40, 59 52, 58 84 Z';
  }
}

export const PortraitRenderer: React.FC<PortraitRendererProps> = ({
  appearance,
  expression = 'listening',
  name = '인물',
  size = 96,
  className,
}) => {
  const uid = useId().replace(/:/g, '_');

  if (!appearance) {
    return (
      <div
        className={`${styles.frame} ${className ?? ''}`}
        style={{ width: size, height: size }}
        aria-label={`${name} 기본 초상`}
      >
        <div className={styles.fallback}>{name.slice(0, 2)}</div>
      </div>
    );
  }

  const skin = SKIN_TONE_MAP[appearance.skin_tone ?? 'tone_02'] ?? SKIN_TONE_MAP.tone_02;
  const hairBase = sanitizeHex(appearance.hair_color, '#231F20');
  const hairLight = adjustLuminance(hairBase, 46);
  const hairShadow = adjustLuminance(hairBase, -34);

  const clothBase = sanitizeHex(appearance.clothing_color, '#5E7A68');
  const clothLight = adjustLuminance(clothBase, 34);
  const clothShadow = adjustLuminance(clothBase, -38);

  const facePreset = appearance.face_preset ?? 'calm_oval';
  const hairStyle = appearance.hair_style ?? 'dandy_cut';
  const eyeStyle = appearance.eye_style ?? 'sparkling';
  const acne = appearance.acne_level ?? 'none';
  const freckles = appearance.freckles_level ?? 'none';
  const wrinkles = appearance.wrinkles_level ?? 'none';

  const headPath = getHeadPath(facePreset);

  // Brow & mouth geometry modulated by expression while preserving identity
  let leftBrowPath = 'M 71 88 Q 81 84 89 87';
  let rightBrowPath = 'M 111 87 Q 119 84 129 88';
  let mouthPath = 'M 90 134 Q 100 139 110 134';
  let headTilt = 0;

  if (expression === 'challenging') {
    leftBrowPath = 'M 70 84 L 90 91';
    rightBrowPath = 'M 130 84 L 110 91';
    mouthPath = 'M 90 136 Q 100 132 111 135';
    headTilt = -1.8;
  } else if (expression === 'apologetic') {
    leftBrowPath = 'M 70 91 Q 80 85 90 84';
    rightBrowPath = 'M 130 91 Q 120 85 110 84';
    mouthPath = 'M 91 137 Q 100 133 109 137';
    headTilt = 2.2;
  } else {
    // listening
    leftBrowPath = 'M 71 87 Q 81 83 90 86';
    rightBrowPath = 'M 110 86 Q 119 83 129 87';
    mouthPath = 'M 89 133 Q 100 140 111 133';
    headTilt = 1.0;
  }

  return (
    <div
      className={`${styles.frame} ${className ?? ''}`}
      style={{ width: size, height: size }}
      role="img"
      aria-label={`${name} 초상 (${expression})`}
      data-testid="portrait-renderer"
      data-hair-style={hairStyle}
      data-expression={expression}
    >
      <svg viewBox="0 0 200 200" className={styles.svg} aria-hidden="true">
        <defs>
          {/* 3D Volumetric Skin Gradient */}
          <radialGradient id={`skin_${uid}`} cx="38%" cy="32%" r="68%">
            <stop offset="0%" stopColor={skin.highlight} />
            <stop offset="56%" stopColor={skin.base} />
            <stop offset="100%" stopColor={skin.shadow} />
          </radialGradient>

          {/* Neck Ambient Occlusion */}
          <linearGradient id={`neck_${uid}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={skin.shadow} />
            <stop offset="55%" stopColor={skin.base} />
            <stop offset="100%" stopColor={skin.shadow} />
          </linearGradient>

          {/* 3D Hair Gradient (Preserves specular sheen regardless of HEX color) */}
          <linearGradient id={`hair_${uid}`} x1="0.15" y1="0" x2="0.85" y2="1">
            <stop offset="0%" stopColor={hairLight} />
            <stop offset="45%" stopColor={hairBase} />
            <stop offset="100%" stopColor={hairShadow} />
          </linearGradient>

          {/* 3D Clothing Gradient */}
          <linearGradient id={`cloth_${uid}`} x1="0.2" y1="0" x2="0.8" y2="1">
            <stop offset="0%" stopColor={clothLight} />
            <stop offset="52%" stopColor={clothBase} />
            <stop offset="100%" stopColor={clothShadow} />
          </linearGradient>

          <filter id={`softShadow_${uid}`} x="-15%" y="-15%" width="130%" height="130%">
            <feDropShadow dx="0" dy="3" stdDeviation="2.5" floodColor="#241914" floodOpacity="0.18" />
          </filter>
        </defs>

        {/* Subtle warm studio backdrop halo */}
        <circle cx="100" cy="102" r="84" fill="hsla(38, 45%, 94%, 0.55)" />

        {/* Layer 1: Back Hair Silhouette (for long, braided, perm styles) */}
        <g transform={`rotate(${headTilt} 100 110)`}>
          {hairStyle === 'long_straight' && (
            <path
              d="M 48 78 C 44 128, 44 168, 52 194 L 148 194 C 156 168, 156 128, 152 78 C 148 40, 126 26, 100 26 C 74 26, 52 40, 48 78 Z"
              fill={`url(#hair_${uid})`}
            />
          )}
          {hairStyle === 'perm' && (
            <g fill={`url(#hair_${uid})`}>
              <circle cx="54" cy="84" r="22" />
              <circle cx="50" cy="112" r="21" />
              <circle cx="56" cy="138" r="19" />
              <circle cx="146" cy="84" r="22" />
              <circle cx="150" cy="112" r="21" />
              <circle cx="144" cy="138" r="19" />
              <ellipse cx="100" cy="56" rx="54" ry="34" />
            </g>
          )}
          {hairStyle === 'braid' && (
            <g fill={`url(#hair_${uid})`}>
              <ellipse cx="100" cy="64" rx="50" ry="38" />
              {/* Side sculpted braid over right shoulder */}
              <ellipse cx="142" cy="130" rx="13" ry="11" />
              <ellipse cx="145" cy="147" rx="12" ry="10" />
              <ellipse cx="146" cy="163" rx="10" ry="9" />
              <path d="M 141 170 Q 146 184 152 188 Q 138 186 138 172 Z" />
              <circle cx="146" cy="171" r="4" fill={clothLight} />
            </g>
          )}
        </g>

        {/* Layer 2: Neck & 3D Shoulders / Collar */}
        <path
          d="M 84 144 L 84 170 C 84 176, 116 176, 116 170 L 116 144 Z"
          fill={`url(#neck_${uid})`}
        />
        <path
          d="M 34 202 C 38 170, 62 160, 84 158 Q 100 170 116 158 C 138 160, 162 170, 166 202 Z"
          fill={`url(#cloth_${uid})`}
          filter={`url(#softShadow_${uid})`}
        />
        {/* Soft collar seam */}
        <path
          d="M 79 159 Q 100 177 121 159"
          fill="none"
          stroke={clothLight}
          strokeWidth="2.5"
          strokeLinecap="round"
          opacity="0.7"
        />

        {/* Layer 3: Sculpted Head & Ears */}
        <g transform={`rotate(${headTilt} 100 105)`}>
          {/* Ears */}
          <ellipse cx="56" cy="105" rx="8" ry="13" fill={skin.shadow} />
          <ellipse cx="57" cy="105" rx="5" ry="9" fill={skin.base} />
          <ellipse cx="144" cy="105" rx="8" ry="13" fill={skin.shadow} />
          <ellipse cx="143" cy="105" rx="5" ry="9" fill={skin.base} />

          {/* Head Volume */}
          <path d={headPath} fill={`url(#skin_${uid})`} filter={`url(#softShadow_${uid})`} />

          {/* Subsurface Cheek Warmth */}
          <ellipse cx="74" cy="116" rx="11" ry="6.5" fill="#E58B78" opacity="0.22" />
          <ellipse cx="126" cy="116" rx="11" ry="6.5" fill="#E58B78" opacity="0.22" />

          {/* Layer 4: Independent Skin Traits (Wrinkles, Freckles, Acne) */}
          {wrinkles !== 'none' && (
            <g
              stroke={skin.shadow}
              strokeLinecap="round"
              fill="none"
              opacity={wrinkles === 'distinct' ? 0.78 : 0.42}
              strokeWidth={wrinkles === 'distinct' ? 1.6 : 1.1}
            >
              {/* Forehead & smile lines & eye corners */}
              <path d="M 84 68 Q 100 65 116 68" />
              {wrinkles === 'distinct' && <path d="M 87 73 Q 100 71 113 73" />}
              <path d="M 63 99 L 58 97 M 63 102 L 58 103" />
              <path d="M 137 99 L 142 97 M 137 102 L 142 103" />
              <path d="M 81 124 Q 77 131 80 137" />
              <path d="M 119 124 Q 123 131 120 137" />
            </g>
          )}

          {freckles !== 'none' && (
            <g fill="#9E6347" opacity={freckles === 'distinct' ? 0.65 : 0.34}>
              <circle cx="71" cy="113" r="1.3" />
              <circle cx="76" cy="116" r="1.1" />
              <circle cx="81" cy="112" r="1.2" />
              <circle cx="119" cy="112" r="1.2" />
              <circle cx="124" cy="116" r="1.1" />
              <circle cx="129" cy="113" r="1.3" />
              {freckles === 'distinct' && (
                <>
                  <circle cx="95" cy="110" r="1.0" />
                  <circle cx="101" cy="109" r="1.1" />
                  <circle cx="106" cy="111" r="1.0" />
                  <circle cx="68" cy="118" r="1.1" />
                  <circle cx="132" cy="118" r="1.1" />
                </>
              )}
            </g>
          )}

          {acne !== 'none' && (
            <g opacity={acne === 'distinct' ? 0.72 : 0.42}>
              <circle cx="72" cy="126" r="2.1" fill="#D46A62" />
              <circle cx="127" cy="122" r="1.8" fill="#D46A62" />
              {acne === 'distinct' && (
                <>
                  <circle cx="77" cy="131" r="1.7" fill="#C95B53" />
                  <circle cx="122" cy="129" r="2.2" fill="#C95B53" />
                  <circle cx="114" cy="74" r="1.8" fill="#D46A62" />
                </>
              )}
            </g>
          )}

          {/* Layer 5: 3D Nose Bridge & Tip */}
          <path
            d="M 99 96 L 97 115 Q 100 119 104 115"
            fill="none"
            stroke={skin.shadow}
            strokeWidth="2.1"
            strokeLinecap="round"
          />
          <ellipse cx="100" cy="113" rx="2.5" ry="1.6" fill={skin.highlight} opacity="0.65" />

          {/* Layer 6: Eyes (sparkling vs glaring) */}
          {eyeStyle === 'sparkling' ? (
            <g>
              {/* Left eye */}
              <ellipse cx="80" cy="100" rx="8.5" ry="7.5" fill="#FFFFFF" />
              <circle cx="80.5" cy="100.5" r="5.3" fill="#2A211D" />
              <circle cx="78.5" cy="98.2" r="2.1" fill="#FFFFFF" />
              <circle cx="82.5" cy="102.5" r="1.0" fill="#FFFFFF" />
              <path
                d="M 70.5 98 Q 80 91 89.5 98"
                fill="none"
                stroke="#2A211D"
                strokeWidth="2.4"
                strokeLinecap="round"
              />
              {/* Right eye */}
              <ellipse cx="120" cy="100" rx="8.5" ry="7.5" fill="#FFFFFF" />
              <circle cx="119.5" cy="100.5" r="5.3" fill="#2A211D" />
              <circle cx="117.5" cy="98.2" r="2.1" fill="#FFFFFF" />
              <circle cx="121.5" cy="102.5" r="1.0" fill="#FFFFFF" />
              <path
                d="M 110.5 98 Q 120 91 129.5 98"
                fill="none"
                stroke="#2A211D"
                strokeWidth="2.4"
                strokeLinecap="round"
              />
            </g>
          ) : (
            <g>
              {/* Glaring half-lidded eyes */}
              <path d="M 71 100 Q 80 96 89 100 Q 80 106 71 100 Z" fill="#FFFFFF" />
              <circle cx="81" cy="100" r="4.3" fill="#231C19" />
              <circle cx="79.5" cy="99" r="1.2" fill="#FFFFFF" />
              <path
                d="M 70 98 L 90 99"
                fill="none"
                stroke="#231C19"
                strokeWidth="2.8"
                strokeLinecap="round"
              />

              <path d="M 111 100 Q 120 96 129 100 Q 120 106 111 100 Z" fill="#FFFFFF" />
              <circle cx="119" cy="100" r="4.3" fill="#231C19" />
              <circle cx="117.5" cy="99" r="1.2" fill="#FFFFFF" />
              <path
                d="M 130 98 L 110 99"
                fill="none"
                stroke="#231C19"
                strokeWidth="2.8"
                strokeLinecap="round"
              />
            </g>
          )}

          {/* Eyebrows */}
          <path
            d={leftBrowPath}
            fill="none"
            stroke={hairShadow}
            strokeWidth="3.2"
            strokeLinecap="round"
          />
          <path
            d={rightBrowPath}
            fill="none"
            stroke={hairShadow}
            strokeWidth="3.2"
            strokeLinecap="round"
          />

          {/* Mouth */}
          <path
            d={mouthPath}
            fill="none"
            stroke="#9C574B"
            strokeWidth="2.8"
            strokeLinecap="round"
          />

          {/* Layer 7: Front Sculpted Hair & Specular Highlights */}
          {hairStyle === 'sport' && (
            <g>
              <path
                d="M 55 84 C 54 48, 74 30, 100 29 C 126 30, 146 48, 145 84 C 141 64, 124 55, 100 55 C 76 55, 59 64, 55 84 Z"
                fill={`url(#hair_${uid})`}
              />
              <path
                d="M 78 40 L 85 31 L 93 39 L 101 28 L 109 39 L 117 32 L 123 41"
                fill={`url(#hair_${uid})`}
              />
            </g>
          )}

          {hairStyle === 'dandy_cut' && (
            <g>
              <path
                d="M 53 88 C 51 44, 74 28, 102 28 C 129 28, 149 46, 147 88 C 138 69, 125 66, 108 75 C 94 65, 72 68, 53 88 Z"
                fill={`url(#hair_${uid})`}
              />
              <path
                d="M 72 48 Q 100 38 126 50"
                fill="none"
                stroke={hairLight}
                strokeWidth="3"
                strokeLinecap="round"
                opacity="0.45"
              />
            </g>
          )}

          {hairStyle === 'leaf_cut' && (
            <g>
              <path
                d="M 51 94 C 49 45, 74 27, 100 27 C 126 27, 151 45, 149 94 C 136 74, 116 58, 100 54 C 84 58, 64 74, 51 94 Z"
                fill={`url(#hair_${uid})`}
              />
              {/* Center-parted flowing leaf bangs */}
              <path
                d="M 100 44 C 84 52, 68 70, 62 90 C 74 78, 88 68, 100 58 C 112 68, 126 78, 138 90 C 132 70, 116 52, 100 44 Z"
                fill={`url(#hair_${uid})`}
              />
              <path
                d="M 70 54 Q 86 44 98 48 M 102 48 Q 114 44 130 54"
                fill="none"
                stroke={hairLight}
                strokeWidth="2.6"
                strokeLinecap="round"
                opacity="0.45"
              />
            </g>
          )}

          {hairStyle === 'long_straight' && (
            <g>
              <path
                d="M 52 88 C 52 44, 74 28, 100 28 C 126 28, 148 44, 148 88 C 136 68, 118 66, 100 67 C 82 66, 64 68, 52 88 Z"
                fill={`url(#hair_${uid})`}
              />
              <path
                d="M 73 48 Q 100 40 127 48"
                fill="none"
                stroke={hairLight}
                strokeWidth="3.2"
                strokeLinecap="round"
                opacity="0.48"
              />
            </g>
          )}

          {hairStyle === 'perm' && (
            <g fill={`url(#hair_${uid})`}>
              <circle cx="72" cy="54" r="18" />
              <circle cx="92" cy="48" r="19" />
              <circle cx="112" cy="49" r="19" />
              <circle cx="130" cy="56" r="17" />
              <path
                d="M 68 50 Q 82 42 94 48 M 106 48 Q 118 42 130 52"
                fill="none"
                stroke={hairLight}
                strokeWidth="2.6"
                strokeLinecap="round"
                opacity="0.45"
              />
            </g>
          )}

          {hairStyle === 'braid' && (
            <g>
              <path
                d="M 53 88 C 53 44, 75 29, 100 29 C 125 29, 147 44, 147 88 C 134 66, 114 60, 96 62 C 78 64, 64 72, 53 88 Z"
                fill={`url(#hair_${uid})`}
              />
              <path
                d="M 74 49 Q 102 40 126 51"
                fill="none"
                stroke={hairLight}
                strokeWidth="2.8"
                strokeLinecap="round"
                opacity="0.45"
              />
            </g>
          )}
        </g>
      </svg>
    </div>
  );
};
