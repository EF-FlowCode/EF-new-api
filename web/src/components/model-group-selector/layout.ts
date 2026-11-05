export const modelGroupSelectorLayoutClasses = {
  desktopPanel: 'max-h-[min(50vh,28rem)] overflow-hidden',
  desktopContent:
    'grid h-[min(50vh,28rem)] max-h-[min(50vh,28rem)] min-h-0 gap-3 p-2 md:grid-cols-[9.5rem_minmax(0,1fr)]',
  groupColumn: 'flex h-full min-h-0 min-w-0 flex-col overflow-hidden',
  groupScroll:
    'mt-2 grid min-h-0 flex-1 auto-rows-[2rem] content-start gap-1 overflow-y-auto pr-1',
  modelColumn: 'flex h-full min-h-0 min-w-0 overflow-hidden rounded-lg border',
  modelCommand: 'min-h-0 flex-1 rounded-lg border-0 bg-transparent p-1',
  modelList:
    'min-h-0 flex-1 max-h-none [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin] [&::-webkit-scrollbar]:block [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-border [&::-webkit-scrollbar-track]:bg-transparent',
  modelItem:
    'relative mb-0.5 flex items-center justify-between rounded-md border border-transparent px-2 py-1.5 pl-3 text-[12px] leading-4 transition-colors before:absolute before:inset-y-2 before:left-1 before:w-1 before:rounded-full before:bg-transparent',
  selectedModelItem:
    'border-primary/40 bg-primary/12 text-foreground shadow-sm before:bg-primary',
  unselectedModelItem:
    'text-muted-foreground hover:bg-accent hover:text-foreground',
  selectedModelText: 'font-semibold text-foreground',
  unselectedModelText: 'font-medium',
} as const

type ScrollableOption = {
  offsetHeight?: number
  offsetTop?: number
  scrollIntoView: (options?: ScrollIntoViewOptions) => void
}

type ScrollableOptionContainer = {
  clientHeight: number
  scrollTo?: (options: ScrollToOptions) => void
  scrollTop: number
}

export function scrollSelectedOptionIntoView(
  selectedOption: ScrollableOption | null,
  scrollContainer?: ScrollableOptionContainer | null
): void {
  if (
    scrollContainer &&
    selectedOption?.offsetTop !== undefined &&
    selectedOption.offsetHeight !== undefined
  ) {
    const scrollTop = Math.max(
      0,
      selectedOption.offsetTop -
        (scrollContainer.clientHeight - selectedOption.offsetHeight) / 2
    )
    if (scrollContainer.scrollTo) {
      scrollContainer.scrollTo({ top: scrollTop, behavior: 'auto' })
    } else {
      scrollContainer.scrollTop = scrollTop
    }
    return
  }

  selectedOption?.scrollIntoView({
    block: 'center',
    inline: 'nearest',
  })
}
