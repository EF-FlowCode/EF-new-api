import { CircleOff } from 'lucide-react'

export function FeatureUnavailable({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto flex min-h-[50vh] w-full max-w-[1440px] items-center justify-center'>
        <div className='border-muted-foreground/30 flex max-w-lg flex-col items-center rounded-2xl border border-dashed px-8 py-16 text-center'>
          <CircleOff className='text-muted-foreground mb-4 size-9' />
          <h1 className='text-lg font-semibold'>{title}</h1>
          <p className='text-muted-foreground mt-2 text-sm'>{description}</p>
        </div>
      </div>
    </div>
  )
}
