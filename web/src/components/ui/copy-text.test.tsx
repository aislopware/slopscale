import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { CopyText } from "~/components/ui/copy-text.tsx";

describe(CopyText, () => {
  // The control pulls its hover tint 4px past the text either side; a container that sizes itself
  // to the control measures the narrower margin box, and a cap at that width cut the last character
  // of every value in a definition list.
  it("shows the whole value inside a container sized to it", async () => {
    const view = await render(
      <div className="flex w-96 justify-end">
        <span className="min-w-0 overflow-clip text-ellipsis whitespace-nowrap">
          <CopyText value="http://127.0.0.1:9100/oidc" />
        </span>
      </div>,
    );
    const text = view.getByText("http://127.0.0.1:9100/oidc").element();

    expect(text.scrollWidth).toBeLessThanOrEqual(text.clientWidth);
  });
});
