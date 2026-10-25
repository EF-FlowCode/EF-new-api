import {
  MATCH_EQ,
  MATCH_GT,
  MATCH_GTE,
  MATCH_LT,
  MATCH_LTE,
  MATCH_RANGE,
  hasNonCanonicalTimeRange,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type RequestRuleGroup,
  type TimeCondition,
} from './billing-expr'

export type TimePricingPeriod = {
  timezone: string
  startHour: number
  endHour: number
  multiplier: number
}

export type TimePricingSummary = {
  currentMultiplier: number
  periods: TimePricingPeriod[]
  timezones: string[]
  ruleCount: number
}

type ZonedTimeParts = {
  hour: number
  minute: number
  weekday: number
  month: number
  day: number
}

const WEEKDAYS: Record<string, number> = {
  Sun: 0,
  Mon: 1,
  Tue: 2,
  Wed: 3,
  Thu: 4,
  Fri: 5,
  Sat: 6,
}

function getZonedTimeParts(now: Date, timezone: string): ZonedTimeParts | null {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      hour: 'numeric',
      minute: 'numeric',
      weekday: 'short',
      month: 'numeric',
      day: 'numeric',
      hourCycle: 'h23',
    }).formatToParts(now)
    const values = Object.fromEntries(
      parts.map((part) => [part.type, part.value])
    )
    return {
      hour: Number(values.hour),
      minute: Number(values.minute),
      weekday: WEEKDAYS[values.weekday] ?? -1,
      month: Number(values.month),
      day: Number(values.day),
    }
  } catch {
    return null
  }
}

function getTimeValue(parts: ZonedTimeParts, condition: TimeCondition): number {
  return parts[condition.timeFunc]
}

function matchesTimeCondition(
  value: number,
  condition: TimeCondition
): boolean {
  if (!Number.isFinite(value)) return false
  const expected = Number(condition.value)

  switch (condition.mode) {
    case MATCH_EQ:
      return value === expected
    case MATCH_GT:
      return value > expected
    case MATCH_GTE:
      return value >= expected
    case MATCH_LT:
      return value < expected
    case MATCH_LTE:
      return value <= expected
    case MATCH_RANGE: {
      const start = Number(condition.rangeStart)
      const end = Number(condition.rangeEnd)
      const operator = condition.rangeOperator || (start <= end ? '&&' : '||')
      return operator === '&&'
        ? value >= start && value < end
        : value >= start || value < end
    }
    default:
      return false
  }
}

function isTimeRuleGroup(group: RequestRuleGroup): boolean {
  return (
    group.conditions.length > 0 &&
    group.conditions.every((condition) => condition.source === 'time')
  )
}

function matchesTimeRuleGroup(
  group: RequestRuleGroup,
  now: Date,
  partsCache: Map<string, ZonedTimeParts | null>
): boolean {
  return group.conditions.every((condition) => {
    if (condition.source !== 'time') return false
    if (!partsCache.has(condition.timezone)) {
      partsCache.set(
        condition.timezone,
        getZonedTimeParts(now, condition.timezone)
      )
    }
    const parts = partsCache.get(condition.timezone)
    return Boolean(
      parts && matchesTimeCondition(getTimeValue(parts, condition), condition)
    )
  })
}

function toHourPeriod(group: RequestRuleGroup): TimePricingPeriod | null {
  const multiplier = Number(group.multiplier)
  if (!Number.isFinite(multiplier) || multiplier <= 0) return null

  const conditions = group.conditions.filter(
    (condition): condition is TimeCondition =>
      condition.source === 'time' && condition.timeFunc === 'hour'
  )
  if (conditions.length !== group.conditions.length) return null

  if (conditions.length === 1 && conditions[0].mode === MATCH_RANGE) {
    const startHour = Number(conditions[0].rangeStart)
    const endHour = Number(conditions[0].rangeEnd)
    const canonicalOperator = startHour <= endHour ? '&&' : '||'
    if (
      !Number.isFinite(startHour) ||
      !Number.isFinite(endHour) ||
      (conditions[0].rangeOperator &&
        conditions[0].rangeOperator !== canonicalOperator)
    ) {
      return null
    }
    return {
      timezone: conditions[0].timezone,
      startHour,
      endHour,
      multiplier,
    }
  }

  if (conditions.length !== 2) return null
  const start = conditions.find((condition) => condition.mode === MATCH_GTE)
  const end = conditions.find((condition) => condition.mode === MATCH_LT)
  if (!start || !end || start.timezone !== end.timezone) return null

  const startHour = Number(start.value)
  const endHour = Number(end.value)
  if (
    !Number.isFinite(startHour) ||
    !Number.isFinite(endHour) ||
    startHour >= endHour
  ) {
    return null
  }
  return {
    timezone: start.timezone,
    startHour,
    endHour,
    multiplier,
  }
}

export function getTimePricingSummary(
  billingExpr: string,
  now: Date = new Date()
): TimePricingSummary | null {
  const split = splitBillingExprAndRequestRules(billingExpr)
  if (hasNonCanonicalTimeRange(split.requestRuleExpr)) return null
  const parsed = tryParseRequestRuleExpr(split.requestRuleExpr || '')
  const timeRuleGroups = (parsed || []).filter(isTimeRuleGroup)
  if (timeRuleGroups.length === 0) return null

  const partsCache = new Map<string, ZonedTimeParts | null>()
  const currentMultiplier = timeRuleGroups.reduce((multiplier, group) => {
    if (!matchesTimeRuleGroup(group, now, partsCache)) return multiplier
    const groupMultiplier = Number(group.multiplier)
    return Number.isFinite(groupMultiplier)
      ? multiplier * groupMultiplier
      : multiplier
  }, 1)
  const timezones = Array.from(
    new Set(
      timeRuleGroups.flatMap((group) =>
        group.conditions.flatMap((condition) =>
          condition.source === 'time' ? [condition.timezone] : []
        )
      )
    )
  )

  return {
    currentMultiplier,
    periods: timeRuleGroups.flatMap((group) => {
      const period = toHourPeriod(group)
      return period ? [period] : []
    }),
    timezones,
    ruleCount: timeRuleGroups.length,
  }
}
