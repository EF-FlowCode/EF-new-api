import { Link } from '@tanstack/react-router'
import { ThemeSwitch } from '@/components/theme-switch'
import './portal.css'

/**
 * e-flowcode 营销门户首页 (替代旧版「未登录直接弹 sign-in」流程)
 *
 * - 渲染于 / 路由,无论是否登录都可见。
 * - 与 new-api 默认壳 __root.tsx 共存:壳只有 NavigationProgress + Toaster,
 *   不会注入额外顶栏,所以 portal 自带 nav 不冲突。
 * - 所有样式作用在 .portal-root 父选择器下,不污染其它路由。
 */

const Arrow = () => (
  <svg viewBox='0 0 12 12' fill='none' stroke='currentColor' strokeWidth='2'>
    <path d='M3 6h6M7 3l3 3-3 3' />
  </svg>
)

interface Workshop {
  href: string
  cls: string
  idx: string
  name: string
  en: string
  desc: string
}

const workshops: Workshop[] = [
  { href: 'https://e-flowcode.cc/image-web/',   cls: 'c-image',   idx: '№ 01', name: '图片工坊', en: 'image-web',   desc: 'GPT Image 2 / Nano Banana 一键出图,参数极简,批量预览。' },
  { href: 'https://e-flowcode.cc/video-web/',   cls: 'c-video',   idx: '№ 02', name: '视频工坊', en: 'video-web',   desc: 'happyhorse 系列 t2v / i2v / r2v / video-edit,异步轮询。' },
  { href: 'https://e-flowcode.cc/diagram-web/', cls: 'c-diagram', idx: '№ 03', name: '图表工坊', en: 'diagram-web', desc: '流程 / 循环 / 层级 / 并列,AI 直出 SmartArt。' },
  { href: 'https://e-flowcode.cc/docx-web/',    cls: 'c-docx',    idx: '№ 04', name: '笺墨工坊', en: 'docx-web',    desc: 'LLM 出 FormatPlan + 本地引擎写回,聊天式排版。' },
  { href: 'https://e-flowcode.cc/canvas-web/',  cls: 'c-canvas',  idx: '№ 05', name: '织墨工坊', en: 'canvas-web',  desc: '节点拖拽 + 连线聚合上游 + Image 2 / Nano Banana 生图。' },
  { href: 'https://e-flowcode.cc/voice-web/',   cls: 'c-voice',   idx: '№ 06', name: '语音工坊', en: 'voice-web',   desc: 'Qwen-TTS 4 音色 / qwen3-tts-flash 22 音色,一键合成。' },
  { href: 'https://e-flowcode.cc/omni-web/',    cls: 'c-omni',    idx: '№ 07', name: '通感工坊', en: 'omni-web',    desc: 'Qwen3.5-Omni 全模态理解—文 / 图 / 音 / 视频 → 文本。' },
  { href: '/pricing',                            cls: 'c-market',  idx: '№ 00', name: '模型广场', en: 'e-flowcode.cc', desc: '完整模型清单、用户分组、控制台、API Key 管理。' },
]

export function Portal() {
  return (
    <div className='portal-root'>
      {/* ───────── nav ───────── */}
      <nav className='top'>
        <div className='nav-inner'>
          <Link to='/' className='brand'>
            <img
              className='brand-mark'
              src='https://files.seeusercontent.com/2026/04/01/tR2t/logo-new.png'
              alt='e-flowcode'
            />
            <span className='brand-name'>e-flowcode</span>
          </Link>
          <div />
          <div className='nav-links'>
            <Link to='/pricing' className='nav-link'>模型广场</Link>
            <Link to='/leaderboard' className='nav-link'>排行榜</Link>
            <Link to='/dashboard' className='nav-link'>控制台</Link>
            <a className='nav-link' href='https://e-flowcode.cc/docs/' target='_blank' rel='noopener noreferrer'>文档</a>
          </div>
          <div className='theme-picker-slot'>
            <ThemeSwitch />
          </div>
          <Link to='/sign-in' className='nav-cta'>注册 →</Link>
        </div>
      </nav>

      {/* ───────── hero ───────── */}
      <section className='hero'>
        <div className='hero-l'>
          <h1 className='hero-title'>
            主流大模型,<br />
            <em>统一入口</em>。
          </h1>
          <div className='hero-ctas'>
            <Link to='/dashboard' className='btn btn-primary'>
              控制台
              <svg width='12' height='12' viewBox='0 0 16 16' fill='none' stroke='currentColor' strokeWidth='2.5'>
                <path d='M3 8h10M9 4l4 4-4 4' />
              </svg>
            </Link>
            <Link to='/pricing' className='btn btn-ghost'>模型广场</Link>
            <a className='btn btn-ghost' href='https://e-flowcode.cc/docs/' target='_blank' rel='noopener noreferrer'>文档</a>
          </div>
        </div>

        <div className='hero-r'>
          <div className='codewin'>
            <div className='codewin-head'>
              <div className='cw-bullets'><span /><span /><span /></div>
              <span>~/your-app · main.py</span>
              <span className='cw-tab'>python</span>
            </div>
            <div className='codewin-body'>
              <span className='cw-line'><span className='cw-comment'># 把官方 base_url 换成 e-flowcode 即可</span></span>
              <span className='cw-line'><span className='cw-key'>from</span> <span className='cw-plain'>openai</span> <span className='cw-key'>import</span> <span className='cw-plain'>OpenAI</span></span>
              <span className='cw-line'></span>
              <span className='cw-line'><span className='cw-var'>client</span> <span className='cw-punct'>=</span> <span className='cw-fn'>OpenAI</span><span className='cw-punct'>(</span></span>
              <span className='cw-line'>    <span className='cw-var'>base_url</span><span className='cw-punct'>=</span><span className='cw-str'>"https://e-flowcode.cc/v1"</span><span className='cw-punct'>,</span></span>
              <span className='cw-line'>    <span className='cw-var'>api_key</span><span className='cw-punct'>=</span><span className='cw-str'>"sk-************"</span><span className='cw-punct'>,</span></span>
              <span className='cw-line'><span className='cw-punct'>)</span></span>
              <span className='cw-line'></span>
              <span className='cw-line'><span className='cw-var'>resp</span> <span className='cw-punct'>=</span> <span className='cw-var'>client</span><span className='cw-punct'>.</span><span className='cw-var'>chat</span><span className='cw-punct'>.</span><span className='cw-var'>completions</span><span className='cw-punct'>.</span><span className='cw-fn'>create</span><span className='cw-punct'>(</span></span>
              <span className='cw-line'>    <span className='cw-var'>model</span><span className='cw-punct'>=</span><span className='cw-str'>"claude-opus-4-7"</span><span className='cw-punct'>,</span>   <span className='cw-comment'># 直接写厂商模型名</span></span>
              <span className='cw-line'>    <span className='cw-var'>messages</span><span className='cw-punct'>=</span><span className='cw-punct'>[{'{'}</span><span className='cw-str'>"role"</span><span className='cw-punct'>:</span> <span className='cw-str'>"user"</span><span className='cw-punct'>,</span> <span className='cw-str'>"content"</span><span className='cw-punct'>:</span> <span className='cw-str'>"嗨"</span><span className='cw-punct'>{'}'}]</span><span className='cw-punct'>,</span></span>
              <span className='cw-line'><span className='cw-punct'>)</span></span>
            </div>
            <div className='codewin-foot'>
              <span><span className='blink' />OpenAI / Anthropic 协议任选其一</span>
              <a href='https://e-flowcode.cc/docs/' target='_blank' rel='noopener noreferrer'>完整文档 →</a>
            </div>
          </div>
        </div>
      </section>

      {/* ───────── 工坊墙 ───────── */}
      <section className='wall-wrap' id='wall'>
        <div className='section-head'>
          <h2 className='section-h'>顺手还能用的小工坊。</h2>
        </div>
        <div className='wall'>
          {workshops.map((w) => {
            const isInternal = w.href.startsWith('/')
            const content = (
              <>
                <div className='idx'>{w.idx}</div>
                <h3 className='name'>{w.name}</h3>
                <div className='en'>{w.en}</div>
                <p className='desc'>{w.desc}</p>
                <span className='arrow'><Arrow /></span>
              </>
            )
            return isInternal ? (
              <Link key={w.en} to={w.href} className={`note ${w.cls}`}>{content}</Link>
            ) : (
              <a key={w.en} href={w.href} className={`note ${w.cls}`} target='_blank' rel='noopener noreferrer'>{content}</a>
            )
          })}
        </div>
      </section>
    </div>
  )
}
