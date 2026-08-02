import type { Theme } from '@/context/theme-constants'

export interface AtelierThemeMeta {
  value: Theme
  label: string
  swatchColor: string
}

// eflow: 统一古风 6 主题。slug=英文语义名，label=诗意中文名，swatchColor=该主题主色。
// 顺序即下拉展示顺序：5 浅色（红/蓝/绿/紫/冷白）+ 1 暗色。
export const ATELIER_THEME_META: AtelierThemeMeta[] = [
  { value: 'cinnabar', label: '丹霞', swatchColor: '#c84536' },
  { value: 'azure', label: '霁青', swatchColor: '#4767a3' },
  { value: 'verdant', label: '苍翠', swatchColor: '#51866a' },
  { value: 'violet', label: '烟紫', swatchColor: '#7a679e' },
  { value: 'frost', label: '凝霜', swatchColor: '#dbe2ec' },
  { value: 'midnight', label: '夜阑', swatchColor: '#1e2433' },
]
