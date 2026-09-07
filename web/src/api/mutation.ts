import type { UseMutationResult } from "@tanstack/react-query";
import type { FetchResponse, MaybeOptionalInit } from "openapi-fetch";
import type { MethodResponse } from "openapi-react-query";
import type { HttpMethod, MediaType, PathsWithMethod } from "openapi-typescript-helpers";

import type { api } from "~/api/client.ts";
import type { paths } from "~/api/schema.gen.ts";

/**
 * The result of `api.useMutation(method, path)`, nameable in interfaces. The declared error is the
 * OpenAPI problem shape; at runtime the client middleware throws an `ApiError` carrying it, so read
 * errors through `errorMessage`.
 */
export type Mutation<
  Method extends HttpMethod,
  Path extends PathsWithMethod<paths, Method>,
> = UseMutationResult<
  MethodResponse<typeof api, Method, Path>,
  Required<
    FetchResponse<
      NonNullable<paths[Path][Method]>,
      MaybeOptionalInit<paths[Path], Method>,
      MediaType
    >
  >["error"],
  MaybeOptionalInit<paths[Path], Method>
>;
