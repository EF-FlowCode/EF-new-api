import { type ImgHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

// eflow: 全站 logo 统一为部署自有图标（静态 /logo.png + 运行时 systemLogo）。
// upstream 这里原是内联的描边 SVG 站标；本站不使用任何第三方站标，改为渲染
// 部署的 /logo.png。注意：品牌/站标可自由替换；AGPL 仅要求保留源码版权头与
// LICENSE（见仓库根 LICENSE），与此处展示用图标无关。
export function Logo({
  className,
  ...props
}: ImgHTMLAttributes<HTMLImageElement>) {
  return (
    <img
      src='/logo.png'
      alt='logo'
      width={24}
      height={24}
      className={cn('size-6', className)}
      {...props}
    />
  )
}
