import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { RoleBadge } from "~/components/users/role-badge.tsx";
import { userRoles } from "~/components/users/roles.ts";

function tint(chip: Element | null): string {
  return chip instanceof Element ? getComputedStyle(chip).backgroundColor : "";
}

describe(RoleBadge, () => {
  // Roles are outline badges, as the dashboard's plan badge is; a member, the default, is grey.
  it("names the role, with a member set apart from the elevated roles", async () => {
    const view = await render(
      <>
        {userRoles.map((role) => (
          <RoleBadge key={role} role={role} />
        ))}
      </>,
    );

    const chips = [...view.container.querySelectorAll("[data-role]")];
    const tints = new Set(chips.map((chip) => tint(chip.firstElementChild)));

    expect(chips.map((chip) => chip.textContent)).toStrictEqual([
      "Owner",
      "Admin",
      "Network admin",
      "IT admin",
      "Auditor",
      "Member",
    ]);
    expect(tints.size).toBe(2);
    expect(tint(view.getByText("Member").element())).not.toBe(
      tint(view.getByText("Owner").element()),
    );
  });

  it("treats an unknown or empty role as a member", async () => {
    const unknown = "";
    const view = await render(<RoleBadge role={unknown} />);
    const chip = view.container.querySelector('[data-role="member"]');

    expect(chip?.textContent).toBe("Member");
  });
});
