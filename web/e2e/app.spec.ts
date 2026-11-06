import { Page, expect, test } from '@playwright/test';

async function advanceRitualUntilVisible(page: Page, targetTestId: string, maxSteps = 14) {
  for (let i = 0; i < maxSteps; i++) {
    if (await page.getByTestId(targetTestId).isVisible().catch(() => false)) {
      return;
    }
    const skipBtn = page.getByTestId('ritual-skip-btn');
    if (await skipBtn.isVisible().catch(() => false)) {
      await skipBtn.click();
      await page.waitForTimeout(120);
      continue;
    }
    const nextBtn = page.getByTestId('ritual-next-btn');
    if (await nextBtn.isVisible().catch(() => false)) {
      await nextBtn.click();
      await page.waitForTimeout(120);
      continue;
    }
    await page.waitForTimeout(150);
  }
}

test.describe('마음 한 칸 (Emotion Garden) End-to-End Verification', () => {
  test('Release flow: character traits, Korean IME safety, A/B compare, return-to-chat, sea ritual, and cross-tab purge', async ({
    browser,
  }) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    await page.goto('/characters/new?kind=release');
    await expect(page.getByTestId('char-name-input')).toBeVisible();

    // Select custom hair & independent skin traits (CHAR01, CHAR02)
    await page.getByTestId('char-name-input').fill('박 과장');
    await page.getByTestId('hair-style-perm').click();
    await page.getByTestId('skin-tone-tone_03').click();
    await page.getByTestId('trait-freckles_level-mild').click();
    await page.getByTestId('trait-wrinkles_level-mild').click();
    await page.getByTestId('save-character-btn').click();

    // Should redirect to Home launcher with the newly created character selected
    await expect(page.getByTestId('confirm-start-release')).toBeVisible();
    await page.getByTestId('confirm-start-release').click();

    await expect(page.getByTestId('conversation-chat-view')).toBeVisible();
    const convUrl = page.url();

    // Switch to Compare mode (acknowledging first-time cost/latency modal if shown)
    await page.getByTestId('mode-compare-btn').click();
    const noticeModal = page.getByTestId('compare-notice-modal');
    if (await noticeModal.isVisible().catch(() => false)) {
      await page.getByTestId('confirm-compare-notice-btn').click();
    }
    await expect(page.getByTestId('mode-compare-btn')).toHaveAttribute('aria-pressed', 'true');

    // CHAT05: Korean IME Enter during composition must NOT prematurely send
    const composer = page.getByTestId('chat-composer');
    await composer.focus();
    await composer.fill('오늘 회의에서 내 말을 끊어서 정말 서운했어');
    await composer.evaluate((el) => {
      const evt = new KeyboardEvent('keydown', {
        key: 'Enter',
        code: 'Enter',
        keyCode: 229,
        bubbles: true,
        cancelable: true,
        isComposing: true,
      });
      el.dispatchEvent(evt);
    });
    // Text must remain unsent in composer during IME composition
    await expect(composer).toHaveValue('오늘 회의에서 내 말을 끊어서 정말 서운했어');

    // Focus composer and press normal Enter to submit the turn
    await composer.focus();
    await composer.press('Enter');
    await expect(composer).toHaveValue('');

    // Candidate A & B appear and finish streaming via Mock adapters (AI02)
    await expect(page.getByTestId('candidate-card-a')).toBeVisible();
    await expect(page.getByTestId('candidate-card-b')).toBeVisible();
    await expect(page.getByTestId('accept-run-a')).toBeVisible();
    await page.getByTestId('accept-run-a').click();

    // Assistant reply promoted to canonical timeline (1: opening, 2: user, 3: assistant)
    await expect(page.getByTestId('message-assistant-3')).toBeVisible();

    // Open a second tab on the same conversation to verify BroadcastChannel purge eviction (DATA04)
    const tab2 = await context.newPage();
    await tab2.goto(convUrl);
    await expect(tab2.getByTestId('conversation-chat-view')).toBeVisible();

    // Begin envelope closure -> pack starts -> skip -> return-to-chat (RIT01)
    await page.getByTestId('begin-closure-btn').click();
    await expect(page.getByTestId('ritual-controller')).toBeVisible();
    await advanceRitualUntilVisible(page, 'return-to-chat-btn', 3);
    await page.getByTestId('return-to-chat-btn').click();
    await expect(page.getByTestId('conversation-chat-view')).toBeVisible();
    await expect(page.getByTestId('message-assistant-3')).toBeVisible();

    // Begin closure again -> complete common steps -> choose sea route -> complete 4 steps -> purge (RIT02)
    await page.getByTestId('begin-closure-btn').click();
    await expect(page.getByTestId('ritual-controller')).toBeVisible();

    await advanceRitualUntilVisible(page, 'route-selector');
    await expect(page.getByTestId('select-route-sea')).toBeVisible();
    await page.getByTestId('select-route-sea').click();

    await advanceRitualUntilVisible(page, 'ritual-complete-btn');
    await expect(page.getByTestId('ritual-complete-btn')).toBeVisible();
    await page.getByTestId('ritual-complete-btn').click();

    // Both Tab 1 and Tab 2 must show the serene purged view with zero raw messages (DATA04)
    await expect(page.getByTestId('purged-completion-view')).toBeVisible();
    await expect(tab2.getByTestId('purged-completion-view')).toBeVisible();

    // Reloading the purged conversation URL returns 410 -> serene purged view
    await page.goto(convUrl);
    await expect(page.getByTestId('purged-completion-view')).toBeVisible();
    await expect(page.locator('body')).not.toContainText('내 말을 끊어서 정말 서운했어');

    // Comparisons page lists metadata-only stats without raw conversation text (DATA02)
    await page.goto('/settings/comparisons');
    await expect(page.getByTestId('comparison-item').first()).toBeVisible();
    await expect(page.locator('body')).not.toContainText('내 말을 끊어서 정말 서운했어');

    await context.close();
  });

  test('Grow flow: flower gift, kept_line, 5-step human planting, and 320px 6-column mobile garden', async ({
    page,
  }) => {
    await page.goto('/characters/new?kind=grow');
    await page.getByTestId('char-name-input').fill('숲지기 윤');
    await page.getByTestId('hair-style-braid').click();
    await page.getByTestId('save-character-btn').click();

    await expect(page.getByTestId('confirm-start-grow')).toBeVisible();
    await page.getByTestId('confirm-start-grow').click();

    await expect(page.getByTestId('conversation-chat-view')).toBeVisible();
    const rawSecretMessage = '진로 때문에 밤마다 마음이 조급해져서 잠이 잘 오지 않아';
    await page.getByTestId('chat-composer').fill(rawSecretMessage);
    await page.getByTestId('send-message-btn').click();

    // Wait for single-mode Mock AI reply to resolve into canonical timeline (seq=3)
    await expect(page.getByTestId('message-assistant-3')).toBeVisible();

    // Close grow conversation -> receive 1-time flower gift
    await page.getByTestId('begin-closure-btn').click();
    await expect(page.getByTestId('gift-ready-view')).toBeVisible();

    const keptLine = '조급해하지 말고 오늘 하루만큼만 걸어가기';
    await page.getByTestId('kept-line-input').fill(keptLine);
    await page.getByTestId('pick-cell-2-2').click();
    await page.getByTestId('start-planting-btn').click();

    // Walk through 5-step human planting ceremony (arrive -> dig -> plant -> cover -> water)
    await expect(page.getByTestId('ritual-controller')).toBeVisible();
    await advanceRitualUntilVisible(page, 'ritual-complete-btn');

    await expect(page.getByTestId('ritual-complete-btn')).toBeVisible();
    await page.getByTestId('ritual-complete-btn').click();
    await expect(page.getByTestId('purged-completion-view')).toBeVisible();

    // Switch viewport to 320px mobile width and inspect 6-column garden responsiveness (GARD03, GARD08, GARD11)
    await page.setViewportSize({ width: 320, height: 680 });
    await page.getByTestId('go-to-garden-btn').click();
    await expect(page.getByTestId('garden-grid')).toBeVisible();

    // Expand garden to 6 columns x 4 rows and confirm zero horizontal document overflow at 320px
    await page.getByTestId('garden-cols-input').fill('6');
    await page.getByTestId('garden-rows-input').fill('4');
    await page.getByTestId('garden-resize-btn').click();
    await expect(page.getByTestId('garden-cell-4-6')).toBeVisible();

    const overflowPx = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(overflowPx).toBeLessThanOrEqual(1);

    // Inspect planted flower at (2, 2): shows kept_line + Asia/Seoul date, never raw conversation text
    await page.getByTestId('garden-cell-2-2').click();
    await expect(page.getByTestId('flower-detail-drawer')).toBeVisible();
    await expect(page.getByTestId('flower-kept-line')).toContainText(keptLine);
    await expect(page.getByTestId('flower-planted-date')).toContainText(/\d{4}\.\s*\d{2}\.\s*\d{2}/);
    await expect(page.locator('body')).not.toContainText(rawSecretMessage);
  });
});
