// 拆出来满足 react-refresh/only-export-components——theme-provider.tsx 只导出
// 组件/hook，常量与类型放这里。
// eflow: 6 主题 slug 用英文语义名（古风中文名见 atelier-themes.ts label）。
//   cinnabar 丹霞 / azure 霁青 / verdant 苍翠 / violet 烟紫 / frost 凝霜 / midnight 夜阑
//   midnight 是全套唯一暗档（theme-provider 对它加 .dark）。
export const THEMES = [
  'cinnabar',
  'azure',
  'verdant',
  'violet',
  'frost',
  'midnight',
] as const

export type Theme = (typeof THEMES)[number]
