import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

const user = "jane.doe";
// What types.AuthID takes after its prefix.
const authIdHexLength = 24;
const desktop = 1280;
const phone = 390;
const machines = ["e2e-bulk-a", "e2e-bulk-b"];

async function signIn(page: Page): Promise<void> {
  await page.goto("/console/login");
  await page.getByRole("button", { name: /continue with/iu }).click();
  await expect(page).toHaveURL(/\/console\/?$/u);
}

/** Adds a machine owned by the signed-in user, the way the dev seed does. */
async function register(page: Page, name: string): Promise<void> {
  // A cookie-authenticated write has to come from the console's own origin, as a page's would.
  const headers = { Origin: new URL(page.url()).origin };
  const key = `hskey-authreq-${crypto.randomUUID().replaceAll("-", "").slice(0, authIdHexLength)}`;

  // debug/node only caches the registration; node/register turns it into a row.
  const cached = await page.request.post("/api/v1/debug/node", {
    headers,
    data: { user, key, name },
  });
  expect(cached.ok(), await cached.text()).toBe(true);

  const registered = await page.request.post(`/api/v1/node/register?user=${user}&key=${key}`, {
    headers,
  });
  expect(registered.ok(), await registered.text()).toBe(true);
}

test.describe("machines bulk selection", () => {
  test.beforeAll(async ({ browser }) => {
    const page = await browser.newPage();
    await signIn(page);

    await Promise.all(machines.map((name) => register(page, name)));

    await page.close();
  });

  test.beforeEach(async ({ page }) => {
    await signIn(page);
  });

  for (const width of [desktop, phone]) {
    test(`ticking a row leaves the rows in place at ${width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/console/machines");

      const first = page.getByRole("checkbox", { name: /^Select e2e-bulk-a/u });
      const second = page.getByRole("checkbox", { name: /^Select e2e-bulk-b/u });
      const before = await second.boundingBox();

      if (before === null) {
        throw new Error("the second machine's tick box is not on the page");
      }

      await first.click();
      await expect(first).toBeChecked();
      expect(await second.boundingBox()).toEqual(before);
      await expect(
        page.getByRole("toolbar", { name: "Actions for the selected machines" }),
      ).toBeVisible();

      // A quick second click lands where the pointer already is, so it must still find the box
      // it aimed at rather than the row above, which would open that machine instead.
      await page.mouse.click(before.x + before.width / 2, before.y + before.height / 2);
      await expect(second).toBeChecked();
      await expect(page).toHaveURL(/\/console\/machines$/u);

      await page.screenshot({ path: testInfo.outputPath("selected.png") });
    });
  }
});
