import React, { useEffect, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { Appearance, ConversationKind } from '../api/types';
import { PortraitExpression, PortraitRenderer } from '../characters/PortraitRenderer';
import styles from './CharacterFormPage.module.css';

const DEFAULT_APPEARANCE: Omit<Appearance, 'schema_version' | 'asset_catalog_version'> = {
  face_preset: 'calm_oval',
  hair_style: 'dandy_cut',
  hair_color: '#231F20',
  skin_tone: 'tone_02',
  eye_style: 'sparkling',
  acne_level: 'none',
  freckles_level: 'none',
  wrinkles_level: 'none',
  clothing_color: '#5E7A68',
};

export const CharacterFormPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const isEdit = Boolean(id);
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const cloneFromId = searchParams.get('cloneFrom');
  const queryKind = searchParams.get('kind');

  const { data: catalog } = useQuery({
    queryKey: ['avatar-catalog'],
    queryFn: () => api.getAvatarCatalog(),
  });

  const { data: charData, isLoading: charsLoading } = useQuery({
    queryKey: ['characters', true],
    queryFn: () => api.listCharacters(true),
    enabled: isEdit || Boolean(cloneFromId),
  });

  const [kind, setKind] = useState<ConversationKind>(
    queryKind === 'release' ? 'release' : 'grow'
  );
  const [name, setName] = useState('');
  const [age, setAge] = useState('29');
  const [gender, setGender] = useState<'female' | 'male' | 'custom' | 'unspecified'>('unspecified');
  const [genderCustom, setGenderCustom] = useState('');
  const [appearance, setAppearance] = useState(DEFAULT_APPEARANCE);
  const [expectedVersion, setExpectedVersion] = useState('1');
  const [previewExp, setPreviewExp] = useState<PortraitExpression>(
    queryKind === 'release' ? 'challenging' : 'listening'
  );
  const [initializedFromSource, setInitializedFromSource] = useState(false);
  const [saving, setSaving] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (initializedFromSource) return;
    if (isEdit && charData) {
      const target = charData.characters.find((c) => c.id === id);
      if (target) {
        setKind(target.kind);
        setName(target.name);
        setAge(String(target.age || 29));
        setGender(target.gender);
        setGenderCustom(target.gender_custom ?? '');
        setAppearance({
          face_preset: target.appearance.face_preset,
          hair_style: target.appearance.hair_style,
          hair_color: target.appearance.hair_color,
          skin_tone: target.appearance.skin_tone,
          eye_style: target.appearance.eye_style,
          acne_level: target.appearance.acne_level,
          freckles_level: target.appearance.freckles_level,
          wrinkles_level: target.appearance.wrinkles_level,
          clothing_color: target.appearance.clothing_color,
        });
        setExpectedVersion(target.version);
        setPreviewExp(target.kind === 'release' ? 'challenging' : 'listening');
        setInitializedFromSource(true);
      }
    } else if (cloneFromId && charData) {
      const source = charData.characters.find((c) => c.id === cloneFromId);
      if (source) {
        const oppositeKind: ConversationKind = source.kind === 'release' ? 'grow' : 'release';
        setKind(oppositeKind);
        setName(`${source.name}`.slice(0, 20));
        setAge(String(source.age || 29));
        setGender(source.gender);
        setGenderCustom(source.gender_custom ?? '');
        setAppearance({
          face_preset: source.appearance.face_preset,
          hair_style: source.appearance.hair_style,
          hair_color: source.appearance.hair_color,
          skin_tone: source.appearance.skin_tone,
          eye_style: source.appearance.eye_style,
          acne_level: source.appearance.acne_level,
          freckles_level: source.appearance.freckles_level,
          wrinkles_level: source.appearance.wrinkles_level,
          clothing_color: source.appearance.clothing_color,
        });
        setPreviewExp(oppositeKind === 'release' ? 'challenging' : 'listening');
        setInitializedFromSource(true);
      }
    }
  }, [isEdit, id, cloneFromId, charData, initializedFromSource]);

  const setAppField = <K extends keyof typeof DEFAULT_APPEARANCE>(
    key: K,
    val: (typeof DEFAULT_APPEARANCE)[K]
  ) => {
    setAppearance((prev) => ({ ...prev, [key]: val }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const trimmedName = name.trim();
    if (!trimmedName) {
      setErrorMsg('인물의 이름을 1~20자로 입력해 주세요.');
      return;
    }
    const parsedAge = parseInt(age, 10);
    if (!Number.isFinite(parsedAge) || parsedAge < 1 || parsedAge > 120) {
      setErrorMsg('나이는 1~120 사이의 숫자로 입력해 주세요.');
      return;
    }

    setSaving(true);
    setErrorMsg(null);
    try {
      if (isEdit && id) {
        await api.updateCharacter(id, {
          expected_version: expectedVersion,
          name: trimmedName,
          age: parsedAge,
          gender,
          gender_custom: gender === 'custom' ? genderCustom.trim() : undefined,
          appearance,
        });
        await queryClient.invalidateQueries({ queryKey: ['characters'] });
        navigate('/characters');
      } else {
        const created = await api.createCharacter({
          kind,
          name: trimmedName,
          age: parsedAge,
          gender,
          gender_custom: gender === 'custom' ? genderCustom.trim() : undefined,
          appearance,
        });
        await queryClient.invalidateQueries({ queryKey: ['characters'] });
        navigate(`/?launch=${created.kind}&characterId=${encodeURIComponent(created.id)}`);
      }
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg('인물 정보를 저장하는 중 문제가 생겼어요.');
      }
    } finally {
      setSaving(false);
    }
  };

  if ((isEdit || cloneFromId) && charsLoading) {
    return <p className={styles.loading}>인물 정보를 불러오는 중…</p>;
  }

  return (
    <div className={styles.page}>
      <div className={styles.topRow}>
        <div>
          <Link to="/characters" className={styles.backLink}>
            ← 등장인물 목록으로
          </Link>
          <h1 className={styles.title}>
            {isEdit
              ? '등장인물 다듬기'
              : cloneFromId
                ? '반대 역할 인물로 복제 생성'
                : '새 등장인물 만들기'}
          </h1>
        </div>

        {isEdit && id && (
          <Link
            to={`/characters/new?cloneFrom=${encodeURIComponent(id)}`}
            className={styles.cloneBtn}
          >
            반대 역할({kind === 'release' ? '피움' : '비움'})로 복제 생성
          </Link>
        )}
      </div>

      <div className={styles.layout}>
        {/* Sticky Live Portrait Preview */}
        <aside className={styles.previewPane} aria-label="실시간 초상 미리보기">
          <div className={styles.previewCard}>
            <PortraitRenderer
              appearance={appearance}
              expression={previewExp}
              name={name || '미리보기'}
              size={176}
            />
            <strong className={styles.previewName}>{name.trim() || '이름 미입력'}</strong>
            <span className={kind === 'release' ? styles.rolePillRelease : styles.rolePillGrow}>
              {kind === 'release' ? '털어놓기 · 비움 상대' : '고민 나누기 · 피움 상대'}
            </span>

            <div className={styles.expPreviewBox}>
              <span className={styles.expLabel}>표정 미리보기</span>
              <div className={styles.expBtns}>
                {(
                  [
                    ['challenging', '도전적'],
                    ['apologetic', '사과하는'],
                    ['listening', '경청하는'],
                  ] as const
                ).map(([expKey, label]) => (
                  <button
                    key={expKey}
                    type="button"
                    className={`${styles.expBtn} ${
                      previewExp === expKey ? styles.expBtnActive : ''
                    }`}
                    onClick={() => setPreviewExp(expKey)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </aside>

        {/* Form Controls */}
        <form className={styles.formPane} onSubmit={(e) => void handleSubmit(e)}>
          {errorMsg && (
            <div className={styles.errorBanner} role="alert">
              {errorMsg}
            </div>
          )}

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>1. 역할과 기본 정보</h2>

            <div className={styles.field}>
              <span className={styles.label}>대화 역할 {isEdit && '(생성 후 변경 불가)'}</span>
              <div className={styles.roleChoices}>
                <button
                  type="button"
                  disabled={isEdit}
                  onClick={() => {
                    setKind('release');
                    setPreviewExp('challenging');
                  }}
                  className={`${styles.roleCard} ${
                    kind === 'release' ? styles.roleReleaseActive : ''
                  }`}
                  data-testid="role-release-btn"
                >
                  <strong>비움 상대 (털어놓기)</strong>
                  <span>서운함이나 분노를 쏟아내고 진심 어린 사과를 듣거나 조용히 흘려보낼 대상</span>
                </button>
                <button
                  type="button"
                  disabled={isEdit}
                  onClick={() => {
                    setKind('grow');
                    setPreviewExp('listening');
                  }}
                  className={`${styles.roleCard} ${kind === 'grow' ? styles.roleGrowActive : ''}`}
                  data-testid="role-grow-btn"
                >
                  <strong>피움 상대 (고민 나누기)</strong>
                  <span>판단 없이 내 이야기를 들어주고 마지막에 작은 꽃을 건네줄 대상</span>
                </button>
              </div>
            </div>

            <div className={styles.twoCol}>
              <div className={styles.field}>
                <label htmlFor="char-name-input" className={styles.label}>
                  이름 / 호칭 (1~20자)
                </label>
                <input
                  id="char-name-input"
                  type="text"
                  maxLength={20}
                  required
                  className={styles.input}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="예: 김 팀장, 오랜 친구, 숲지기"
                  data-testid="char-name-input"
                />
              </div>

              <div className={styles.field}>
                <label htmlFor="char-age-input" className={styles.label}>
                  나이 (1~120)
                </label>
                <input
                  id="char-age-input"
                  type="number"
                  min={1}
                  max={120}
                  className={styles.input}
                  value={age}
                  onChange={(e) => setAge(e.target.value)}
                  data-testid="char-age-input"
                />
              </div>
            </div>

            <div className={styles.field}>
              <span className={styles.label}>성별</span>
              <div className={styles.chipRow}>
                {(
                  [
                    ['unspecified', '지정 안 함'],
                    ['female', '여성'],
                    ['male', '남성'],
                    ['custom', '직접 입력'],
                  ] as const
                ).map(([gVal, gLabel]) => (
                  <button
                    key={gVal}
                    type="button"
                    className={`${styles.chip} ${gender === gVal ? styles.chipActive : ''}`}
                    onClick={() => setGender(gVal)}
                  >
                    {gLabel}
                  </button>
                ))}
              </div>
              {gender === 'custom' && (
                <input
                  type="text"
                  maxLength={20}
                  className={styles.input}
                  value={genderCustom}
                  onChange={(e) => setGenderCustom(e.target.value)}
                  placeholder="직접 성별 정체성 입력 (1~20자)"
                />
              )}
            </div>
          </section>

          {catalog && (
            <>
              <section className={styles.section}>
                <h2 className={styles.sectionTitle}>2. 얼굴형과 눈매 · 피부 톤</h2>

                <div className={styles.field}>
                  <span className={styles.label}>얼굴 윤곽 프리셋</span>
                  <div className={styles.presetGrid}>
                    {catalog.face_presets.map((fp) => (
                      <button
                        key={fp.id}
                        type="button"
                        onClick={() => setAppField('face_preset', fp.id)}
                        className={`${styles.presetCard} ${
                          appearance.face_preset === fp.id ? styles.presetActive : ''
                        }`}
                        data-testid={`face-preset-${fp.id}`}
                      >
                        <strong>{fp.label}</strong>
                        <span>{fp.description}</span>
                      </button>
                    ))}
                  </div>
                </div>

                <div className={styles.field}>
                  <span className={styles.label}>피부 톤</span>
                  <div className={styles.swatchRow}>
                    {catalog.skin_tones.map((st) => (
                      <button
                        key={st.id}
                        type="button"
                        onClick={() => setAppField('skin_tone', st.id)}
                        className={`${styles.skinBtn} ${
                          appearance.skin_tone === st.id ? styles.skinActive : ''
                        }`}
                        data-testid={`skin-tone-${st.id}`}
                      >
                        <span
                          className={styles.colorCircle}
                          style={{ backgroundColor: st.base_hex }}
                        />
                        <span>{st.label}</span>
                      </button>
                    ))}
                  </div>
                </div>

                <div className={styles.field}>
                  <span className={styles.label}>눈빛 인상</span>
                  <div className={styles.chipRow}>
                    {catalog.eye_styles.map((es) => (
                      <button
                        key={es.id}
                        type="button"
                        onClick={() => setAppField('eye_style', es.id)}
                        className={`${styles.chip} ${
                          appearance.eye_style === es.id ? styles.chipActive : ''
                        }`}
                        data-testid={`eye-style-${es.id}`}
                      >
                        {es.label}
                      </button>
                    ))}
                  </div>
                </div>
              </section>

              <section className={styles.section}>
                <h2 className={styles.sectionTitle}>3. 머리 모양과 색상</h2>

                <div className={styles.field}>
                  <span className={styles.label}>머리 스타일 (6종)</span>
                  <div className={styles.chipRow}>
                    {catalog.hair_styles.map((hs) => (
                      <button
                        key={hs.id}
                        type="button"
                        onClick={() => setAppField('hair_style', hs.id)}
                        className={`${styles.chip} ${
                          appearance.hair_style === hs.id ? styles.chipActive : ''
                        }`}
                        data-testid={`hair-style-${hs.id}`}
                      >
                        {hs.label}
                      </button>
                    ))}
                  </div>
                </div>

                <div className={styles.field}>
                  <span className={styles.label}>머리 색상</span>
                  <div className={styles.colorRow}>
                    {catalog.hair_palette.map((hex) => (
                      <button
                        key={hex}
                        type="button"
                        aria-label={`머리 색상 ${hex}`}
                        onClick={() => setAppField('hair_color', hex)}
                        className={`${styles.hexSwatch} ${
                          appearance.hair_color.toUpperCase() === hex.toUpperCase()
                            ? styles.hexSwatchActive
                            : ''
                        }`}
                        style={{ backgroundColor: hex }}
                      />
                    ))}
                    <input
                      type="color"
                      aria-label="직접 머리 색상 선택"
                      value={
                        /^#[0-9A-Fa-f]{6}$/.test(appearance.hair_color)
                          ? appearance.hair_color
                          : '#231F20'
                      }
                      onChange={(e) => setAppField('hair_color', e.target.value.toUpperCase())}
                      className={styles.colorPicker}
                    />
                  </div>
                </div>
              </section>

              <section className={styles.section}>
                <h2 className={styles.sectionTitle}>4. 피부 특징(독립 조합)과 상의 색상</h2>

                {(
                  [
                    ['acne_level', '트러블 · 여드름'],
                    ['freckles_level', '주근깨'],
                    ['wrinkles_level', '주름 · 세월감'],
                  ] as const
                ).map(([fieldKey, label]) => (
                  <div key={fieldKey} className={styles.field}>
                    <span className={styles.label}>{label}</span>
                    <div className={styles.chipRow}>
                      {catalog.feature_levels.map((lvl) => (
                        <button
                          key={lvl.id}
                          type="button"
                          onClick={() => setAppField(fieldKey, lvl.id)}
                          className={`${styles.chip} ${
                            appearance[fieldKey] === lvl.id ? styles.chipActive : ''
                          }`}
                          data-testid={`trait-${fieldKey}-${lvl.id}`}
                        >
                          {lvl.label}
                        </button>
                      ))}
                    </div>
                  </div>
                ))}

                <div className={styles.field}>
                  <span className={styles.label}>상의 색상</span>
                  <div className={styles.colorRow}>
                    {catalog.clothing_palette.map((hex) => (
                      <button
                        key={hex}
                        type="button"
                        aria-label={`상의 색상 ${hex}`}
                        onClick={() => setAppField('clothing_color', hex)}
                        className={`${styles.hexSwatch} ${
                          appearance.clothing_color.toUpperCase() === hex.toUpperCase()
                            ? styles.hexSwatchActive
                            : ''
                        }`}
                        style={{ backgroundColor: hex }}
                      />
                    ))}
                    <input
                      type="color"
                      aria-label="직접 상의 색상 선택"
                      value={
                        /^#[0-9A-Fa-f]{6}$/.test(appearance.clothing_color)
                          ? appearance.clothing_color
                          : '#5E7A68'
                      }
                      onChange={(e) => setAppField('clothing_color', e.target.value.toUpperCase())}
                      className={styles.colorPicker}
                    />
                  </div>
                </div>
              </section>
            </>
          )}

          <div className={styles.submitBar}>
            <Link to="/characters" className={styles.cancelBtn}>
              취소
            </Link>
            <button
              type="submit"
              disabled={saving}
              className={kind === 'release' ? styles.submitReleaseBtn : styles.submitGrowBtn}
              data-testid="save-character-btn"
            >
              {saving
                ? '저장하는 중…'
                : isEdit
                  ? '변경 사항 저장하기'
                  : '인물 완성하고 대화 준비하기'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
