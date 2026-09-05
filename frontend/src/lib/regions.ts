// OCI commercial regions (docs.oracle.com/en-us/iaas/Content/General/Concepts/regions.htm, 2026-09).
// identifier -> { name: official region name, zh: Chinese label, country }
export interface RegionInfo {
  name: string
  zh: string
  country: string
}

export const REGIONS: Record<string, RegionInfo> = {
  'ap-sydney-1': { name: 'Australia East (Sydney)', zh: '澳大利亚东部 · 悉尼', country: '澳大利亚' },
  'ap-melbourne-1': { name: 'Australia Southeast (Melbourne)', zh: '澳大利亚东南部 · 墨尔本', country: '澳大利亚' },
  'sa-saopaulo-1': { name: 'Brazil East (Sao Paulo)', zh: '巴西东部 · 圣保罗', country: '巴西' },
  'sa-vinhedo-1': { name: 'Brazil Southeast (Vinhedo)', zh: '巴西东南部 · 维涅杜', country: '巴西' },
  'ca-montreal-1': { name: 'Canada Southeast (Montreal)', zh: '加拿大东南部 · 蒙特利尔', country: '加拿大' },
  'ca-toronto-1': { name: 'Canada Southeast (Toronto)', zh: '加拿大东南部 · 多伦多', country: '加拿大' },
  'sa-santiago-1': { name: 'Chile Central (Santiago)', zh: '智利中部 · 圣地亚哥', country: '智利' },
  'sa-valparaiso-1': { name: 'Chile West (Valparaiso)', zh: '智利西部 · 瓦尔帕莱索', country: '智利' },
  'sa-bogota-1': { name: 'Colombia Central (Bogota)', zh: '哥伦比亚中部 · 波哥大', country: '哥伦比亚' },
  'eu-paris-1': { name: 'France Central (Paris)', zh: '法国中部 · 巴黎', country: '法国' },
  'eu-marseille-1': { name: 'France South (Marseille)', zh: '法国南部 · 马赛', country: '法国' },
  'eu-frankfurt-1': { name: 'Germany Central (Frankfurt)', zh: '德国中部 · 法兰克福', country: '德国' },
  'ap-hyderabad-1': { name: 'India South (Hyderabad)', zh: '印度南部 · 海得拉巴', country: '印度' },
  'ap-mumbai-1': { name: 'India West (Mumbai)', zh: '印度西部 · 孟买', country: '印度' },
  'ap-batam-1': { name: 'Indonesia North (Batam)', zh: '印度尼西亚北部 · 巴淡', country: '印度尼西亚' },
  'il-jerusalem-1': { name: 'Israel Central (Jerusalem)', zh: '以色列中部 · 耶路撒冷', country: '以色列' },
  'eu-milan-1': { name: 'Italy Northwest (Milan)', zh: '意大利西北部 · 米兰', country: '意大利' },
  'eu-turin-1': { name: 'Italy North (Turin)', zh: '意大利北部 · 都灵', country: '意大利' },
  'ap-osaka-1': { name: 'Japan Central (Osaka)', zh: '日本中部 · 大阪', country: '日本' },
  'ap-tokyo-1': { name: 'Japan East (Tokyo)', zh: '日本东部 · 东京', country: '日本' },
  'ap-kulai-2': { name: 'Malaysia West 2 (Kulai)', zh: '马来西亚西部 2 · 古来', country: '马来西亚' },
  'mx-queretaro-1': { name: 'Mexico Central (Queretaro)', zh: '墨西哥中部 · 克雷塔罗', country: '墨西哥' },
  'mx-monterrey-1': { name: 'Mexico Northeast (Monterrey)', zh: '墨西哥东北部 · 蒙特雷', country: '墨西哥' },
  'af-casablanca-1': { name: 'Morocco West (Casablanca)', zh: '摩洛哥西部 · 卡萨布兰卡', country: '摩洛哥' },
  'eu-amsterdam-1': { name: 'Netherlands Northwest (Amsterdam)', zh: '荷兰西北部 · 阿姆斯特丹', country: '荷兰' },
  'me-riyadh-1': { name: 'Saudi Arabia Central (Riyadh)', zh: '沙特中部 · 利雅得', country: '沙特阿拉伯' },
  'me-jeddah-1': { name: 'Saudi Arabia West (Jeddah)', zh: '沙特西部 · 吉达', country: '沙特阿拉伯' },
  'ap-singapore-1': { name: 'Singapore (Singapore)', zh: '新加坡', country: '新加坡' },
  'ap-singapore-2': { name: 'Singapore West (Singapore)', zh: '新加坡西', country: '新加坡' },
  'af-johannesburg-1': { name: 'South Africa Central (Johannesburg)', zh: '南非中部 · 约翰内斯堡', country: '南非' },
  'ap-seoul-1': { name: 'South Korea Central (Seoul)', zh: '韩国中部 · 首尔', country: '韩国' },
  'ap-chuncheon-1': { name: 'South Korea North (Chuncheon)', zh: '韩国北部 · 春川', country: '韩国' },
  'eu-madrid-1': { name: 'Spain Central (Madrid)', zh: '西班牙中部 · 马德里', country: '西班牙' },
  'eu-madrid-3': { name: 'Spain Central (Madrid 3)', zh: '西班牙中部 · 马德里 3', country: '西班牙' },
  'eu-stockholm-1': { name: 'Sweden Central (Stockholm)', zh: '瑞典中部 · 斯德哥尔摩', country: '瑞典' },
  'eu-zurich-1': { name: 'Switzerland North (Zurich)', zh: '瑞士北部 · 苏黎世', country: '瑞士' },
  'me-abudhabi-1': { name: 'UAE Central (Abu Dhabi)', zh: '阿联酋中部 · 阿布扎比', country: '阿联酋' },
  'me-dubai-1': { name: 'UAE East (Dubai)', zh: '阿联酋东部 · 迪拜', country: '阿联酋' },
  'uk-london-1': { name: 'UK South (London)', zh: '英国南部 · 伦敦', country: '英国' },
  'uk-cardiff-1': { name: 'UK West (Newport)', zh: '英国西部 · 纽波特', country: '英国' },
  'us-ashburn-1': { name: 'US East (Ashburn)', zh: '美国东部 · 阿什本', country: '美国' },
  'us-chicago-1': { name: 'US Midwest (Chicago)', zh: '美国中西部 · 芝加哥', country: '美国' },
  'us-phoenix-1': { name: 'US West (Phoenix)', zh: '美国西部 · 凤凰城', country: '美国' },
  'us-sanjose-1': { name: 'US West (San Jose)', zh: '美国西部 · 圣何塞', country: '美国' },
}

/** Chinese label for a region identifier, falling back to the identifier itself. */
export const regionLabel = (id?: string): string => {
  if (!id) return ''
  return REGIONS[id.toLowerCase()]?.zh || id
}

/** Country of the region, empty when unknown. */
export const regionCountry = (id?: string): string => (id ? REGIONS[id.toLowerCase()]?.country || '' : '')

/** ISO country code (from the account's subscription) to a Chinese name. */
const COUNTRY_CODES: Record<string, string> = {
  CN: '中国', HK: '中国香港', TW: '中国台湾', MO: '中国澳门', SG: '新加坡', JP: '日本', KR: '韩国', US: '美国', CA: '加拿大',
  GB: '英国', DE: '德国', FR: '法国', NL: '荷兰', IT: '意大利', ES: '西班牙', SE: '瑞典', CH: '瑞士', AU: '澳大利亚',
  IN: '印度', ID: '印度尼西亚', MY: '马来西亚', TH: '泰国', VN: '越南', PH: '菲律宾', BR: '巴西', MX: '墨西哥', CL: '智利',
  CO: '哥伦比亚', AE: '阿联酋', SA: '沙特阿拉伯', IL: '以色列', ZA: '南非', MA: '摩洛哥', TR: '土耳其', RU: '俄罗斯',
}
export const countryName = (code?: string): string => {
  if (!code) return ''
  const c = code.toUpperCase()
  return COUNTRY_CODES[c] || c
}
