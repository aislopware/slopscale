import { expect, test } from "@playwright/test";

// The identity cmd/dev's mock provider signs everyone in as, listed in its
// oidc.admin_users so the console opens with admin rights.
const user = "jane.doe";
const unauthorized = 401;

test.describe("console sign-in", () => {
  test("sends a visitor to the sign-in page and through the identity provider", async ({
    page,
  }) => {
    await page.goto("/admin/machines");
    await expect(page).toHaveURL(/\/admin\/login/u);

    await page.getByRole("button", { name: /continue with/iu }).click();

    // The provider needs no credentials, so the browser lands straight back
    // where it was going, signed in.
    await expect(page).toHaveURL(/\/admin\/machines/u);

    await page.getByRole("button", { name: "Account" }).click();
    await expect(page.getByRole("menu")).toContainText(user);
  });

  test("records the sign-in in the audit log", async ({ page }) => {
    await page.goto("/admin/login");
    await page.getByRole("button", { name: /continue with/iu }).click();
    await expect(page).toHaveURL(/\/admin\/?$/u);

    await page.goto("/admin/audit");
    await expect(page.getByRole("heading", { name: "Audit log" })).toBeVisible();
    await expect(page.getByText("console.login").first()).toBeVisible();
    await expect(page.getByText(user).first()).toBeVisible();
  });

  test("signs out and drops the session", async ({ page }) => {
    await page.goto("/admin/login");
    await page.getByRole("button", { name: /continue with/iu }).click();
    await expect(page).toHaveURL(/\/admin\/?$/u);

    await page.getByRole("button", { name: "Account" }).click();
    await page.getByRole("menuitem", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/admin\/login/u);

    const whoami = await page.request.get("/api/v1/whoami");
    expect(whoami.status()).toBe(unauthorized);
  });
});
