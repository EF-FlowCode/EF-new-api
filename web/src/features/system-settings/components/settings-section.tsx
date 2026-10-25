type SettingsSectionProps = {
  title: string
  titleProps?: React.HTMLAttributes<HTMLHeadingElement>
  description?: string
  children: React.ReactNode
  className?: string
}

export function SettingsSection({
  title,
  titleProps,
  description,
  children,
  className,
}: SettingsSectionProps) {
  const baseClassName =
    'border-border/70 bg-card/60 space-y-4 rounded-lg border p-4 shadow-xs sm:p-5'
  const sectionClassName = className
    ? `${baseClassName} ${className}`
    : baseClassName

  return (
    <section className={sectionClassName}>
      <div className='space-y-1'>
        <h3
          {...titleProps}
          className={
            titleProps?.className
              ? `text-sm font-semibold ${titleProps.className}`
              : 'text-sm font-semibold'
          }
        >
          {title}
        </h3>
        {description && (
          <p className='text-muted-foreground text-sm'>{description}</p>
        )}
      </div>
      {children}
    </section>
  )
}
