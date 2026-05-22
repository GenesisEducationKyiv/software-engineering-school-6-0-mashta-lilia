import { test, expect } from '@playwright/test';
import { reset, seedSubscription, statusOf } from '../fixtures/seed';

test.describe('Unsubscribe link from email', () => {
  test.beforeEach(async () => {
    await reset();
  });

  test('opening the unsubscribe URL deactivates an active subscription', async ({ page }) => {
    const token = 'e2e-unsub-token-001';
    await seedSubscription({
      email: 'bob@e2e.local',
      owner: 'rust-lang',
      name: 'rust',
      token,
      status: 'active',
    });

    const response = await page.goto(`/api/unsubscribe/${token}`);
    expect(response!.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Successfully unsubscribed.');

    expect(await statusOf(token)).toBe('unsubscribed');
  });
});
