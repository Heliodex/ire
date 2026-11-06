// iconPaths keeps the inline stroke icons in one place. Each entry is a list of path data strings so Icon.svelte can render them without injecting markup.
export const iconPaths = {
	arrowR: ["M5 12h14M13 6l6 6-6 6"],
	check: ["M5 13l4 4L19 7"],
	checkCircle: ["M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0", "M8.5 12l2.3 2.3L15.5 9.8"],
	chevD: ["M6 9l6 6 6-6"],
	clock: ["M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0", "M12 7.5V12l3 1.8"],
	config: [
		"M4 6h9M17 6h3M4 12h3M11 12h9M4 18h13M21 18h-1",
		"M17 6a2 2 0 1 1-4 0 2 2 0 0 1 4 0",
		"M11 12a2 2 0 1 1-4 0 2 2 0 0 1 4 0",
		"M21 18a2 2 0 1 1-4 0 2 2 0 0 1 4 0",
	],
	info: ["M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0", "M12 11v5M12 7.6v.5"],
	lock: [
		"M6.5 10.5h11a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2v-6a2 2 0 0 1 2-2z",
		"M8 10.5V8a4 4 0 0 1 8 0v2.5",
	],
	msg: ["M21 12a8 8 0 0 1-11.6 7.1L4 20l1-5A8 8 0 1 1 21 12z"],
	refresh: ["M21 12a9 9 0 1 1-2.64-6.36", "M21 4v5h-5"],
	shield: ["M12 3l8 3v6c0 5-4 8-8 9-4-1-8-4-8-9V6z"],
	user: ["M15.6 8a3.6 3.6 0 1 1-7.2 0 3.6 3.6 0 0 1 7.2 0", "M5 20c.8-3.5 3.6-5 7-5s6.2 1.5 7 5"],
	x: ["M6 6l12 12M18 6L6 18"],
} as const

export type IconName = keyof typeof iconPaths
