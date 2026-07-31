// Mirrors internal/records/domain/url.go's CanonicalOrigin.IsLoopback: the
// only hosts nothing outside this machine can make a browser tab's real
// origin report as. Used here purely to decide whether it's worth asking the
// content script for a project tag at all — the daemon independently
// re-derives loopback-ness itself and never trusts what the client sends.

function isLoopbackIPv4(host: string): boolean {
  const octets = host.split('.')
  if (octets.length !== 4 || octets[0] !== '127') return false
  return octets.every((o) => /^\d{1,3}$/.test(o) && Number(o) <= 255)
}

export function isLoopbackHost(hostname: string): boolean {
  const host = hostname.toLowerCase()
  return host === 'localhost' || host === '::1' || isLoopbackIPv4(host)
}
