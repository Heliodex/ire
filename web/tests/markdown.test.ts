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

	test("renders HTML img tags as images and strips other tags", () => {
		const blocks = parseMarkdown(
			'<img src="https://example.com/a.png" alt="First">\n\n<img width="10" src="https://example.com/b.png">\n\n<script>alert(1)</script>',
		)
		const images = blocks.filter(block => block.kind === "image")

		expect(images).toHaveLength(2)
		expect(images[0]).toMatchObject({ image: { alt: "First", url: "https://example.com/a.png" } })
		expect(images[1]?.image?.url).toBe("https://example.com/b.png")
		expect(blocks.map(block => block.text).join(" ")).not.toContain("<script>")
	})

	test("keeps adjacent img tags on one line as separate image blocks", () => {
		const blocks = parseMarkdown('<img src="https://example.com/a.png"><img src="https://example.com/b.png">')

		expect(blocks.map(block => block.kind)).toEqual(["image", "image"])
		expect(blocks[0].image?.url).toBe("https://example.com/a.png")
		expect(blocks[1].image?.url).toBe("https://example.com/b.png")
	})

	test("keeps text around images as a paragraph", () => {
		const blocks = parseMarkdown('See <img src="https://example.com/a.png"> here')

		expect(blocks).toHaveLength(1)
		expect(blocks[0].kind).toBe("paragraph")
		expect(blocks[0].inline?.filter(node => typeof node !== "string")).toHaveLength(1)
	})

	test("splits image-only lines out of the surrounding paragraph", () => {
		const blocks = parseMarkdown(
			'Works.\n<img src="https://example.com/a.png"><img src="https://example.com/b.png">\nCaption\n\nAfter',
		)

		expect(blocks.map(block => block.kind)).toEqual(["paragraph", "image", "image", "paragraph", "paragraph"])
		expect(blocks[0].text).toBe("Works.")
		expect(blocks[3].text).toBe("Caption")
	})

	test("decodes entities in img sources", () => {
		const blocks = parseMarkdown('<img src="https://example.com/a?x=1&amp;y=2" alt="q">')

		expect(blocks).toHaveLength(1)
		expect(blocks[0]).toMatchObject({
			image: { alt: "q", url: "https://example.com/a?x=1&y=2" },
			kind: "image",
		})
	})

	test("resolves relative image URLs against the base", () => {
		const base = "https://raw.githubusercontent.com/o/r/main/"
		const blocks = parseMarkdown("![a](./docs/pic.png)\n\n![b](/root.png)\n\n![c](https://example.com/x.png)", base)
		const images = blocks.filter(block => block.kind === "image")

		expect(images.map(image => image.image?.url)).toEqual([
			"https://raw.githubusercontent.com/o/r/main/docs/pic.png",
			"https://raw.githubusercontent.com/o/r/main/root.png",
			"https://example.com/x.png",
		])
	})

	test("drops relative image URLs without a base", () => {
		const blocks = parseMarkdown("![a](./docs/pic.png)")

		expect(blocks.some(block => block.kind === "image")).toBe(false)
	})

	test("supports angle-bracket image URLs with spaces", () => {
		const blocks = parseMarkdown("![shot](<./my photo.png>)", "https://raw.githubusercontent.com/o/r/main/")

		expect(blocks).toHaveLength(1)
		expect(blocks[0].image?.url).toBe("https://raw.githubusercontent.com/o/r/main/my%20photo.png")
	})
})
