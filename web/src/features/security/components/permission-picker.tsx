import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import type { AccessTokenCatalogGroup } from '../types'

const GROUP_LABELS: Record<AccessTokenCatalogGroup['group'], string> = {
  personal: 'Personal',
  admin: 'Administration',
  system: 'System',
}

function scopeKey(resource: string, action: string) {
  return `${resource}:${action}`
}

interface PermissionPickerProps {
  groups: AccessTokenCatalogGroup[]
  value: string[]
  onChange: (scopes: string[]) => void
  disabled?: boolean
}

/** Grouped resource × action checkboxes for an access token grant. */
export function PermissionPicker({
  groups,
  value,
  onChange,
  disabled,
}: PermissionPickerProps) {
  const { t } = useTranslation()
  const selected = new Set(value)

  const toggle = (key: string, checked: boolean) => {
    const next = new Set(selected)
    if (checked) next.add(key)
    else next.delete(key)
    onChange([...next].sort())
  }

  const setGroup = (group: AccessTokenCatalogGroup, checked: boolean) => {
    const next = new Set(selected)
    for (const resource of group.resources) {
      for (const action of resource.actions) {
        const key = scopeKey(resource.resource, action.action)
        if (checked) next.add(key)
        else next.delete(key)
      }
    }
    onChange([...next].sort())
  }

  return (
    <div className='space-y-4'>
      {groups.map((group) => {
        const keys = group.resources.flatMap((resource) =>
          resource.actions.map((action) =>
            scopeKey(resource.resource, action.action)
          )
        )
        const count = keys.filter((key) => selected.has(key)).length
        return (
          <section key={group.group} className='rounded-lg border'>
            <header className='bg-muted/40 flex items-center justify-between gap-2 border-b px-3 py-2'>
              <span className='text-sm font-medium'>
                {t(GROUP_LABELS[group.group])}
                <span className='text-muted-foreground ml-2 text-xs'>
                  {count}/{keys.length}
                </span>
              </span>
              <div className='flex gap-1'>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  className='h-7 px-2 text-xs'
                  disabled={disabled || count === keys.length}
                  onClick={() => setGroup(group, true)}
                >
                  {t('Select all')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  className='h-7 px-2 text-xs'
                  disabled={disabled || count === 0}
                  onClick={() => setGroup(group, false)}
                >
                  {t('Clear')}
                </Button>
              </div>
            </header>
            <div className='divide-y'>
              {group.resources.map((resource) => (
                <div
                  key={resource.resource}
                  className='flex flex-col gap-2 px-3 py-2 sm:flex-row sm:items-start sm:justify-between'
                >
                  <span className='text-sm'>{t(resource.label_key)}</span>
                  <div className='flex flex-wrap gap-x-4 gap-y-1'>
                    {resource.actions.map((action) => {
                      const key = scopeKey(resource.resource, action.action)
                      const id = `scope-${key}`
                      return (
                        <label
                          key={key}
                          htmlFor={id}
                          className='flex cursor-pointer items-center gap-1.5 text-xs'
                          title={
                            action.description_key
                              ? t(action.description_key)
                              : undefined
                          }
                        >
                          <Checkbox
                            id={id}
                            checked={selected.has(key)}
                            disabled={disabled}
                            onCheckedChange={(checked) =>
                              toggle(key, checked === true)
                            }
                          />
                          {t(action.label_key)}
                        </label>
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
          </section>
        )
      })}
    </div>
  )
}
