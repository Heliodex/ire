import { type GenericSchema, safeParse } from "valibot"
import { ApiError } from "./api-error"
import { errorResponseSchema } from "./contract"

export const program = "half-life"

// parseJSON parses a response through the contract schema, including API errors. Exported for the generated client.
export async function parseJSON<T>(response: Response, schema: GenericSchema<T>): Promise<T> {
	const data: unknown = await response.json().catch(() => null)

	if (!response.ok) {
		const error = safeParse(errorResponseSchema, data)
		if (error.success) throw new ApiError(error.output.error, error.output.code, error.output.field ?? "")
		throw new ApiError(`The review API returned ${response.status}`, "http_error")
	}

	const parsed = safeParse(schema, data)
	if (!parsed.success) throw new Error("The review API returned an unexpected response shape.")

	return parsed.output
}

// requestVoid performs a request whose success has no body. Exported for the generated client.
export async function requestVoid(path: string, init: RequestInit): Promise<void> {
	const response = await fetch(path, init)
	if (response.ok) return

	const data: unknown = await response.json().catch(() => null)
	const error = safeParse(errorResponseSchema, data)
	if (error.success) throw new ApiError(error.output.error, error.output.code, error.output.field ?? "")
	throw new ApiError(`The review API returned ${response.status}`, "http_error")
}
