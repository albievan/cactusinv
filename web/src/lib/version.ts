import { api, unwrap } from './api/client'
import { useAsync } from './useAsync'

/** useVersion loads the public version/build (shown on the login screen and in the nav). */
export function useVersion() {
  const { data } = useAsync(() => unwrap(api.GET('/version')).catch(() => ({ version: '', build: '' })), [])
  return data && data.version ? `v${data.version} · build ${data.build}` : ''
}
