import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { RoleBadge } from "~/components/users/role-badge.tsx";
import { userRoles } from "~/components/users/roles.ts";

function tint(chip: Element | null): string {
  return chip instanceof Element ? getComputedStyle(chip).backgroundColor : "";
}

describe(RoleBadge, () => {
  it("names the role and gives every role a tint of its own", async () => {
    const view = await render(
      <>
        {userRoles.map((role) => (
          <RoleBadge key={role} role={role} />
        ))}
      </>,
    );

    const chips = [...view.container.querySelectorAll("[data-role]")];
    const tints = new Set(chips.map((chip) => tint(chip)));

    expect(chips.map((chip) => chip.textContent)).toStrictEqual([
      "Owner",
      "Admin",
      "Network admin",
      "IT admin",
      "Auditor",
      "Member",
    ]);
    expect(tints.size).toBe(userRoles.length);
  });

  it("treats an unknown or empty role as a member", async () => {
    const unknown = "";
    const view = await render(<RoleBadge role={unknown} />);
    const chip = view.container.querySelector('[data-role="member"]');

    expect(chip?.textContent).toBe("Member");
  });
});
