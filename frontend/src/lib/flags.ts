// Self-hosted SVG flags (country-flag-icons, 3:2). Windows browsers render flag emoji as two
// plain letters, so flags are shown as images everywhere; the emoji stays as a fallback only.
import { regionCountryCode, flagEmoji } from './regions'

const files = import.meta.glob('/node_modules/country-flag-icons/3x2/{AE,AU,BR,CA,CH,CL,CN,CO,DE,ES,FR,GB,HK,ID,IL,IN,IT,JP,KR,MA,MO,MX,MY,NL,PH,RU,SA,SE,SG,TH,TR,TW,US,VN,ZA}.svg', { eager: true, query: '?url', import: 'default' }) as Record<string, string>

/** URL of the SVG flag for an ISO 3166-1 alpha-2 code, or '' when unknown. */
export const flagUrl = (cc?: string): string => {
  const up = (cc || '').toUpperCase()
  if (up.length !== 2) return ''
  return files[`/node_modules/country-flag-icons/3x2/${up}.svg`] || ''
}

/** Flag URL for an OCI region id (ap-tokyo-1 -> JP). */
export const regionFlagUrl = (region?: string): string => flagUrl(regionCountryCode(region))

export { flagEmoji }
