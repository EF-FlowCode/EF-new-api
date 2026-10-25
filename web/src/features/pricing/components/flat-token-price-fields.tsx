import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { FlatTokenPrices } from '../lib/flat-token-price'

const PRICE_FIELDS = [
  ['inputPrice', 'Input price', '3', true],
  ['outputPrice', 'Output price', '15', true],
  ['cacheReadPrice', 'Cache read price', '0.3', false],
  ['cacheCreatePrice', 'Cache write price (5m)', '3.75', false],
  ['cacheCreate1hPrice', 'Cache write price (1h)', '6', false],
  ['imageInputPrice', 'Image input price', '', false],
  ['imageOutputPrice', 'Image output price', '', false],
  ['audioInputPrice', 'Audio input price', '', false],
  ['audioOutputPrice', 'Audio output price', '', false],
  ['batchInputPrice', 'Batch input price', '', false],
] as const

type FlatTokenPriceFieldsProps = {
  value: FlatTokenPrices
  onChange: (value: FlatTokenPrices) => void
}

export function FlatTokenPriceFields({
  value,
  onChange,
}: FlatTokenPriceFieldsProps) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'All prices are in USD per 1M tokens. Cache write prices are stored independently.'
        )}
      </p>
      <div className='grid gap-4 sm:grid-cols-2'>
        {PRICE_FIELDS.map(([field, label, placeholder, required]) => (
          <div key={field} className='space-y-2'>
            <Label htmlFor={'token-price-' + field}>{t(label)}</Label>
            <Input
              id={'token-price-' + field}
              type='number'
              min='0'
              step='any'
              required={required}
              placeholder={placeholder}
              value={value[field] || ''}
              onChange={(event) =>
                onChange({ ...value, [field]: event.target.value })
              }
            />
          </div>
        ))}
      </div>
    </div>
  )
}
