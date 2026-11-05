import {
  BarChartIcon,
  CodeSquareIcon,
  GraduationCapIcon,
  MessageSquarePlusIcon,
  NotepadTextIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

type PlaygroundEmptyStateProps = {
  onSelectPrompt: (prompt: string) => void
}

const starterPrompts = [
  { icon: BarChartIcon, text: 'Analyze data' },
  { icon: NotepadTextIcon, text: 'Summarize text' },
  { icon: CodeSquareIcon, text: 'Code' },
  { icon: GraduationCapIcon, text: 'Get advice' },
]

export function PlaygroundEmptyState({
  onSelectPrompt,
}: PlaygroundEmptyStateProps) {
  const { t } = useTranslation()

  return (
    <div className='flex min-h-[min(520px,calc(100svh-18rem))] items-center justify-center px-1 py-8 md:py-12'>
      <div className='grid w-full max-w-2xl gap-5 text-center'>
        <div className='bg-muted/50 text-muted-foreground mx-auto flex size-11 items-center justify-center rounded-xl border'>
          <MessageSquarePlusIcon className='size-5' aria-hidden='true' />
        </div>

        <div className='grid gap-2'>
          <h2 className='text-xl font-semibold tracking-tight text-balance md:text-2xl'>
            {t('Start a playground chat')}
          </h2>
          <p className='text-muted-foreground mx-auto max-w-lg text-sm leading-6 text-balance'>
            {t(
              'Test a model with a starter prompt, or write your own request below.'
            )}
          </p>
        </div>

        <div className='grid gap-2 sm:grid-cols-2'>
          {starterPrompts.map(({ icon: Icon, text }) => {
            const prompt = t(text)

            return (
              <Button
                className='h-auto min-h-11 justify-start gap-2 px-3 py-2.5 text-left whitespace-normal'
                key={text}
                onClick={() => onSelectPrompt(prompt)}
                variant='outline'
              >
                <Icon className='text-muted-foreground size-4' />
                <span>{prompt}</span>
              </Button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
