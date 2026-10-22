export function createTerminalOutputDecoder() {
  const textDecoder = new TextDecoder()
  let escapeState = 'text'

  return {
    push(base64) {
      let bytes
      try {
        bytes = Uint8Array.from(atob(String(base64 || '')), char => char.charCodeAt(0))
      } catch {
        return ''
      }

      const text = textDecoder.decode(bytes, { stream: true })
      let output = ''
      for (const char of text) {
        const code = char.codePointAt(0)
        if (escapeState === 'text') {
          if (code === 0x1b) escapeState = 'escape'
          else output += char
        } else if (escapeState === 'escape') {
          if (char === ']') escapeState = 'osc'
          else if (char === '[') escapeState = 'csi'
          else escapeState = 'text'
        } else if (escapeState === 'csi') {
          if (code >= 0x40 && code <= 0x7e) escapeState = 'text'
        } else if (escapeState === 'osc') {
          if (code === 0x07) escapeState = 'text'
          else if (code === 0x1b) escapeState = 'osc-escape'
        } else if (escapeState === 'osc-escape') {
          if (char === '\\') escapeState = 'text'
          else if (code !== 0x1b) escapeState = 'osc'
        }
      }
      return output
    },
  }
}
