import type { UseMutationResult } from "@tanstack/react-query";
import type { FetchResponse, MaybeOptionalInit } from "openapi-fetch";
import type { MethodResponse } from "openapi-react-query";
import type { HttpMethod, MediaType, PathsWithMethod } from "openapi-typescript-helpers";

import type { api } from "~/api/client.ts";
import type { paths } from "~/api/schema.gen.ts";

/**
 * What a mutation resolves to. An operation that answers 204 with no body has no response type to
 * name, which reads as `never`; the hook hands back `undefined` for one of those.
 */
type MutationData<Method extends HttpMethod, Path extends PathsWithMethod<paths, Method>> = [
  MethodResponse<typeof api, Method, Path>,
] extends [never]
  ? undefined
  : MethodResponse<typeof api, Method, Path>;

/**
 * The result of `api.useMutation(method, path)`, nameable in interfaces. The declared error is the
 * OpenAPI problem shape; at runtime the client middleware throws an `ApiError` carrying it, so read
 * errors through `errorMessage`.
 */
export type Mutation<
  Method extends HttpMethod,
  Path extends PathsWithMethod<paths, Method>,
> = UseMutationResult<
  MutationData<Method, Path>,
  Required<
    FetchResponse<
      NonNullable<paths[Path][Method]>,
      MaybeOptionalInit<paths[Path], Method>,
      MediaType
    >
  >["error"],
  MaybeOptionalInit<paths[Path], Method>
>;
