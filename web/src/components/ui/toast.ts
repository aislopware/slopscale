import { createKumoToastManager } from "@cloudflare/kumo/components/toast";

import { errorMessage } from "~/api/error.ts";

/** One manager for the app so mutations outside the React tree can notify too. */
export const toastManager = createKumoToastManager();

const successTimeoutMs = 4000;
const errorTimeoutMs = 8000;

export const toast = {
  success(title: string, description?: string): void {
    toastManager.add({
      title,
      variant: "success",
      timeout: successTimeoutMs,
      ...(description === undefined ? {} : { description }),
    });
  },
  /** `cause` may be anything a request rejected with; it is rendered through {@link errorMessage}. */
  error(title: string, cause?: unknown): void {
    toastManager.add({
      title,
      variant: "error",
      timeout: errorTimeoutMs,
      ...(cause === undefined ? {} : { description: errorMessage(cause) }),
    });
  },
};
