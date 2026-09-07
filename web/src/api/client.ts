import createFetchClient from "openapi-fetch";
import type { Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";
import { safeParse } from "valibot";

import { ApiError, problemSchema } from "~/api/error.ts";
import type { Problem } from "~/api/error.ts";
import type { paths } from "~/api/schema.gen.ts";
import { session } from "~/auth/session.ts";

const auth: Middleware = {
  onRequest({ request }) {
    const apiKey = session.get();

    if (apiKey !== null) {
      request.headers.set("Authorization", `Bearer ${apiKey}`);
    }

    return request;
  },
  async onResponse({ response }) {
    if (response.ok) {
      return response;
    }

    const problem = await readProblem(response);
    const error = new ApiError(
      response.status,
      problem,
      `${response.status} ${response.statusText}`,
    );

    if (error.unauthorized) {
      session.clear();
    }

    throw error;
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
fetchClient.use(auth);

/**
 * TanStack Query bindings over the typed client. `api.queryOptions` feeds route loaders,
 * `api.useQuery`/`api.useMutation` feed components; both key queries by `[method, path, init]` so
 * invalidation works by path.
 */
export const api = createQueryClient(fetchClient);
