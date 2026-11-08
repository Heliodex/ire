// A deliberately small Markdown parser: it returns structured blocks, never raw HTML, so untrusted content cannot inject markup into the page.

export type MarkdownKind = "heading" | "paragraph" | "list" | "code" | "quote" | "image"

export interface MarkdownInlineImage {
	alt: string
	url: string
}

export type MarkdownInline = string | MarkdownInlineImage

export interface MarkdownBlock {
	image?: MarkdownInlineImage
	inline?: MarkdownInline[]
	items: string[]
	kind: MarkdownKind
	language: string
	level: number
	text: string
}

// parseMarkdown groups markdown into headings, paragraphs, lists, quotes, fenced code and images.
export function parseMarkdown(source: string): MarkdownBlock[] {
	const parser = new MarkdownParser()

	for (const line of source.split("\n")) parser.line(line)

	parser.finish()

	return parser.blocks
}

function safeImageURL(value: string): string | null {
	const url = value.trim()
	if (url.startsWith("https://") || url.startsWith("http://")) return url
	return null
}

function inlineImage(line: string): MarkdownInlineImage | null {
	const match = /^!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)$/.exec(line.trim())
	if (!match) return null
	const url = safeImageURL(match[2])
	if (!url) return null
	return { alt: match[1], url }
}

// inline parses safe inline content: image syntax becomes image nodes, links become their text, and HTML/formatting markers are stripped.
function inline(value: string): MarkdownInline[] {
	const nodes: MarkdownInline[] = []
	const pattern = /!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)|\[([^\]]+)\]\([^)]+\)|<[^>]*>/g
	let last = 0
	for (const match of value.matchAll(pattern)) {
		const index = match.index ?? 0
		const text = cleanText(value.slice(last, index))
		if (text) nodes.push(text)
		if (match[0].startsWith("![")) {
			const url = safeImageURL(match[2])
			if (url) nodes.push({ alt: match[1], url })
		} else if (match[3] != null) {
			const linkText = cleanText(match[3])
			if (linkText) nodes.push(linkText)
		}
		last = index + match[0].length
	}
	const tail = cleanText(value.slice(last))
	if (tail) nodes.push(tail)
	if (nodes.length === 0) {
		const cleaned = cleanText(value)
		return cleaned ? [cleaned] : []
	}
	return nodes
}

function cleanText(value: string): string {
	return value
		.replaceAll(/[*_`]/g, "")
		.replaceAll(/\s+/g, " ")
		.trim()
}

function inlineText(value: string): string {
	return inline(value)
		.map(node => (typeof node === "string" ? node : node.alt))
		.join(" ")
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

		const image = inlineImage(raw)
		if (image) {
			this.flush()
			this.blocks.push({ image, items: [], kind: "image", language: "", level: 0, text: image.alt })
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
				text: inlineText(heading[2]),
			})
			return
		}

		const quote = /^\s*>\s?(.*)$/.exec(raw)
		if (quote) {
			this.flush()
			this.blocks.push({ items: [], kind: "quote", language: "", level: 0, text: inlineText(quote[1]) })
			return
		}

		const item = /^\s*(?:[-*+]|\d+\.)\s+(.*)$/.exec(raw)
		if (item) {
			if (this.paragraph.length > 0) this.flush()
			this.list.push(inlineText(item[1]))
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
			const text = this.paragraph.join(" ")
			const nodes = inline(text)
			if (nodes.length === 1 && typeof nodes[0] !== "string") {
				this.blocks.push({ image: nodes[0], items: [], kind: "image", language: "", level: 0, text: nodes[0].alt })
			} else {
				this.blocks.push({
					inline: nodes,
					items: [],
					kind: "paragraph",
					language: "",
					level: 0,
					text: inlineText(text),
				})
			}
			this.paragraph = []
		}

		if (this.list.length > 0) {
			this.blocks.push({ items: this.list, kind: "list", language: "", level: 0, text: "" })
			this.list = []
		}
	}
}
