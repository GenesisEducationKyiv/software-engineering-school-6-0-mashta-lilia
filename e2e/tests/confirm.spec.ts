import { test, expect } from '@playwright/test';
import { reset, seedSubscription, statusOf } from '../fixtures/seed';

test.describe('Confirm link from email', () => {
  test.beforeEach(async () => {
    await reset();
  });

  test('opening the confirm URL flips a pending subscription to active', async ({ page }) => {
    const token = 'e2e-confirm-token-001';
    await seedSubscription({
      email: 'alice@e2e.local',
      owner: 'golang',
      name: 'go',
      token,
      status: 'pending',
    });

    // The confirm URL is what arrives in the confirmation email. We open it
    // exactly the way a real user would click it from their inbox.
    const response = await page.goto(`/api/confirm/${token}`);
    expect(response, 'response object should be defined').not.toBeNull();
    expect(response!.status(), 'HTTP status from confirm endpoint').toBe(200);

    // The handler returns JSON. In a browser the body is rendered as text;
    // we assert on the human-readable text directly.
    await expect(page.locator('body')).toContainText('Subscription confirmed successfully.');

    expect(await statusOf(token)).toBe('active');
  });

  test('opening confirm with an unknown token returns 404', async ({ page }) => {
    const response = await page.goto('/api/confirm/this-token-does-not-exist');
    expect(response!.status()).toBe(404);
    await expect(page.locator('body')).toContainText('invalid or expired token');
  });
});
