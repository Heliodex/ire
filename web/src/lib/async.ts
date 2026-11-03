// ignore detaches a background call whose failure the caller does not act on.
export function ignore(promise: Promise<unknown>): void {
	// oxlint-disable-next-line promise/prefer-await-to-then -- This helper exists to contain the detached call.
	void promise.catch(() => {})
}

// sleep waits between a visible confirmation and the next screen.
export function sleep(ms: number): Promise<void> {
	// oxlint-disable-next-line promise/avoid-new -- The platform only offers a callback timer.
	return new Promise((resolve): void => {
		setTimeout(resolve, ms)
	})
}
