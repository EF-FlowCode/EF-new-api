import { useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

// 字段对应 setting/invitation_code_setting.go::InvitationCodePolicy。
// group_quotas / role_quotas 是字典类型，编辑频次低，留给 JSON raw 改。
type Policy = {
  min_account_age_days?: number
  default_generate_quota?: number
  monthly_generate_quota?: number
  min_topup_money?: number
  default_code_max_uses?: number
  default_code_valid_days?: number
  group_quotas?: Record<string, number>
  role_quotas?: Record<string, number>
}

type Props = {
  value: string
  onChange: (value: string) => void
}

const DEFAULT_POLICY: Required<
  Omit<Policy, 'group_quotas' | 'role_quotas'>
> & { group_quotas: Record<string, number>; role_quotas: Record<string, number> } = {
  min_account_age_days: 7,
  default_generate_quota: 5,
  monthly_generate_quota: 0,
  min_topup_money: 0,
  default_code_max_uses: 1,
  default_code_valid_days: 30,
  group_quotas: {},
  role_quotas: {},
}

// 解析 JSON 字符串到 Policy 对象，失败返回 null（保留 JSON 原文给用户修）。
function parsePolicy(value: string): Policy | null {
  const trimmed = value.trim()
  if (!trimmed) return {}
  try {
    const parsed = JSON.parse(trimmed)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Policy
    }
    return null
  } catch {
    return null
  }
}

// 把 Policy 对象重新序列化成漂亮 JSON。保持字段顺序对人友好。
function stringifyPolicy(policy: Policy): string {
  // 按 DEFAULT_POLICY 的字段顺序排列，未知字段附加在后面（保留 admin 自定义的奇怪字段）。
  const ordered: Record<string, unknown> = {}
  const keys: Array<keyof typeof DEFAULT_POLICY> = [
    'min_account_age_days',
    'default_generate_quota',
    'monthly_generate_quota',
    'min_topup_money',
    'default_code_max_uses',
    'default_code_valid_days',
    'group_quotas',
    'role_quotas',
  ]
  for (const k of keys) {
    if (policy[k] !== undefined) ordered[k] = policy[k]
  }
  for (const [k, v] of Object.entries(policy)) {
    if (!(k in ordered)) ordered[k] = v
  }
  return JSON.stringify(ordered, null, 2)
}

export function InvitationCodePolicyVisualEditor({ value, onChange }: Props) {
  const [rawMode, setRawMode] = useState(false)
  const [rawDraft, setRawDraft] = useState(value)
  const [rawError, setRawError] = useState<string | null>(null)

  const policy = useMemo(() => parsePolicy(value), [value])
  const policyInvalid = policy === null

  // 切到 raw 模式时同步初始化草稿。
  const enterRawMode = () => {
    setRawDraft(value)
    setRawError(null)
    setRawMode(true)
  }

  const exitRawMode = () => {
    // 尝试解析草稿；解析成功才退出，失败则报错留在 raw 模式。
    const parsed = parsePolicy(rawDraft)
    if (parsed === null) {
      setRawError('JSON 格式无效，请修正后再切回可视化编辑。')
      return
    }
    setRawError(null)
    setRawMode(false)
    onChange(stringifyPolicy(parsed))
  }

  // 可视化字段改动 -> 序列化 -> 传出。
  const updateField = <K extends keyof Policy>(key: K, raw: string) => {
    if (policy === null) return // 当前 JSON 不合法，禁止 visual 修改
    const next: Policy = { ...policy }
    if (raw === '') {
      delete (next as Record<string, unknown>)[key]
    } else {
      const n = Number(raw)
      if (Number.isFinite(n)) (next as Record<string, unknown>)[key] = n
    }
    onChange(stringifyPolicy(next))
  }

  // raw textarea 改动只更新 draft，确认时才校验并 commit 到外层。
  const handleRawChange = (text: string) => {
    setRawDraft(text)
    setRawError(null)
    const parsed = parsePolicy(text)
    if (parsed !== null) {
      // 实时同步：JSON 有效就立即写回外层，避免漏掉保存。
      onChange(text)
    }
  }

  if (rawMode) {
    return (
      <div className='space-y-2'>
        <div className='flex items-center justify-between'>
          <Label className='text-sm font-medium'>原始 JSON</Label>
          <Button type='button' size='sm' variant='outline' onClick={exitRawMode}>
            <ChevronRight className='mr-1 h-3 w-3 rotate-90' />
            回到可视化
          </Button>
        </div>
        <Textarea
          value={rawDraft}
          onChange={(e) => handleRawChange(e.target.value)}
          rows={14}
          spellCheck={false}
          className='font-mono text-xs'
        />
        {rawError ? (
          <p className='text-destructive text-xs'>{rawError}</p>
        ) : null}
        <p className='text-muted-foreground text-xs'>
          手动编辑 JSON 字段，可改 group_quotas / role_quotas 等字典类型。
          有效 JSON 会即时同步到表单。
        </p>
      </div>
    )
  }

  if (policyInvalid) {
    return (
      <div className='space-y-2'>
        <div className='text-destructive text-sm'>
          当前 JSON 格式无效，无法用可视化编辑。请切到 raw 模式修正。
        </div>
        <Button type='button' size='sm' variant='outline' onClick={enterRawMode}>
          打开原始 JSON 编辑
        </Button>
      </div>
    )
  }

  const get = (k: keyof Policy, fallback: number) =>
    typeof policy[k] === 'number' ? (policy[k] as number) : fallback

  return (
    <div className='space-y-4'>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1'>
          <Label className='text-xs'>最低账龄（天）</Label>
          <Input
            type='number'
            min={0}
            value={get('min_account_age_days', DEFAULT_POLICY.min_account_age_days)}
            onChange={(e) => updateField('min_account_age_days', e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            注册满 N 天才能自助生成邀请码。0 = 不限。
          </p>
        </div>

        <div className='space-y-1'>
          <Label className='text-xs'>终身配额</Label>
          <Input
            type='number'
            min={-1}
            value={get(
              'default_generate_quota',
              DEFAULT_POLICY.default_generate_quota
            )}
            onChange={(e) =>
              updateField('default_generate_quota', e.target.value)
            }
          />
          <p className='text-muted-foreground text-xs'>
            每用户最多累计生成的邀请码数量。-1 = 无限。
          </p>
        </div>

        <div className='space-y-1'>
          <Label className='text-xs'>近 30 天配额（每月）</Label>
          <Input
            type='number'
            min={0}
            value={get(
              'monthly_generate_quota',
              DEFAULT_POLICY.monthly_generate_quota
            )}
            onChange={(e) =>
              updateField('monthly_generate_quota', e.target.value)
            }
          />
          <p className='text-muted-foreground text-xs'>
            近 30 天滑动窗口内最多生成几条。<strong>1 = 每月最多 1 条</strong>。0 = 不限。
          </p>
        </div>

        <div className='space-y-1'>
          <Label className='text-xs'>最低累计充值（元）</Label>
          <Input
            type='number'
            min={0}
            step={0.01}
            value={get('min_topup_money', DEFAULT_POLICY.min_topup_money)}
            onChange={(e) => updateField('min_topup_money', e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            按 topup.money 累计求和（仅成功订单）。<strong>0 = 不限</strong>；&gt;0 时用户必须累计充值满该金额才能生成邀请码。
          </p>
        </div>

        <div className='space-y-1'>
          <Label className='text-xs'>每码最大使用次数</Label>
          <Input
            type='number'
            min={0}
            value={get(
              'default_code_max_uses',
              DEFAULT_POLICY.default_code_max_uses
            )}
            onChange={(e) =>
              updateField('default_code_max_uses', e.target.value)
            }
          />
          <p className='text-muted-foreground text-xs'>
            <strong>1 = 邀请一人后就失效</strong>。0 = 不限制使用次数。
          </p>
        </div>

        <div className='space-y-1'>
          <Label className='text-xs'>有效期（天）</Label>
          <Input
            type='number'
            min={0}
            value={get(
              'default_code_valid_days',
              DEFAULT_POLICY.default_code_valid_days
            )}
            onChange={(e) =>
              updateField('default_code_valid_days', e.target.value)
            }
          />
          <p className='text-muted-foreground text-xs'>
            邀请码自生成起 N 天后失效。0 = 永久。
          </p>
        </div>
      </div>

      <div className='flex items-center justify-between border-t pt-3'>
        <p className='text-muted-foreground text-xs'>
          按分组 / 角色定制配额请用 raw JSON 编辑 group_quotas / role_quotas。
        </p>
        <Button type='button' size='sm' variant='outline' onClick={enterRawMode}>
          <ChevronDown className='mr-1 h-3 w-3' />
          打开原始 JSON
        </Button>
      </div>
    </div>
  )
}
