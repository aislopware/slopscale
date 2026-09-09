import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { PostureIntegrationCheckBody } from "~/api/schema.gen.ts";
import {
  TestConnection,
  useCredentialCheck,
} from "~/components/posture-integrations/credential-check.tsx";
import type { CheckRequest } from "~/components/posture-integrations/credential-check.tsx";

/** What the mutation would call once the server answered. */
interface Answer {
  readonly onSuccess: () => void;
}

/** A check that never answers on its own, so a test decides when the server does. */
function heldCheck(answers: Answer[]): CheckRequest {
  return {
    mutate: (_variables, handlers) => {
      answers.push(handlers);
    },
  };
}

const body: PostureIntegrationCheckBody = { name: "Falcon", provider: "falcon" };

function CheckPanel({ check }: { readonly check: CheckRequest }): ReactElement {
  const credentials = useCredentialCheck(check);

  return (
    <>
      <TestConnection
        status={credentials.status}
        disabled={false}
        onTest={() => {
          credentials.test(body);
        }}
      />
      <button
        type="button"
        onClick={() => {
          credentials.reset();
        }}
      >
        Change the secret
      </button>
    </>
  );
}

describe(useCredentialCheck, () => {
  it("verifies the credentials that were tested", async () => {
    const answers: Answer[] = [];
    const screen = await render(<CheckPanel check={heldCheck(answers)} />);

    await screen.getByRole("button", { name: "Test connection" }).click();
    answers[0]?.onSuccess();

    await expect.element(screen.getByText("Connection verified")).toBeVisible();
  });

  it("drops a result the operator has already edited past", async () => {
    const answers: Answer[] = [];
    const screen = await render(<CheckPanel check={heldCheck(answers)} />);

    await screen.getByRole("button", { name: "Test connection" }).click();
    await screen.getByRole("button", { name: "Change the secret" }).click();
    answers[0]?.onSuccess();

    // The answer belongs to the credentials that were sent, not the ones now on screen.
    await expect.element(screen.getByText("Connection verified")).not.toBeInTheDocument();
  });
});
