// Station keys are dynamic, but the static build needs one entry per stage 1 station shell.
export const prerender = true

export function entries(): { key: string }[] {
	return ["triage", "time", "narrative", "hardware", "synthesis"].map(key => ({ key }))
}
