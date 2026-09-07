import createFetchClient from "openapi-fetch";
import type { Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";
import { safeParse } from "valibot";

import { ApiError, problemSchema } from "~/api/error.ts";
import type { Problem } from "~/api/error.ts";
import type { paths } from "~/api/schema.gen.ts";

/**
 * The session cookie rides along on every same-origin request, so there is no credential to attach.
 * Errors become `ApiError`; a 401 (the session ended or expired) is left to the route guards, which
 * send the operator back to sign-in.
 */
const problems: Middleware = {
  async onResponse({ response }) {
    if (response.ok) {
      return response;
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
