import {
	authConfigSchema,
	meResponseSchema,
	queueResponseSchema,
} from "../web/src/lib/contract"

export interface EndpointParam {
	default?: string
	in: "path" | "query" | "body"
	map?: { [_: string]: string }
	name: string
	optional?: boolean
	type: string
	value?: string
	when?: string
}

export interface Endpoint {
	doc?: string
	method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE"
	name: string
	params?: EndpointParam[]
	path: string
	response: "void" | { schema: unknown; select?: string[] }
}

// The endpoint registry is the second half of the contract: schemas describe the shapes,
// entries describe how to call them. `wt contract` turns each entry into a typed fetch
// wrapper in web/src/lib/api.gen.ts. Keep the doc lines – they become the code comments.
export const endpoints: Endpoint[] = Object.freeze([
	{
		doc: "fetchAuthConfig returns which sign-in methods are enabled.",
		method: "GET",
		name: "fetchAuthConfig",
		path: "/api/v1/auth/config",
		response: { schema: authConfigSchema },
	},
	{
		doc: "fetchMe returns the signed-in reviewer, throwing ApiError 401 when there is no session.",
		method: "GET",
		name: "fetchMe",
		path: "/api/v1/me",
		response: { schema: meResponseSchema },
	},
	{
		doc: "fetchQueue returns one page of the stage 1 work list. A sort key orders every page server-side so appended rows keep arriving at the end.",
		method: "GET",
		name: "fetchQueue",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ default: '"all"', in: "query", name: "filter", type: "string" },
			{ default: "1", in: "query", name: "stage", type: "number" },
			{ default: "0", in: "query", name: "offset", type: "number" },
			{ default: "50", in: "query", name: "limit", type: "number" },
			{ default: "null", in: "query", name: "sort", optional: true, type: "string | null" },
			{
				default: "1",
				in: "query",
				map: { "-1": "desc", "1": "asc" },
				name: "dir",
				type: "1 | -1",
				when: "sort",
			},
		],
		path: "/api/v1/programs/{program}/queue",
		response: { schema: queueResponseSchema },
	},
])
