// eflow: 监控分组规则，与后端 GroupMonitoringSetting.ResolveMonitoringGroups 保持一致。
//   name       精确点名（即使系统里已没有该分组也保留）
//   prefix*    前缀匹配系统分组（auto 不参与通配）
//   !name      精确排除，优先级最高
//   !prefix*   前缀排除，只作用于通配进来的分组，不影响精确点名

function cleanRules(rules: readonly string[]): string[] {
  return rules.map((r) => r.trim()).filter(Boolean)
}

function matchRule(rule: string, group: string): boolean {
  return rule.endsWith('*')
    ? group.startsWith(rule.slice(0, -1))
    : rule === group
}

export function resolveMonitoringGroups(
  rules: readonly string[],
  knownGroups: readonly string[]
): string[] {
  const includes: string[] = []
  const excludes: string[] = []
  for (const rule of cleanRules(rules)) {
    if (rule.startsWith('!')) {
      const negated = rule.slice(1).trim()
      if (negated) excludes.push(negated)
    } else {
      includes.push(rule)
    }
  }

  const explicit = new Map<string, boolean>()
  for (const rule of includes) {
    if (!rule.endsWith('*')) {
      explicit.set(rule, true)
      continue
    }
    for (const group of knownGroups) {
      if (group !== 'auto' && matchRule(rule, group) && !explicit.get(group)) {
        explicit.set(group, false)
      }
    }
  }

  const result: string[] = []
  for (const [group, named] of explicit) {
    const excluded = excludes.some((rule) =>
      named ? rule === group : matchRule(rule, group)
    )
    if (!excluded) result.push(group)
  }
  // 与 Go 的 slices.Sort 一致：按码点排序，不用 localeCompare。
  return result.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
}

/** 规则里不匹配任何现有分组的通配项，用于在设置页提示"这条规则没生效"。 */
export function unmatchedPatternRules(
  rules: readonly string[],
  knownGroups: readonly string[]
): string[] {
  return cleanRules(rules).filter((rule) => {
    const pattern = rule.startsWith('!') ? rule.slice(1).trim() : rule
    return (
      pattern.endsWith('*') &&
      !knownGroups.some((g) => g !== 'auto' && matchRule(pattern, g))
    )
  })
}

/**
 * 把监控页抽屉里的勾选结果折回规则：尽量保留原有通配规则，只对和规则结果不一致的
 * 分组追加精确点名（name）或精确排除（!name），并顺手删掉失效的反向条目。
 */
export function applyMonitoringToggles(
  rules: readonly string[],
  knownGroups: readonly string[],
  enabled: ReadonlySet<string>
): string[] {
  let next = cleanRules(rules)
  const isOn = (group: string) =>
    resolveMonitoringGroups(next, knownGroups).includes(group)
  for (const group of knownGroups) {
    const want = enabled.has(group)
    if (want === isOn(group)) continue
    if (want) {
      next = next.filter((r) => r !== `!${group}`)
      if (!isOn(group)) next.push(group)
    } else {
      next = next.filter((r) => r !== group)
      if (isOn(group)) next.push(`!${group}`)
    }
  }
  return next
}
