type Listener = (isDown: boolean) => void

let listener: Listener | null = null

export function reportApiDown() {
  listener?.(true)
}

export function reportApiUp() {
  listener?.(false)
}

export function onApiStatusChange(fn: Listener): () => void {
  listener = fn
  return () => {
    if (listener === fn) listener = null
  }
}
