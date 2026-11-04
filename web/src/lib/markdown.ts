// A deliberately small Markdown parser: it returns text blocks, never HTML, so a repository README cannot inject markup into the page.

export type MarkdownKind = "heading" | "paragraph" | "list" | "code" | "quote"

export interface MarkdownBlock {
	items: string[]
	kind: MarkdownKind
	language: string
	level: number
	text: string
}

// parseMarkdown groups a README into headings, paragraphs, lists, quotes and fenced code.
export function parseMarkdown(source: string): MarkdownBlock[] {
	const parser = new MarkdownParser()

	for (const line of source.split("\n")) parser.line(line)

	parser.finish()

	return parser.blocks
}

// inline removes the syntax that cannot be styled without rendering arbitrary HTML.
function inline(value: string): string {
	return value
		.replaceAll(/!\[[^\]]*\]\([^)]+\)/g, "")
		.replaceAll(/\[([^\]]+)\]\([^)]+\)/g, "$1")
		.replaceAll(/<[^>]*>/g, "")
		.replaceAll(/[*_`]/g, "")
		.replaceAll(/\s+/g, " ")
		.trim()
}

// MarkdownParser holds the block and inline state for one document.
class MarkdownParser {
	blocks: MarkdownBlock[] = []
	private paragraph: string[] = []
	private list: string[] = []
	private code: string[] = []
	private language = ""
	private inCode = false

	// line consumes one source line and appends any finished block.
	line(raw: string): void {
		if (raw.trimStart().startsWith("```")) {
			this.fence(raw)
			return
		}

		if (this.inCode) {
			this.code.push(raw)
			return
		}

		const heading = /^(#{1,6})\s+(.*)$/.exec(raw)
		if (heading) {
			this.flush()
			this.blocks.push({
				items: [],
				kind: "heading",
				language: "",
				level: heading[1].length,
				text: inline(heading[2]),
			})
			return
		}

		const quote = /^\s*>\s?(.*)$/.exec(raw)
		if (quote) {
			this.flush()
			this.blocks.push({ items: [], kind: "quote", language: "", level: 0, text: inline(quote[1]) })
			return
		}

		const item = /^\s*(?:[-*+]|\d+\.)\s+(.*)$/.exec(raw)
		if (item) {
			if (this.paragraph.length > 0) this.flush()
			this.list.push(inline(item[1]))
			return
		}

		if (raw.trim() === "") {
			this.flush()
			return
		}

		this.paragraph.push(raw.trim())
	}

	// finish closes the block left open at the end of the document.
	finish(): void {
		this.flush()

		if (this.inCode)
			this.blocks.push({ items: [], kind: "code", language: this.language, level: 0, text: this.code.join("\n") })
	}

	// fence opens or closes a fenced code block.
	private fence(raw: string): void {
		this.flush()

		if (this.inCode) {
			this.blocks.push({ items: [], kind: "code", language: this.language, level: 0, text: this.code.join("\n") })
			this.code = []
			this.language = ""
		} else this.language = raw.trim().slice(3).trim()

		this.inCode = !this.inCode
	}

	// flush appends the pending paragraph and list.
	private flush(): void {
		if (this.paragraph.length > 0) {
			this.blocks.push({
				items: [],
				kind: "paragraph",
				language: "",
				level: 0,
				text: inline(this.paragraph.join(" ")),
			})
			this.paragraph = []
		}

		if (this.list.length > 0) {
			this.blocks.push({ items: this.list, kind: "list", language: "", level: 0, text: "" })
			this.list = []
		}
	}
}
