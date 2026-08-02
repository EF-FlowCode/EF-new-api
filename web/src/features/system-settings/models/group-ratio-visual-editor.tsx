import { useEffect, useState, useMemo, memo } from 'react'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  GripVertical,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { safeJsonParse } from '../utils/json-parser'
import { GroupSpecialUsableRulesEditor } from './group-special-usable-editor'

type GroupRatioVisualEditorProps = {
  view: 'customer' | 'token'
  groupRatio: string
  topupGroupRatio: string
  userUsableGroups: string
  groupGroupRatio: string
  groupSpecialUsableGroup: string
  autoGroups: string
  onChange: (field: string, value: string) => void
}

type SimpleGroup = {
  name: string
  value: string
}

type UsableGroup = {
  name: string
  description: string
}

type GroupOverride = {
  targetGroup: string
  ratio: number
}

// eflow: 三个 flat 表（分组倍率 / 充值分组倍率 / 用户可选分组）的排序状态
// 跟模型倍率那边走 react-table 不一样——这边表格行数少（一般 < 20），不需要分页 /
// 过滤 / 列可见性，因此走轻量路线：每个表自己一个 useState，点表头切 asc/desc。
type SortDir = 'asc' | 'desc'
type SortConfig<K extends string> = { column: K; dir: SortDir }

type SortHeaderProps<K extends string> = {
  column: K
  label: string
  config: SortConfig<K>
  onChange: (config: SortConfig<K>) => void
  className?: string
}

function SortHeader<K extends string>({
  column,
  label,
  config,
  onChange,
  className,
}: SortHeaderProps<K>) {
  const active = config.column === column
  const Icon = !active
    ? ArrowUpDown
    : config.dir === 'asc'
      ? ArrowUp
      : ArrowDown
  return (
    <button
      type='button'
      className={
        'hover:text-foreground flex items-center gap-1.5 text-left font-medium ' +
        (active ? 'text-foreground' : 'text-muted-foreground') +
        (className ? ' ' + className : '')
      }
      onClick={() => {
        if (!active) {
          onChange({ column, dir: 'asc' })
        } else {
          onChange({ column, dir: config.dir === 'asc' ? 'desc' : 'asc' })
        }
      }}
    >
      <span>{label}</span>
      <Icon className='h-3.5 w-3.5' />
    </button>
  )
}

function compareValues(
  a: string | number,
  b: string | number,
  dir: SortDir
): number {
  // 数值优先（"1.5" 比 "10" 该排 1.5 在前），失败回退到 localeCompare 中文友好。
  const an = typeof a === 'number' ? a : parseFloat(a)
  const bn = typeof b === 'number' ? b : parseFloat(b)
  let cmp: number
  if (Number.isFinite(an) && Number.isFinite(bn)) {
    cmp = an - bn
  } else {
    cmp = String(a).localeCompare(String(b), 'zh-Hans-CN')
  }
  return dir === 'asc' ? cmp : -cmp
}

export const GroupRatioVisualEditor = memo(function GroupRatioVisualEditor({
  view,
  groupRatio,
  topupGroupRatio,
  userUsableGroups,
  groupGroupRatio,
  groupSpecialUsableGroup,
  autoGroups,
  onChange,
}: GroupRatioVisualEditorProps) {
  const { t } = useTranslation()
  const [simpleDialogOpen, setSimpleDialogOpen] = useState(false)
  const [simpleDialogType, setSimpleDialogType] = useState<
    'groupRatio' | 'topupGroupRatio' | null
  >(null)
  const [simpleEditData, setSimpleEditData] = useState<SimpleGroup | null>(null)

  const [usableDialogOpen, setUsableDialogOpen] = useState(false)
  const [usableEditData, setUsableEditData] = useState<UsableGroup | null>(null)

  const [autoGroupDialogOpen, setAutoGroupDialogOpen] = useState(false)
  const [autoGroupInput, setAutoGroupInput] = useState('')

  const [groupOverrideDialogOpen, setGroupOverrideDialogOpen] = useState(false)
  const [groupOverrideUserGroup, setGroupOverrideUserGroup] = useState<
    string | null
  >(null)
  const [groupOverrideEditData, setGroupOverrideEditData] =
    useState<GroupOverride | null>(null)
  const [selectedCustomerTier, setSelectedCustomerTier] = useState<
    string | null
  >(null)

  // 三个 flat 表格的排序状态（默认按名字升序，跟显示语义一致）
  const [groupSort, setGroupSort] = useState<SortConfig<'name' | 'value'>>({
    column: 'name',
    dir: 'asc',
  })
  const [topupSort, setTopupSort] = useState<SortConfig<'name' | 'value'>>({
    column: 'name',
    dir: 'asc',
  })
  const [usableSort, setUsableSort] = useState<
    SortConfig<'name' | 'description'>
  >({ column: 'name', dir: 'asc' })

  // Parse group ratios
  const groupRatioList = useMemo(() => {
    const map = safeJsonParse<Record<string, number>>(groupRatio, {
      fallback: {},
      context: 'group ratios',
    })
    const list = Object.entries(map).map(([name, value]) => ({
      name,
      value: String(value),
    }))
    return [...list].sort((a, b) =>
      compareValues(a[groupSort.column], b[groupSort.column], groupSort.dir)
    )
  }, [groupRatio, groupSort])

  // Parse topup group ratios
  const topupRatioList = useMemo(() => {
    const map = safeJsonParse<Record<string, number>>(topupGroupRatio, {
      fallback: {},
      context: 'topup group ratios',
    })
    const list = Object.entries(map).map(([name, value]) => ({
      name,
      value: String(value),
    }))
    return [...list].sort((a, b) =>
      compareValues(a[topupSort.column], b[topupSort.column], topupSort.dir)
    )
  }, [topupGroupRatio, topupSort])

  // Parse usable groups
  const usableGroupsList = useMemo(() => {
    const map = safeJsonParse<Record<string, string>>(userUsableGroups, {
      fallback: {},
      context: 'user usable groups',
    })
    const list = Object.entries(map).map(([name, description]) => ({
      name,
      description: String(description),
    }))
    return [...list].sort((a, b) =>
      compareValues(a[usableSort.column], b[usableSort.column], usableSort.dir)
    )
  }, [userUsableGroups, usableSort])

  const tokenGroupOptions = useMemo(
    () =>
      groupRatioList.map((group) => ({
        value: group.name,
        label: group.name,
      })),
    [groupRatioList]
  )

  // Parse auto groups
  const autoGroupsList = useMemo(() => {
    return safeJsonParse<string[]>(autoGroups, {
      fallback: [],
      context: 'auto groups',
    })
  }, [autoGroups])

  // Parse group-group ratios
  const groupGroupRatioList = useMemo(() => {
    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        context: 'group-group ratios',
      }
    )
    return Object.entries(map).map(([userGroup, overrides]) => ({
      userGroup,
      overrides: Object.entries(overrides).map(([targetGroup, ratio]) => ({
        targetGroup,
        ratio,
      })),
    }))
  }, [groupGroupRatio])

  const specialUsableGroupMap = useMemo(
    () =>
      safeJsonParse<Record<string, Record<string, string>>>(
        groupSpecialUsableGroup,
        { fallback: {}, silent: true }
      ),
    [groupSpecialUsableGroup]
  )

  const activeCustomerTier =
    selectedCustomerTier &&
    topupRatioList.some((tier) => tier.name === selectedCustomerTier)
      ? selectedCustomerTier
      : (topupRatioList[0]?.name ?? null)

  const selectedOverrides =
    groupGroupRatioList.find((item) => item.userGroup === activeCustomerTier)
      ?.overrides ?? []

  const unregisteredCustomerTiers = useMemo(() => {
    const registered = new Set(topupRatioList.map((tier) => tier.name))
    return Array.from(
      new Set([
        ...groupGroupRatioList.map((item) => item.userGroup),
        ...Object.keys(specialUsableGroupMap),
      ])
    )
      .filter((name) => !registered.has(name))
      .sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  }, [groupGroupRatioList, specialUsableGroupMap, topupRatioList])

  // Simple group handlers (for groupRatio and topupGroupRatio)
  const handleSimpleAdd = (type: 'groupRatio' | 'topupGroupRatio') => {
    setSimpleDialogType(type)
    setSimpleEditData(null)
    setSimpleDialogOpen(true)
  }

  const handleSimpleEdit = (
    type: 'groupRatio' | 'topupGroupRatio',
    group: SimpleGroup
  ) => {
    setSimpleDialogType(type)
    setSimpleEditData(group)
    setSimpleDialogOpen(true)
  }

  const handleSimpleSave = (name: string, value: string) => {
    if (!simpleDialogType) return

    const fieldName =
      simpleDialogType === 'groupRatio' ? groupRatio : topupGroupRatio
    const map = safeJsonParse<Record<string, number>>(fieldName, {
      fallback: {},
      silent: true,
    })

    if (simpleEditData && simpleEditData.name !== name) {
      delete map[simpleEditData.name]
    }

    map[name] = parseFloat(value)

    const field =
      simpleDialogType === 'groupRatio' ? 'GroupRatio' : 'TopupGroupRatio'
    onChange(field, JSON.stringify(map, null, 2))
    if (simpleDialogType === 'topupGroupRatio') {
      setSelectedCustomerTier(name)
    }
    setSimpleDialogOpen(false)
  }

  const handleSimpleDelete = (
    type: 'groupRatio' | 'topupGroupRatio',
    name: string
  ) => {
    const fieldName = type === 'groupRatio' ? groupRatio : topupGroupRatio
    const map = safeJsonParse<Record<string, number>>(fieldName, {
      fallback: {},
      silent: true,
    })
    delete map[name]

    const field = type === 'groupRatio' ? 'GroupRatio' : 'TopupGroupRatio'
    onChange(field, JSON.stringify(map, null, 2))

    if (type === 'topupGroupRatio') {
      const customerOverrides = safeJsonParse<
        Record<string, Record<string, number>>
      >(groupGroupRatio, { fallback: {}, silent: true })
      if (name in customerOverrides) {
        delete customerOverrides[name]
        onChange('GroupGroupRatio', JSON.stringify(customerOverrides, null, 2))
      }
      if (name in specialUsableGroupMap) {
        const nextSpecialRules = { ...specialUsableGroupMap }
        delete nextSpecialRules[name]
        onChange(
          'GroupSpecialUsableGroup',
          JSON.stringify(nextSpecialRules, null, 2)
        )
      }
      if (selectedCustomerTier === name) setSelectedCustomerTier(null)
      return
    }

    const usableMap = safeJsonParse<Record<string, string>>(userUsableGroups, {
      fallback: {},
      silent: true,
    })
    if (name in usableMap) {
      delete usableMap[name]
      onChange('UserUsableGroups', JSON.stringify(usableMap, null, 2))
    }

    const nextAutoGroups = autoGroupsList.filter((group) => group !== name)
    if (nextAutoGroups.length !== autoGroupsList.length) {
      onChange('AutoGroups', JSON.stringify(nextAutoGroups, null, 2))
    }

    const overrides = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      { fallback: {}, silent: true }
    )
    let overridesChanged = false
    for (const userGroup of Object.keys(overrides)) {
      if (name in overrides[userGroup]) {
        delete overrides[userGroup][name]
        overridesChanged = true
      }
      if (Object.keys(overrides[userGroup]).length === 0) {
        delete overrides[userGroup]
      }
    }
    if (overridesChanged) {
      onChange('GroupGroupRatio', JSON.stringify(overrides, null, 2))
    }
  }

  const handleRegisterCustomerTier = (name: string) => {
    const map = safeJsonParse<Record<string, number>>(topupGroupRatio, {
      fallback: {},
      silent: true,
    })
    map[name] = 1
    onChange('TopupGroupRatio', JSON.stringify(map, null, 2))
    setSelectedCustomerTier(name)
  }

  // Usable groups handlers
  const handleUsableAdd = () => {
    setUsableEditData(null)
    setUsableDialogOpen(true)
  }

  const handleUsableEdit = (group: UsableGroup) => {
    setUsableEditData(group)
    setUsableDialogOpen(true)
  }

  const handleUsableSave = (name: string, description: string) => {
    if (!groupRatioList.some((group) => group.name === name)) return

    const map = safeJsonParse<Record<string, string>>(userUsableGroups, {
      fallback: {},
      silent: true,
    })

    if (usableEditData && usableEditData.name !== name) {
      delete map[usableEditData.name]
    }

    map[name] = description

    onChange('UserUsableGroups', JSON.stringify(map, null, 2))
    setUsableDialogOpen(false)
  }

  const handleUsableDelete = (name: string) => {
    const map = safeJsonParse<Record<string, string>>(userUsableGroups, {
      fallback: {},
      silent: true,
    })
    delete map[name]
    onChange('UserUsableGroups', JSON.stringify(map, null, 2))
  }

  // Auto groups handlers
  const handleAutoGroupAdd = () => {
    setAutoGroupInput('')
    setAutoGroupDialogOpen(true)
  }

  const handleAutoGroupSave = () => {
    if (
      !autoGroupInput.trim() ||
      !groupRatioList.some((group) => group.name === autoGroupInput.trim()) ||
      autoGroupsList.includes(autoGroupInput.trim())
    )
      return

    const list = [...autoGroupsList, autoGroupInput.trim()]
    onChange('AutoGroups', JSON.stringify(list, null, 2))
    setAutoGroupDialogOpen(false)
  }

  const handleAutoGroupDelete = (index: number) => {
    const list = autoGroupsList.filter((_, i) => i !== index)
    onChange('AutoGroups', JSON.stringify(list, null, 2))
  }

  const handleAutoGroupMove = (index: number, direction: 'up' | 'down') => {
    const list = [...autoGroupsList]
    const newIndex = direction === 'up' ? index - 1 : index + 1

    if (newIndex < 0 || newIndex >= list.length) return
    ;[list[index], list[newIndex]] = [list[newIndex], list[index]]
    onChange('AutoGroups', JSON.stringify(list, null, 2))
  }

  // Group-group ratio handlers
  const handleOverrideAdd = (userGroup: string) => {
    setGroupOverrideUserGroup(userGroup)
    setGroupOverrideEditData(null)
    setGroupOverrideDialogOpen(true)
  }

  const handleOverrideEdit = (userGroup: string, override: GroupOverride) => {
    setGroupOverrideUserGroup(userGroup)
    setGroupOverrideEditData(override)
    setGroupOverrideDialogOpen(true)
  }

  const handleOverrideSave = (
    targetGroup: string,
    ratio: number,
    oldTargetGroup?: string
  ) => {
    if (
      !groupOverrideUserGroup ||
      !groupRatioList.some((group) => group.name === targetGroup)
    )
      return

    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )

    if (!map[groupOverrideUserGroup]) {
      map[groupOverrideUserGroup] = {}
    }

    if (oldTargetGroup && oldTargetGroup !== targetGroup) {
      delete map[groupOverrideUserGroup][oldTargetGroup]
    }

    map[groupOverrideUserGroup][targetGroup] = ratio

    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
    setGroupOverrideDialogOpen(false)
  }

  const handleOverrideDelete = (userGroup: string, targetGroup: string) => {
    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )

    if (map[userGroup]) {
      delete map[userGroup][targetGroup]
      if (Object.keys(map[userGroup]).length === 0) {
        delete map[userGroup]
      }
    }

    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
  }

  return (
    <div
      className={
        view === 'token'
          ? 'grid gap-6 lg:grid-cols-2'
          : 'grid items-start gap-6 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]'
      }
    >
      {/* Group Ratios */}
      {view === 'token' && (
        <Card className='min-w-0'>
          <CardHeader>
            <CardTitle>{t('Token group base ratios')}</CardTitle>
            <CardDescription>
              {t(
                'All token groups are defined here. Other settings can only reference these names.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className='space-y-4'>
              <Button onClick={() => handleSimpleAdd('groupRatio')} size='sm'>
                <Plus className='mr-2 h-4 w-4' />
                {t('Add token group')}
              </Button>
              {groupRatioList.length > 0 && (
                <div className='max-h-[32rem] overflow-auto rounded-md border'>
                  <Table className='table-fixed'>
                    <TableHeader className='bg-background sticky top-0 z-10'>
                      <TableRow>
                        <TableHead className='w-[55%]'>
                          <SortHeader
                            column='name'
                            label={t('Token group')}
                            config={groupSort}
                            onChange={setGroupSort}
                          />
                        </TableHead>
                        <TableHead className='w-[20%]'>
                          <SortHeader
                            column='value'
                            label={t('Ratio')}
                            config={groupSort}
                            onChange={setGroupSort}
                          />
                        </TableHead>
                        <TableHead className='w-[25%] text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {groupRatioList.map((group) => (
                        <TableRow key={group.name}>
                          <TableCell className='truncate font-medium'>
                            {group.name}
                          </TableCell>
                          <TableCell>{group.value}</TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-2'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() =>
                                  handleSimpleEdit('groupRatio', group)
                                }
                              >
                                <Pencil className='h-4 w-4' />
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() =>
                                  handleSimpleDelete('groupRatio', group.name)
                                }
                              >
                                <Trash2 className='h-4 w-4' />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Topup Group Ratios */}
      {view === 'customer' && (
        <Card className='min-w-0'>
          <CardHeader>
            <CardTitle>{t('Customer tier recharge discounts')}</CardTitle>
            <CardDescription>
              {t(
                'All customer tiers are defined here. A multiplier of 1 means no recharge discount.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className='space-y-4'>
              <Button
                onClick={() => handleSimpleAdd('topupGroupRatio')}
                size='sm'
              >
                <Plus className='mr-2 h-4 w-4' />
                {t('Add customer tier')}
              </Button>
              {unregisteredCustomerTiers.length > 0 && (
                <div className='space-y-2 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-950/30'>
                  <p className='text-sm font-medium'>
                    {t('Unregistered customer tiers found')}
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'These tiers are referenced by existing rules but are not registered in the customer tier list.'
                    )}
                  </p>
                  <div className='flex flex-wrap gap-2'>
                    {unregisteredCustomerTiers.map((name) => (
                      <Button
                        key={name}
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => handleRegisterCustomerTier(name)}
                      >
                        {t('Register {{name}}', { name })}
                      </Button>
                    ))}
                  </div>
                </div>
              )}
              {topupRatioList.length > 0 && (
                <div className='rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>
                          <SortHeader
                            column='name'
                            label={t('Customer tier')}
                            config={topupSort}
                            onChange={setTopupSort}
                          />
                        </TableHead>
                        <TableHead>
                          <SortHeader
                            column='value'
                            label={t('Recharge multiplier')}
                            config={topupSort}
                            onChange={setTopupSort}
                          />
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {topupRatioList.map((group) => (
                        <TableRow
                          key={group.name}
                          data-state={
                            activeCustomerTier === group.name
                              ? 'selected'
                              : undefined
                          }
                        >
                          <TableCell className='font-medium'>
                            <button
                              type='button'
                              className='hover:underline'
                              onClick={() =>
                                setSelectedCustomerTier(group.name)
                              }
                            >
                              {group.name}
                            </button>
                          </TableCell>
                          <TableCell>{group.value}</TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-2'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() =>
                                  handleSimpleEdit('topupGroupRatio', group)
                                }
                              >
                                <Pencil className='h-4 w-4' />
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() =>
                                  handleSimpleDelete(
                                    'topupGroupRatio',
                                    group.name
                                  )
                                }
                              >
                                <Trash2 className='h-4 w-4' />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Inter-group ratio overrides */}
      {view === 'customer' && (
        <div className='min-w-0 space-y-6'>
          <Card>
            <CardHeader>
              <CardTitle>
                {activeCustomerTier
                  ? t('{{tier}} exclusive pricing', {
                      tier: activeCustomerTier,
                    })
                  : t('Customer tier exclusive pricing')}
              </CardTitle>
              <CardDescription>
                {t(
                  'Override token group billing multipliers for the selected customer tier.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {!activeCustomerTier ? (
                <p className='text-muted-foreground py-6 text-center text-sm'>
                  {t('Select a customer tier to manage its benefits.')}
                </p>
              ) : (
                <div className='space-y-4'>
                  <Button
                    onClick={() => handleOverrideAdd(activeCustomerTier)}
                    size='sm'
                  >
                    <Plus className='mr-2 h-4 w-4' />
                    {t('Add exclusive price')}
                  </Button>
                  {selectedOverrides.length === 0 ? (
                    <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-sm'>
                      {t('No exclusive pricing for this customer tier.')}
                    </p>
                  ) : (
                    <div className='rounded-md border'>
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>{t('Target group')}</TableHead>
                            <TableHead>{t('Ratio')}</TableHead>
                            <TableHead className='text-right'>
                              {t('Actions')}
                            </TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {selectedOverrides.map((override) => (
                            <TableRow key={override.targetGroup}>
                              <TableCell className='font-medium'>
                                {override.targetGroup}
                              </TableCell>
                              <TableCell>{override.ratio}</TableCell>
                              <TableCell className='text-right'>
                                <div className='flex justify-end gap-2'>
                                  <Button
                                    variant='ghost'
                                    size='sm'
                                    onClick={() =>
                                      handleOverrideEdit(
                                        activeCustomerTier,
                                        override
                                      )
                                    }
                                  >
                                    <Pencil className='h-4 w-4' />
                                  </Button>
                                  <Button
                                    variant='ghost'
                                    size='sm'
                                    onClick={() =>
                                      handleOverrideDelete(
                                        activeCustomerTier,
                                        override.targetGroup
                                      )
                                    }
                                  >
                                    <Trash2 className='h-4 w-4' />
                                  </Button>
                                </div>
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          <GroupSpecialUsableRulesEditor
            key={activeCustomerTier ?? 'no-customer-tier'}
            value={groupSpecialUsableGroup}
            tokenGroups={groupRatioList.map((group) => group.name)}
            customerTier={activeCustomerTier}
            onChange={(value) => onChange('GroupSpecialUsableGroup', value)}
          />
        </div>
      )}

      {/* Usable Groups */}
      {view === 'token' && (
        <Card className='min-w-0'>
          <CardHeader>
            <CardTitle>{t('Token groups available to users')}</CardTitle>
            <CardDescription>
              {t(
                'Choose from token groups defined in base ratios and add a user-facing description.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className='space-y-4'>
              <Button onClick={handleUsableAdd} size='sm'>
                <Plus className='mr-2 h-4 w-4' />
                {t('Set as user-selectable')}
              </Button>
              {usableGroupsList.length > 0 && (
                <div className='max-h-[32rem] overflow-auto rounded-md border'>
                  <Table className='table-fixed'>
                    <TableHeader className='bg-background sticky top-0 z-10'>
                      <TableRow>
                        <TableHead className='w-[35%]'>
                          <SortHeader
                            column='name'
                            label={t('Token group')}
                            config={usableSort}
                            onChange={setUsableSort}
                          />
                        </TableHead>
                        <TableHead className='w-[45%]'>
                          <SortHeader
                            column='description'
                            label={t('Description')}
                            config={usableSort}
                            onChange={setUsableSort}
                          />
                        </TableHead>
                        <TableHead className='w-[20%] text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {usableGroupsList.map((group) => (
                        <TableRow key={group.name}>
                          <TableCell className='truncate font-medium'>
                            {group.name}
                          </TableCell>
                          <TableCell
                            className='truncate'
                            title={group.description}
                          >
                            {group.description}
                          </TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-2'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() => handleUsableEdit(group)}
                              >
                                <Pencil className='h-4 w-4' />
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() => handleUsableDelete(group.name)}
                              >
                                <Trash2 className='h-4 w-4' />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Auto Groups */}
      {view === 'token' && (
        <Card className='min-w-0 lg:col-span-2'>
          <CardHeader>
            <CardTitle>{t('Token group auto assignment')}</CardTitle>
            <CardDescription>
              {t(
                'Priority order for automatic group assignment. New tokens rotate through this list.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className='space-y-4'>
              <Button onClick={handleAutoGroupAdd} size='sm'>
                <Plus className='mr-2 h-4 w-4' />
                {t('Add token group')}
              </Button>
              {autoGroupsList.length > 0 && (
                <div className='space-y-2'>
                  {autoGroupsList.map((group, index) => (
                    <div
                      key={index}
                      className='flex items-center gap-2 rounded-md border p-3'
                    >
                      <GripVertical className='text-muted-foreground h-4 w-4' />
                      <span className='flex-1 font-medium'>{group}</span>
                      <div className='flex gap-1'>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={index === 0}
                          onClick={() => handleAutoGroupMove(index, 'up')}
                        >
                          ↑
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={index === autoGroupsList.length - 1}
                          onClick={() => handleAutoGroupMove(index, 'down')}
                        >
                          ↓
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => handleAutoGroupDelete(index)}
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Simple Group Dialog */}
      <SimpleGroupDialog
        open={simpleDialogOpen}
        onOpenChange={setSimpleDialogOpen}
        onSave={handleSimpleSave}
        editData={simpleEditData}
        type={simpleDialogType}
      />

      {/* Usable Group Dialog */}
      <UsableGroupDialog
        open={usableDialogOpen}
        onOpenChange={setUsableDialogOpen}
        onSave={handleUsableSave}
        editData={usableEditData}
        groupOptions={tokenGroupOptions.filter(
          (option) =>
            option.value === usableEditData?.name ||
            !usableGroupsList.some((group) => group.name === option.value)
        )}
      />

      {/* Auto Group Dialog */}
      <Dialog open={autoGroupDialogOpen} onOpenChange={setAutoGroupDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Add auto group')}</DialogTitle>
            <DialogDescription>
              {t('Choose an existing token group for auto assignment.')}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4 py-4'>
            <div className='space-y-2'>
              <Label>{t('Token group')}</Label>
              <Combobox
                value={autoGroupInput}
                onValueChange={setAutoGroupInput}
                options={tokenGroupOptions.filter(
                  (option) => !autoGroupsList.includes(option.value)
                )}
                placeholder={t('Select a token group')}
                searchPlaceholder={t('Search token groups...')}
                emptyText={t('No available token group found.')}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setAutoGroupDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleAutoGroupSave}>{t('Add')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Group Override Dialog */}
      <GroupOverrideDialog
        open={groupOverrideDialogOpen}
        onOpenChange={setGroupOverrideDialogOpen}
        onSave={handleOverrideSave}
        editData={groupOverrideEditData}
        userGroup={groupOverrideUserGroup}
        groupOptions={tokenGroupOptions}
      />
    </div>
  )
})

// Simple Group Dialog Component
type SimpleGroupDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (name: string, value: string) => void
  editData: SimpleGroup | null
  type: 'groupRatio' | 'topupGroupRatio' | null
}

function SimpleGroupDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  type,
}: SimpleGroupDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [value, setValue] = useState('')

  const title =
    type === 'groupRatio'
      ? t('token group base ratio')
      : t('customer tier recharge discount')

  // eflow: Radix Dialog 的 onOpenChange 只在用户主动关闭时回调，父组件控制 open=true
  // 时不触发；改用 useEffect 在 open & editData 变化时初始化字段，否则编辑入口打开后
  // name/value 始终为空字符串，handleSave 早退（!name.trim()）→ 静默无法保存。
  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect */
    if (open && editData) {
      setName(editData.name)
      setValue(editData.value)
    } else if (!open) {
      setName('')
      setValue('')
    }
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [open, editData])

  const handleSave = () => {
    if (!name.trim() || !value.trim()) return
    onSave(name.trim(), value.trim())
    setName('')
    setValue('')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editData
              ? t('Edit {{title}}', { title })
              : t('Add {{title}}', { title })}
          </DialogTitle>
          <DialogDescription>
            {type === 'groupRatio'
              ? t('Configure the base billing multiplier for a token group.')
              : t(
                  'Configure the recharge price multiplier for a customer tier. A multiplier of 0.9 means 10% off.'
                )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>
              {type === 'groupRatio' ? t('Token group') : t('Customer tier')}
            </Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('default')}
              disabled={!!editData}
            />
          </div>
          <div className='space-y-2'>
            <Label>
              {type === 'groupRatio'
                ? t('Billing multiplier')
                : t('Recharge multiplier')}
            </Label>
            <Input
              value={value}
              onChange={(e) => {
                const val = e.target.value
                if (val === '' || !isNaN(parseFloat(val))) {
                  setValue(val)
                }
              }}
              placeholder='1.0'
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Usable Group Dialog Component
type UsableGroupDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (name: string, description: string) => void
  editData: UsableGroup | null
  groupOptions: Array<{ value: string; label: string }>
}

function UsableGroupDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  groupOptions,
}: UsableGroupDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')

  // 同 SimpleGroupDialog：onOpenChange 不会被父组件 open=true 触发，必须改 useEffect。
  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect */
    if (open && editData) {
      setName(editData.name)
      setDescription(editData.description)
    } else if (!open) {
      setName('')
      setDescription('')
    }
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [open, editData])

  const handleSave = () => {
    if (!name.trim() || !description.trim()) return
    onSave(name.trim(), description.trim())
    setName('')
    setDescription('')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editData
              ? t('Edit user-selectable group')
              : t('Set as user-selectable')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Choose an existing token group and add the description shown to users.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>{t('Token group')}</Label>
            <Combobox
              value={name}
              onValueChange={setName}
              options={groupOptions}
              placeholder={t('Select a token group')}
              searchPlaceholder={t('Search token groups...')}
              emptyText={t('No available token group found.')}
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Description')}</Label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('VIP users with premium access')}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Group Override Dialog Component
type GroupOverrideDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (targetGroup: string, ratio: number, oldTargetGroup?: string) => void
  editData: GroupOverride | null
  userGroup: string | null
  groupOptions: Array<{ value: string; label: string }>
}

function GroupOverrideDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  userGroup,
  groupOptions,
}: GroupOverrideDialogProps) {
  const { t } = useTranslation()
  const [targetGroup, setTargetGroup] = useState('')
  const [ratio, setRatio] = useState('')

  // 同 SimpleGroupDialog：onOpenChange 不会被父组件 open=true 触发，必须改 useEffect。
  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect */
    if (open && editData) {
      setTargetGroup(editData.targetGroup)
      setRatio(String(editData.ratio))
    } else if (!open) {
      setTargetGroup('')
      setRatio('')
    }
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [open, editData])

  const handleSave = () => {
    if (!targetGroup.trim() || !ratio.trim()) return
    const parsedRatio = parseFloat(ratio)
    if (isNaN(parsedRatio)) return

    onSave(targetGroup.trim(), parsedRatio, editData?.targetGroup)
    setTargetGroup('')
    setRatio('')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editData ? t('Edit ratio override') : t('Add ratio override')}
          </DialogTitle>
          <DialogDescription>
            {userGroup
              ? t(
                  'Configure a custom ratio for "{{userGroup}}" users when using a specific token group.',
                  { userGroup }
                )
              : t(
                  'Configure a custom ratio for when users use a specific token group.'
                )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>{t('Target group')}</Label>
            <Combobox
              value={targetGroup}
              onValueChange={setTargetGroup}
              options={groupOptions}
              placeholder={t('Select a token group')}
              searchPlaceholder={t('Search token groups...')}
              emptyText={t('No token group found.')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('The token group that will have a custom ratio')}
            </p>
          </div>
          <div className='space-y-2'>
            <Label>{t('Ratio')}</Label>
            <Input
              value={ratio}
              onChange={(e) => {
                const val = e.target.value
                if (val === '' || !isNaN(parseFloat(val))) {
                  setRatio(val)
                }
              }}
              placeholder='0.9'
            />
            <p className='text-muted-foreground text-xs'>
              {t('Multiplier applied when {{userGroup}} uses {{targetGroup}}', {
                userGroup: userGroup || t('this user group'),
                targetGroup: targetGroup || t('this token group'),
              })}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
