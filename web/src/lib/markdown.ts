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
// imageBase resolves relative image URLs (for example a README's `./docs/photo.png`); without it only absolute URLs become images.
export function parseMarkdown(source: string, imageBase = ""): MarkdownBlock[] {
	const parser = new MarkdownParser(imageBase)

	for (const line of source.split("\n")) parser.line(line)

	parser.finish()

	return parser.blocks
}

// githubRawBase returns the raw-file base a README's relative image URLs resolve against.
export function githubRawBase(fullName: string, ref?: string): string | undefined {
	const name = fullName.trim()
	if (name === "") return undefined
	const trimmedRef = (ref ?? "").trim()
	const branch = trimmedRef === "" ? "HEAD" : trimmedRef
	return `https://raw.githubusercontent.com/${name}/${branch}/`
}

function decodeEntities(value: string): string {
	return value.replaceAll("&amp;", "&")
}

// resolveImageURL keeps absolute HTTP(S) image URLs and resolves relative ones against imageBase.
function resolveImageURL(value: string, imageBase: string): string | undefined {
	const url = decodeEntities(value).trim()
	if (url === "" || url.startsWith("#")) return undefined
	if (/^https?:\/\//i.test(url)) return url
	if (/^[a-z][a-z0-9+.-]*:/i.test(url)) return undefined
	if (imageBase === "") return undefined
	// READMEs live at the repo root, so a leading ./ or / still means the repo root.
	const path = url.replace(/^(?:\.\/)+/, "").replace(/^\/+/, "")
	if (path === "" || path.startsWith("#")) return undefined
	try {
		return new URL(path, imageBase).href
	} catch {
		return undefined
	}
}

function imageURLTarget(value: string): string {
	const angle = /^<([^<>]+)>$/.exec(value.trim())
	return angle ? (angle[1] ?? "") : value
}

function attribute(tag: string, name: string): string {
	const match = new RegExp(`${name}\\s*=\\s*("[^"]*"|'[^']*'|[^\\s>]+)`, "i").exec(tag)
	if (!match) return ""
	const raw = match[1] ?? ""
	const quoted = /^"([\s\S]*)"$/.exec(raw) ?? /^'([\s\S]*)'$/.exec(raw)
	return decodeEntities(quoted ? (quoted[1] ?? "") : raw).trim()
}

// imgTagImage reads one HTML img tag; every other tag stays stripped.
function imgTagImage(tag: string, imageBase: string): MarkdownInlineImage | undefined {
	const url = resolveImageURL(imageURLTarget(attribute(tag, "src")), imageBase)
	if (url === undefined) return undefined
	return { alt: cleanText(attribute(tag, "alt")), url }
}

function inlineImage(line: string, imageBase: string): MarkdownInlineImage | undefined {
	const trimmed = line.trim()
	const img = /^<img\b[^>]*>$/i.exec(trimmed)
	if (img) return imgTagImage(img[0] ?? "", imageBase)
	const match = /^!\[([^\]]*)\]\((<[^<>]+>|[^)]+?)(?:\s+"[^"]*")?\)$/.exec(trimmed)
	if (!match) return undefined
	const url = resolveImageURL(imageURLTarget(match[2] ?? ""), imageBase)
	if (url === undefined) return undefined
	return { alt: match[1] ?? "", url }
}

// inline parses safe inline content: images and img tags become image nodes, links become their text, and other HTML/formatting markers are stripped.
function inline(value: string, imageBase: string): MarkdownInline[] {
	const nodes: MarkdownInline[] = []
	const pattern = /!\[([^\]]*)\]\((<[^<>]+>|[^)]+?)(?:\s+"[^"]*")?\)|\[([^\]]+)\]\([^)]+\)|<img\b[^>]*>|<[^>]*>/gi
	let last = 0
	for (const match of value.matchAll(pattern)) {
		const index = match.index ?? 0
		const text = cleanText(value.slice(last, index))
		if (text !== "") nodes.push(text)
		if (match[0].startsWith("![")) {
			const url = resolveImageURL(imageURLTarget(match[2] ?? ""), imageBase)
			if (url !== undefined) nodes.push({ alt: match[1] ?? "", url })
		} else if (/^<img\b/i.test(match[0])) {
			const image = imgTagImage(match[0], imageBase)
			if (image !== undefined) nodes.push(image)
		} else if (match[3] !== undefined) {
			const linkText = cleanText(match[3])
			if (linkText !== "") nodes.push(linkText)
		}
		last = index + match[0].length
	}
	const tail = cleanText(value.slice(last))
	if (tail !== "") nodes.push(tail)
	if (nodes.length === 0) {
		const cleaned = cleanText(value)
		return cleaned ? [cleaned] : []
	}
	return nodes
}

function cleanText(value: string): string {
	return value.replaceAll(/[*_`]/g, "").replaceAll(/\s+/g, " ").trim()
}

function inlineText(value: string, imageBase = ""): string {
	return inline(value, imageBase)
		.map((node): string => (typeof node === "string" ? node : node.alt))
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

	constructor(private readonly imageBase = "") {}

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

		const image = inlineImage(raw, this.imageBase)
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
				text: inlineText(heading[2], this.imageBase),
			})
			return
		}

		const quote = /^\s*>\s?(.*)$/.exec(raw)
		if (quote) {
			this.flush()
			this.blocks.push({ items: [], kind: "quote", language: "", level: 0, text: inlineText(quote[1], this.imageBase) })
			return
		}

		const item = /^\s*(?:[-*+]|\d+\.)\s+(.*)$/.exec(raw)
		if (item) {
			if (this.paragraph.length > 0) this.flush()
			this.list.push(inlineText(item[1], this.imageBase))
			return
		}

		if (raw.trim() === "") {
			this.flush()
			return
		}

		// A line of only images becomes one block per image, so each renders on its own row
		// instead of merging into the surrounding paragraph.
		const nodes = inline(raw.trim(), this.imageBase)
		if (nodes.length > 0 && nodes.every(node => typeof node !== "string")) {
			this.flush()
			for (const node of nodes)
				this.blocks.push({ image: node, items: [], kind: "image", language: "", level: 0, text: node.alt })
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
			const nodes = inline(text, this.imageBase)
			// A line of only images becomes one block per image, so each renders on its own row.
			if (nodes.length > 0 && nodes.every(node => typeof node !== "string"))
				for (const node of nodes)
					this.blocks.push({ image: node, items: [], kind: "image", language: "", level: 0, text: node.alt })
			else
				this.blocks.push({
					inline: nodes,
					items: [],
					kind: "paragraph",
					language: "",
					level: 0,
					text: inlineText(text, this.imageBase),
				})

			this.paragraph = []
		}

		if (this.list.length > 0) {
			this.blocks.push({ items: this.list, kind: "list", language: "", level: 0, text: "" })
			this.list = []
		}
	}
}
