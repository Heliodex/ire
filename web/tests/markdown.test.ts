import { describe, expect, test } from "bun:test"
import { parseMarkdown } from "../src/lib/markdown"

describe("parseMarkdown", () => {
	test("groups headings, paragraphs, lists and quotes", () => {
		const blocks = parseMarkdown("# Title\n\nHello **world**.\n\n- one\n- two\n\n> note\n")

		expect(blocks.map(block => block.kind)).toEqual(["heading", "paragraph", "list", "quote"])
		expect(blocks[0]).toMatchObject({ level: 1, text: "Title" })
		expect(blocks[1].text).toBe("Hello world.")
		expect(blocks[2].items).toEqual(["one", "two"])
		expect(blocks[3].text).toBe("note")
	})

	test("keeps fenced code verbatim", () => {
		const blocks = parseMarkdown("```ts\nconst a = 1 < 2\n```\n")

		expect(blocks).toHaveLength(1)
		expect(blocks[0]).toMatchObject({ kind: "code", language: "ts", text: "const a = 1 < 2" })
	})

	test("never keeps raw HTML or image syntax", () => {
		const blocks = parseMarkdown("<script>alert(1)</script>\n\n![board](https://example.com/b.png)")

		expect(blocks.every(block => block.kind !== "code")).toBe(true)
		const text = blocks.map(block => block.text).join(" ")

		expect(text).not.toContain("<script>")
		expect(text).not.toContain("![")
	})
})
