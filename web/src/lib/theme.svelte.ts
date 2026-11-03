type Theme = "light" | "dark"

const storageKey = "review-theme"

let current = $state<Theme>("light")

// applyTheme sets the class and stores the choice. The inline script in app.html already applied it before paint; this keeps the toggle and the module in step.
function applyTheme(next: Theme): void {
	current = next

	if (typeof document === "undefined") return

	document.documentElement.classList.toggle("dark", next === "dark")

	try {
		localStorage.setItem(storageKey, next)
	} catch {
		// Storage can be blocked; the theme still applies for this page.
	}
}

// theme is the shared light/dark state. It follows the system preference until the reader picks one.
export const theme = {
	init(): void {
		if (typeof document === "undefined") return

		current = document.documentElement.classList.contains("dark") ? "dark" : "light"
	},
	toggle(): void {
		applyTheme(current === "dark" ? "light" : "dark")
	},
	get value(): Theme {
		return current
	},
}
