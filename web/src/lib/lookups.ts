import { api, unwrap, type Currency } from './api/client'
import { useAsync } from './useAsync'

/** useCurrencies loads currencies once per mount and maps code → minor units. */
export function useCurrencies() {
  const s = useAsync(() => unwrap(api.GET('/currencies')).then((r) => r.items), [])
  const minor = (code: string) => s.data?.find((c: Currency) => c.code === code)?.minorUnits ?? 2
  return { ...s, currencies: s.data ?? [], minor }
}
