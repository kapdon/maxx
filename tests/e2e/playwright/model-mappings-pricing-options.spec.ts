import { expect, test, type Page, type Route } from 'playwright/test';

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}

async function mockModelMappingApis(page: Page) {
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url());
    const { pathname } = url;

    if (pathname === '/api/admin/auth/status') {
      return json(route, { authEnabled: false });
    }

    if (pathname === '/api/admin/settings' || pathname === '/api/settings') {
      return json(route, {
        force_project_binding: 'false',
        ui_multitenant_enabled: 'false',
      });
    }

    if (pathname === '/api/admin/proxy-status' || pathname === '/api/proxy-status') {
      return json(route, {
        running: true,
        address: '127.0.0.1',
        port: 9880,
        version: 'v0.3.67',
      });
    }

    if (pathname === '/api/admin/model-prices' || pathname === '/api/model-prices') {
      return json(route, [
        {
          id: 1,
          createdAt: '2026-05-08T00:00:00Z',
          modelId: 'gpt-live-mapping-option',
          inputPriceMicro: 1000,
          outputPriceMicro: 2000,
          cacheReadPriceMicro: 0,
          cache5mWritePriceMicro: 0,
          cache1hWritePriceMicro: 0,
          has1mContext: false,
          context1mThreshold: 0,
          inputPremiumNum: 1,
          inputPremiumDenom: 1,
          outputPremiumNum: 1,
          outputPremiumDenom: 1,
        },
      ]);
    }

    if (
      pathname === '/api/admin/providers' ||
      pathname === '/api/providers' ||
      pathname === '/api/admin/routes' ||
      pathname === '/api/routes' ||
      pathname === '/api/admin/projects' ||
      pathname === '/api/projects' ||
      pathname === '/api/admin/api-tokens' ||
      pathname === '/api/api-tokens' ||
      pathname === '/api/admin/model-mappings' ||
      pathname === '/api/model-mappings' ||
      pathname === '/api/admin/response-models' ||
      pathname === '/api/response-models' ||
      pathname === '/api/admin/sessions' ||
      pathname === '/api/sessions'
    ) {
      return json(route, []);
    }

    return json(route, {});
  });
}

test('model mapping inputs include configured model-pricing options', async ({ page }) => {
  await mockModelMappingApis(page);

  await page.goto('/model-mappings', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: /model mappings/i })).toBeVisible();

  await page.getByRole('button', { name: /^Target$/ }).click();
  await page.getByPlaceholder(/search or enter custom model/i).fill('gpt-live-mapping-option');

  await expect(page.getByText('Configured Pricing')).toBeVisible();
  await expect(
    page.getByRole('button', {
      name: /gpt-live-mapping-option\s+gpt-live-mapping-option/,
    }),
  ).toBeVisible();
});
