import { describe, expect, test } from "bun:test"
import { ApiError } from "../src/lib/api-error"
import { ignore, sleep } from "../src/lib/async"
import { makerHref, reviewerHref } from "../src/lib/reviewer"
import { ValidationError } from "../src/lib/validation-error"

describe("reviewerHref", () => {
	test("links identifiers to the profile page", () => {
		expect(reviewerHref("ada@example.com")).toBe("/reviewers/ada%40example.com")
		expect(reviewerHref("  ada  ")).toBe("/reviewers/ada")
	})

	test("returns null for missing identifiers", () => {
		expect(reviewerHref(null)).toBeNull()
		expect(reviewerHref(undefined)).toBeNull()
		expect(reviewerHref("   ")).toBeNull()
	})
})

describe("makerHref", () => {
	test("links emails to the maker page", () => {
		expect(makerHref("maker@example.com")).toBe("/makers/maker%40example.com")
		expect(makerHref("  maker@example.com  ")).toBe("/makers/maker%40example.com")
	})

	test("returns null for missing emails", () => {
		expect(makerHref(null)).toBeNull()
		expect(makerHref(undefined)).toBeNull()
		expect(makerHref("")).toBeNull()
	})
})

describe("ApiError", () => {
	test("carries message, code and field", () => {
		const error = new ApiError("Nope", "invalid", "email")

		expect(error).toBeInstanceOf(Error)
		expect(error.name).toBe("ApiError")
		expect(error.code).toBe("invalid")
		expect(error.field).toBe("email")
	})

	test("defaults code and field", () => {
		const error = new ApiError("Nope")

		expect(error.code).toBe("")
		expect(error.field).toBe("")
	})
})

describe("ValidationError", () => {
	test("uses the first field message and keeps every field", () => {
		const error = new ValidationError({ email: "Bad email", name: "Bad name" })

		expect(error.message).toBe("Bad email")
		expect(error.fields).toEqual({ email: "Bad email", name: "Bad name" })
	})

	test("falls back when there are no fields", () => {
		expect(new ValidationError({}).message).toBe("Please check the form.")
	})
})

describe("async helpers", () => {
	test("sleep waits at least the requested time", async () => {
		const start = Date.now()

		await sleep(15)

		expect(Date.now() - start >= 10).toBe(true)
	})

	test("ignore swallows rejections", async () => {
		ignore(Promise.reject(new Error("background")))

		await sleep(5)
	})
})
