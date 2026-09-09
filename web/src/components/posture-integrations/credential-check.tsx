import { Button } from "@cloudflare/kumo/components/button";
import { useRef, useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegrationCheckBody } from "~/api/schema.gen.ts";
import { Note, Status } from "~/components/ui/status.tsx";

type TestStatus =
  | { readonly kind: "idle" }
  | { readonly kind: "testing" }
  | { readonly kind: "success" }
  | { readonly kind: "error"; readonly message: string };

/**
 * What the check needs of the mutation: a call that reports back. Naming the shape instead of the
 * mutation keeps the rule about stale results testable without a query client.
 */
export interface CheckRequest {
  readonly mutate: (
    variables: { readonly body: PostureIntegrationCheckBody },
    handlers: {
      readonly onSuccess: () => void;
      readonly onError: (error: unknown) => void;
    },
  ) => void;
}

export interface CredentialCheck {
  readonly status: TestStatus;
  /** Forgets the result, because the credentials it belongs to are no longer the ones on screen. */
  readonly reset: () => void;
  readonly test: (body: PostureIntegrationCheckBody) => void;
}

/**
 * The "Test connection" button's state. A result belongs to the credentials it was sent with: every
 * edit and every new test supersedes what is in flight, so a late answer cannot report a draft it
 * never saw as verified.
 */
export function useCredentialCheck(check: CheckRequest): CredentialCheck {
  const [status, setStatus] = useState<TestStatus>({ kind: "idle" });
  const attempt = useRef(0);

  const reset = (): void => {
    attempt.current += 1;
    setStatus({ kind: "idle" });
  };

  const test = (body: PostureIntegrationCheckBody): void => {
    attempt.current += 1;
    const tested = attempt.current;

    setStatus({ kind: "testing" });
    check.mutate(
      { body },
      {
        onSuccess: () => {
          if (tested === attempt.current) {
            setStatus({ kind: "success" });
          }
        },
        onError: (error) => {
          if (tested === attempt.current) {
            setStatus({ kind: "error", message: errorMessage(error) });
          }
        },
      },
    );
  };

  return { status, reset, test };
}

export function TestConnection({
  status,
  disabled,
  onTest,
}: {
  readonly status: TestStatus;
  readonly disabled: boolean;
  readonly onTest: () => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="secondary"
          size="sm"
          loading={status.kind === "testing"}
          disabled={disabled}
          onClick={onTest}
        >
          Test connection
        </Button>
        {status.kind === "success" ? <Status tone="success">Connection verified</Status> : null}
      </div>
      {status.kind === "error" ? <Note tone="danger">{status.message}</Note> : null}
    </div>
  );
}
