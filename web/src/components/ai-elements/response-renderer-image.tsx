import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { sanitizeImageSrc, type ImageNode } from 'stream-markdown-parser'

type ResponseImageProps = {
  node: ImageNode
}

export function ResponseImage(props: ResponseImageProps) {
  const { t } = useTranslation()
  const [hasError, setHasError] = useState(false)
  const src = sanitizeImageSrc(props.node.src)

  if (!src || hasError) {
    return (
      <span className='border-border/70 text-muted-foreground my-4 inline-flex rounded-md border px-3 py-2 text-xs italic'>
        {props.node.alt || t('Image not available')}
      </span>
    )
  }

  return (
    <img
      alt={props.node.alt}
      className='border-border/70 my-4 block h-auto max-h-96 max-w-full rounded-lg border object-contain'
      loading='lazy'
      onError={() => setHasError(true)}
      src={src}
      title={props.node.title ?? undefined}
    />
  )
}
