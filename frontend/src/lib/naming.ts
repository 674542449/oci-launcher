// Console-style default names (instance-YYYYMMDD-HHMM, volume-…, bucket-…), stamped in the
// naming time zone so they match what the backend writes for VCNs.
export const NAME_TIMEZONE = 'Asia/Tokyo'

export const nameStamp = (d: Date = new Date()) => {
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: NAME_TIMEZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(d)
  const get = (type: string) => parts.find((p) => p.type === type)?.value || ''
  return `${get('year')}${get('month')}${get('day')}-${get('hour')}${get('minute')}`
}

export const defaultName = (prefix: string) => `${prefix}-${nameStamp()}`
