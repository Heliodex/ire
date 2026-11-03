import type { Me } from "./api"

// session is the shared sign-in state the layout and pages read. It starts empty on every load; onMount refreshes it from the API.
let me = $state<Me | null>(null)

export const session = {
	get me(): Me | null {
		return me
	},
	set me(value: Me | null) {
		me = value
	},
}
