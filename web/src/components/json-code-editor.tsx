/* eslint-disable react-refresh/only-export-components */
import { useMemo, type ComponentProps } from 'react'
import { AlertCircle, Braces, CheckCircle2, Code2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

export function isValidJsonDraft(value: string): boolean {
  if (!value.trim()) return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

export function formatJsonDraft(value: string): string | null {
  if (!value.trim()) return null
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return null
  }
}

export type JsonCodeEditorProps = Omit<
  ComponentProps<'div'>,
  'name' | 'onBlur' | 'onChange'
> & {
  value: string
  onChange: (value: string) => void
  name?: string
  onBlur?: () => void
  textareaRef?: (element: HTMLTextAreaElement | null) => void
  disabled?: boolean
  heightClassName?: string
  placeholder?: string
  ariaLabel?: string
}

export function JsonCodeEditor({
  value,
  onChange,
  name,
  onBlur,
  textareaRef,
  disabled,
  heightClassName = 'h-56 min-h-56 max-h-56',
  placeholder,
  ariaLabel,
  className,
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rootProps
}: JsonCodeEditorProps) {
  const { t } = useTranslation()
  const valid = useMemo(() => isValidJsonDraft(value), [value])

  const formatJson = () => {
    const formatted = formatJsonDraft(value)
    if (formatted !== null) onChange(formatted)
  }

  return (
    <div
      className={cn(
        'border-input bg-background focus-within:border-ring focus-within:ring-ring/50 overflow-hidden rounded-md border transition-colors focus-within:ring-[3px]',
        className
      )}
      {...rootProps}
    >
      <div className='bg-muted/30 flex min-h-8 items-center justify-between gap-2 border-b px-2'>
        <span className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
          <Braces className='size-3.5' aria-hidden='true' />
          {t('JSON')}
        </span>
        <div className='flex items-center gap-1.5'>
          <span
            className={cn(
              'flex items-center gap-1 text-xs',
              valid ? 'text-emerald-600' : 'text-destructive'
            )}
          >
            {valid ? (
              <CheckCircle2 className='size-3.5' aria-hidden='true' />
            ) : (
              <AlertCircle className='size-3.5' aria-hidden='true' />
            )}
            {valid ? t('Valid JSON') : t('Invalid JSON')}
          </span>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-6 px-2 text-xs'
            onClick={formatJson}
            disabled={disabled || !valid || !value.trim()}
          >
            <Code2 className='size-3.5' aria-hidden='true' />
            {t('Format JSON')}
          </Button>
        </div>
      </div>
      <Textarea
        ref={textareaRef}
        id={id}
        name={name}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        aria-label={ariaLabel}
        aria-describedby={ariaDescribedBy}
        aria-invalid={ariaInvalid ?? !valid}
        onBlur={onBlur}
        onChange={(event) => onChange(event.target.value)}
        className={cn(
          '[field-sizing:fixed] resize-none rounded-none border-0 bg-transparent px-3 py-2 font-mono text-xs leading-5 shadow-none focus-visible:ring-0',
          heightClassName
        )}
        spellCheck={false}
      />
    </div>
  )
}
