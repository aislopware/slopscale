import createFetchClient from "openapi-fetch";
import type { Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";
import { safeParse } from "valibot";

import { ApiError, problemSchema, statusUnauthorized } from "~/api/error.ts";
import type { Problem } from "~/api/error.ts";
import type { paths } from "~/api/schema.gen.ts";
import { sessionEnded } from "~/auth/ended.ts";

/** The guards' own question; its 401 is the answer they are asking for, not news. */
const whoami = "/api/v1/whoami";

/**
 * The session cookie rides along on every same-origin request, so there is no credential to attach.
 * Errors become `ApiError`. A 401 on anything but the guards' own question means the session ended
 * under an open page, so the router is told and sends the operator back to sign-in.
 */
const problems: Middleware = {
  async onResponse({ request, response }) {
    if (response.ok) {
      return response;
    }

    if (response.status === statusUnauthorized && !new URL(request.url).pathname.endsWith(whoami)) {
      sessionEnded();
    }

    const problem = await readProblem(response);

    throw new ApiError(response.status, problem, `${response.status} ${response.statusText}`);
  },
};

async function readProblem(response: Response): Promise<Problem | undefined> {
  const contentType = response.headers.get("content-type") ?? "";

  if (!contentType.includes("json")) {
    return undefined;
  }

  try {
    const parsed = safeParse(problemSchema, await response.json());

    return parsed.success ? parsed.output : undefined;
  } catch {
    return undefined;
  }
}

/** Raw typed fetch client; use `api` for anything rendered by React. */
export const fetchClient = createFetchClient<paths>({ baseUrl: "" });
fetchClient.use(problems);

/**
 * TanStack Query bindings over the typed client. `api.queryOptions` feeds route loaders,
 * `api.useQuery`/`api.useMutation` feed components; both key queries by `[method, path, init]` so
 * invalidation works by path.
 */
export const api = createQueryClient(fetchClient);
